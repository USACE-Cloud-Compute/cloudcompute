package cloudcompute

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	. "github.com/usace/cloudcompute"
)

const (
	nanocpu  int64 = 1e9
	mbToByte int64 = 1024 * 1024
)

type DockerEvent struct {
	Status         string `json:"status"`
	Error          string `json:"error"`
	Progress       string `json:"progress"`
	ProgressDetail struct {
		Current int `json:"current"`
		Total   int `json:"total"`
	} `json:"progressDetail"`
}

var ctx context.Context = context.Background()

func NewRunner(job *DockerJob) (*DockerJobRunner, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	runner := DockerJobRunner{
		client: cli,
		djob:   job,
	}
	return &runner, nil
}

type DockerJobRunner struct {
	client      *client.Client
	djob        *DockerJob
	containerId string
	terminated  bool
}

func (dr *DockerJobRunner) Close() {
	err := dr.client.ContainerRemove(ctx, dr.containerId, container.RemoveOptions{})
	if err != nil {
		log.Printf("Failed to remove container:%s\n", err)
	}
	err = dr.client.Close()
	if err != nil {
		log.Printf("Failed to close the docker client transport: %s\n", err)
	}
}

func (dr *DockerJobRunner) Terminate() error {
	log.Printf("Sending STOP signal to Container %s\n", dr.containerId)
	err := dr.client.ContainerStop(ctx, dr.containerId, container.StopOptions{})
	if err != nil {
		log.Printf("Error attempting to terminate Container %s: %s\n", dr.containerId, err)
	}
	log.Printf("Successfuly terminated Container %s\n", dr.containerId)
	dr.djob.Status = Failed
	dr.terminated = true
	return err
}

func (dr *DockerJobRunner) Run() error {
	dr.imagePull()

	containerConfig, err := getContainerConfig(dr.djob)
	if err != nil {
		return err
	}

	hostConfig, err := getHostConfig(dr.djob)
	if err != nil {
		return err
	}

	resp, err := dr.client.ContainerCreate(ctx,
		containerConfig,
		hostConfig,
		nil, nil, "")
	if err != nil {
		dr.djob.Status = Failed
		return err
	}
	dr.djob.Status = Running
	dr.containerId = resp.ID

	/////
	//log.Println(resp)
	log.Printf("RUNNING JOB: %s\n", dr.djob.Job.ManifestID)
	/////

	if err := dr.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		dr.djob.Status = Failed
		return err
	}

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true, // Follow the log stream
		Timestamps: true,
		Tail:       "one",
	}

	logreader, err := dr.client.ContainerLogs(ctx, resp.ID, options)
	if err != nil {
		dr.djob.Status = Failed
		return err
	}
	defer logreader.Close()

	// Continuously read logs from the container
	buffer := make([]byte, 1024)
	for {
		containerJSON, err := dr.client.ContainerInspect(ctx, resp.ID)
		if err != nil {
			dr.djob.Status = Failed
			return err
		}

		if !containerJSON.State.Running {
			if containerJSON.State.ExitCode == 1 {
				dr.djob.Status = Failed
			}
			break
		}

		n, err := logreader.Read(buffer)
		if err != nil && err != io.EOF {
			dr.djob.Status = Failed
			return err
		}
		//os.Stdout.Write(buffer[:n])
		writeToStdOut(buffer[:n], dr.djob.Job.ManifestID)
	}

	//read anything remaining
	n, err := logreader.Read(buffer)
	if err != nil && err != io.EOF {
		dr.djob.Status = Failed
		return err
	}

	//os.Stdout.Write(buffer[:n])
	writeToStdOut(buffer[:n], dr.djob.Job.ManifestID)
	if !dr.terminated && dr.djob.Status != Failed {
		dr.djob.Status = Succeeded
	}
	return nil
}

