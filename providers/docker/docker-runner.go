package cloudcompute

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
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

// @TODO stop using a global background context.  Need to handle this better
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
	err := dr.imagePull()
	if err != nil {
		return err
	}

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
	log.Printf("RUNNING JOB: %s\n", dr.djob.Job.ID)
	/////

	if err := dr.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		dr.djob.Status = Failed
		return err
	}

	drm := NewDockerRunMonitor(dr, resp.ID)
	drm.Wait()

	return nil
}

func (dr *DockerJobRunner) imagePull() error {
	if !dr.isLocalImage() {
		log.Printf("Image: %s not found on local system.  Pulling from the remote address.\n", dr.djob.Plugin.ImageAndTag)
		reader, err := dr.client.ImagePull(ctx, dr.djob.Plugin.ImageAndTag, image.PullOptions{})
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(reader)

		var event DockerEvent

		for {
			if err := decoder.Decode(&event); err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
			fmt.Printf("EVENT: %+v\n", event)
		}
	}
	return nil
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
		//Cmd:   djob.Job.ContainerOverrides.Command,
		Entrypoint: djob.Job.ContainerOverrides.Command,
		Env:        KvpToEnv(env),
	}

	if len(djob.Job.ContainerOverrides.Command) == 0 {
		//config.Cmd = djob.Plugin.Command
		config.Entrypoint = djob.Plugin.Command
	}

	return &config, nil
}

func getHostConfig(djob *DockerJob) (*container.HostConfig, error) {

	memoryStr := djob.Plugin.ComputeEnvironment.Memory
	cpuStr := djob.Plugin.ComputeEnvironment.VCPU
	extraHosts := djob.Plugin.ComputeEnvironment.ExtraHosts

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

	if len(extraHosts) > 0 {
		config.ExtraHosts = extraHosts
	}

	return &config, nil
}
