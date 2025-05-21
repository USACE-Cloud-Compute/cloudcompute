package cloudcompute

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
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

// shuts down on EOR or Read error
func (drm *DockerRunMonitor) startLogMonitor() {
	fmt.Println("STARTING MONITOR FOR: " + drm.containerId)
	go func(containerId string) {

		buffer := make([]byte, 1024)

		for {
			//fmt.Println(">>>>>>>>>>>>>>>>>>>>>> TRYING TO READ " + drm.dr.djob.Job.ID.String() + " >>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
			n, err := drm.logreader.Read(buffer)
			//fmt.Println(">>>>>>>>>>>>>>>>>>>>>> FINISHED READ " + drm.dr.djob.Job.ID.String() + " >>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
			if err != nil {
				if err == io.EOF {
					//fmt.Println(">>>>>>>>>>>>>>>>>>>>>> EOF CLOSE " + drm.dr.djob.Job.ID.String() + " >>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
					//fmt.Printf("Size of Buffer: %d\n", n)
					if n > 0 {
						writeToStdOut(buffer[:n], drm.dr.djob.Job.ID)
					}
					drm.dr.djob.Status = Succeeded
					drm.wg.Done()
					log.Printf("JOB: %s: EOF detected, shutting down log monitor\n", drm.dr.djob.Job.ID.String())
					return
				}
				//fmt.Println(">>>>>>>>>>>>>>>>>>>>>> FAIL CLOSE " + drm.dr.djob.Job.ID.String() + " >>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
				drm.dr.djob.Status = Failed
				drm.wg.Done()
				log.Printf("JOB: %s: Unexpected read error, shutting down log monitor: %s\n", drm.dr.djob.Job.ID.String(), err)
				return
			}
			writeToStdOut(buffer[:n], drm.dr.djob.Job.ID)
		}
	}(drm.containerId)
}

func (drm *DockerRunMonitor) startContainerMonitor() {

	go func(containerId string) {
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
			}

			//wait 500ms to flush logs..then close
			time.Sleep(500 * time.Millisecond)
			drm.closeLogReader()
			log.Printf("JOB: %s: Shutting down container monitor. Container is no longer running.\n", drm.dr.djob.Job.ID.String())
			return
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

func writeToStdOut(buffer []byte, jobId uuid.UUID) {
	line := []byte{}
	for _, b := range buffer {
		if b == '\n' {
			pl := fmt.Sprintf("JOB: %s: %s\n", jobId, line)
			os.Stdout.WriteString(pl)
			line = line[:0]
		} else {
			line = append(line, b)
		}
	}

	//flush any remaining text
	if len(line) > 0 {
		pl := fmt.Sprintf("JOB: %s: %s\n", jobId, line)
		os.Stdout.WriteString(pl)
	}
}
