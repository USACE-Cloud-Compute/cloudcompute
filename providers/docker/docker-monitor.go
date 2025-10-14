package cloudcompute

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
)

type DockerRunMonitor struct {
	dr            *DockerJobRunner
	wg            *sync.WaitGroup
	logreader     io.ReadCloser
	logClosedOnce sync.Once
	containerId   string
}

func NewDockerRunMonitor(dr *DockerJobRunner, containerId string) *DockerRunMonitor {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true, // Follow the log stream
		Timestamps: true,
		Tail:       "one",
	}

	logreader, err := dr.client.ContainerLogs(ctx, containerId, options)
	if err != nil {
		log.Printf("JOB: %s: Error creating docker run monitor: %s\n", dr.djob.Job.ID.String(), err)
		dr.djob.Status = Failed
		return nil
		//drm.wg.Done()
	}

	drm := &DockerRunMonitor{
		wg:          wg,
		containerId: containerId,
		dr:          dr,
		logreader:   logreader,
	}

	drm.startLogMonitor()
	drm.startContainerMonitor()

	return drm
}

func (drm *DockerRunMonitor) startLogMonitor() {
	fmt.Println("STARTING MONITOR FOR: " + drm.containerId)
	go func(containerId string) {
		defer drm.wg.Done()

		// Create custom writer that wraps the original stdout
		customWriter := &DockerMonitorWriter{jobId: drm.dr.djob.Job.ID}

		// Use stdcopy to properly handle multiplexed output
		_, err := stdcopy.StdCopy(customWriter, customWriter, drm.logreader)
		if err != nil {
			if err == io.EOF {
				log.Printf("JOB: %s: EOF detected, shutting down log monitor\n", drm.dr.djob.Job.ID.String())
				drm.dr.djob.Status = Succeeded
				return
			}
			drm.dr.djob.Status = Failed
			log.Printf("JOB: %s: Unexpected read error, shutting down log monitor: %s\n", drm.dr.djob.Job.ID.String(), err)
			return
		}
	}(drm.containerId)
}

func (drm *DockerRunMonitor) startContainerMonitor() {

	go func(containerId string) {
		for {
			containerJSON, err := drm.dr.client.ContainerInspect(ctx, containerId)
			if err != nil {
				drm.dr.djob.Status = Failed
				log.Printf("JOB: %s: Shutting down container monitor. Error inspecting container.: %s\n", drm.dr.djob.Job.ID.String(), err)
				drm.closeLogReader()
				return
			}

			if !containerJSON.State.Running {
				if containerJSON.State.ExitCode == 1 {
					drm.dr.djob.Status = Failed
				} else {
					drm.dr.djob.Status = Succeeded
				}

				//wait 500ms to flush logs..then close
				time.Sleep(500 * time.Millisecond)
				drm.closeLogReader()
				log.Printf("JOB: %s: Shutting down container monitor. Container is no longer running.\n", drm.dr.djob.Job.ID.String())
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}(drm.containerId)
}

func (drm *DockerRunMonitor) closeLogReader() {
	drm.logClosedOnce.Do(func() {
		err := drm.logreader.Close()
		if err != nil {
			log.Printf("JOB: %s: Error closing logreader: %s\n", drm.dr.djob.Job.ID.String(), err)
		}
	})
}

func (drm *DockerRunMonitor) Wait() {

	drm.wg.Wait() //blocks here until wg.Done is called in another method
	drm.closeLogReader()
}

// Custom writer that formats output with job ID
type DockerMonitorWriter struct {
	jobId uuid.UUID
}

func (cw *DockerMonitorWriter) Write(p []byte) (n int, err error) {
	// Process the buffer to handle newlines properly
	line := []byte{}
	for _, b := range p {
		if b == '\n' {
			pl := fmt.Sprintf("JOB: %s: %s\n", cw.jobId, line)
			os.Stdout.WriteString(pl)
			line = line[:0]
		} else {
			line = append(line, b)
		}
	}

	// Flush any remaining text
	if len(line) > 0 {
		pl := fmt.Sprintf("JOB: %s: %s\n", cw.jobId, line)
		os.Stdout.WriteString(pl)
	}

	return len(p), nil
}