func (dr *DockerJobRunner) imagePull() {
	if !dr.isLocalImage() {
		log.Printf("Image: %s not found on local system.  Pulling from the remote address.\n", dr.djob.Plugin.ImageAndTag)
		reader, err := dr.client.ImagePull(ctx, dr.djob.Plugin.ImageAndTag, image.PullOptions{})
		if err != nil {
			panic(err)
		}
		decoder := json.NewDecoder(reader)

		var event DockerEvent

		for {
			if err := decoder.Decode(&event); err != nil {
				if err == io.EOF {
					break
				}
				panic(err)
			}
			fmt.Printf("EVENT: %+v\n", event)
		}
	}
}

func (dr *DockerJobRunner) isLocalImage() bool {
	images, err := dr.client.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		log.Printf("Error checking local images: %s\n", err)
		return false
	}
	for _, image := range images {
		for _, repotag := range image.RepoTags {
			//if repotag == dr.djob.Job.JobDefinition {
			if repotag == dr.djob.Plugin.ImageAndTag {
				return true
			}
		}
	}
	return false
}

func KvpToEnv(kvp []KeyValuePair) []string {
	env := make([]string, len(kvp))
	for i, v := range kvp {
		env[i] = fmt.Sprintf("%s=%s", v.Name, v.Value)
	}
	return env
}

func getContainerConfig(djob *DockerJob) (*container.Config, error) {

	env := djob.Plugin.DefaultEnvironment
	env.Merge(&djob.Job.ContainerOverrides.Environment)
	for _, credential := range djob.Plugin.Credentials {
		env.SetVal(credential.Name, djob.SecretsManager.GetSecret(credential.Value))
	}

	config := container.Config{
		Image: djob.Plugin.ImageAndTag,
		Cmd:   djob.Job.ContainerOverrides.Command,
		Env:   KvpToEnv(env),
	}

	// for _, kvp := range env {
	// 	fmt.Printf("%s=%s\n", kvp.Name, kvp.Value)
	// }

	if len(djob.Job.ContainerOverrides.Command) == 0 {
		config.Cmd = djob.Plugin.Command
	} else {
		config.Cmd = djob.Job.ContainerOverrides.Command
	}

	return &config, nil
}

func getHostConfig(djob *DockerJob) (*container.HostConfig, error) {

	memoryStr := djob.Plugin.ComputeEnvironment.Memory
	cpuStr := djob.Plugin.ComputeEnvironment.VCPU

	for _, rr := range djob.Job.ContainerOverrides.ResourceRequirements {
		switch rr.Type {
		case ResourceTypeVcpu:
			cpuStr = rr.Value
		case ResourceTypeMemory:
			memoryStr = rr.Value
		}
	}

	memory, err := strconv.Atoi(memoryStr)
	if err != nil {
		return nil, fmt.Errorf("invalid memory value: %s", err)
	}

	cpu, err := strconv.Atoi(cpuStr)
	if err != nil {
		return nil, fmt.Errorf("invalid cpu value: %s", err)
	}

	var mounts []mount.Mount
	if len(djob.Plugin.MountPoints) > 0 {
		mounts = make([]mount.Mount, len(djob.Plugin.MountPoints))
		for i, m := range djob.Plugin.MountPoints {
			mounts[i] = mount.Mount{
				Type:   mount.TypeBind, //@TODO need to support other mount types
				Source: m.SourceVolume,
				Target: m.ContainerPath,
			}
		}
	}
	config := container.HostConfig{
		Resources: container.Resources{
			Memory:   int64(memory) * mbToByte,
			NanoCPUs: int64(cpu) * nanocpu,
		},
		Mounts: mounts,
	}
	return &config, nil
}

func writeToStdOut(buffer []byte, manifestId uuid.UUID) {
	line := []byte{}
	for _, b := range buffer {
		if b == '\n' {
			pl := fmt.Sprintf("JOB: %s: %s\n", manifestId, line)
			os.Stdout.WriteString(pl)
			line = line[:0]
		} else {
			line = append(line, b)
		}
	}

	//flush any remaining text
	if len(line) > 0 {
		pl := fmt.Sprintf("JOB: %s: %s\n", manifestId, line)
		os.Stdout.WriteString(pl)
	}
}
