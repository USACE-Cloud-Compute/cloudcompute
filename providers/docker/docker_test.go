package cloudcompute

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	. "github.com/usace/cloudcompute"
)

func TestDocker(t *testing.T) {
	computeProvider := NewDockerComputeProvider(DockerComputeProviderConfig{
		Concurrency:  10,
		StartMonitor: 5,
		MonitorFunction: func(statuses map[JobStatus]int) {
			fmt.Println("--------------")
			fmt.Println(statuses)
			fmt.Println("--------------")
		},
	})

	plugin := &Plugin{
		Name:        "HelloWorld",
		ImageAndTag: "helloworld:latest",
		Command:     []string{"/runme"},
		ComputeEnvironment: PluginComputeEnvironment{
			VCPU:   "1",
			Memory: "512",
		},
	}
	computeProvider.RegisterPlugin(plugin)

	manifestId := uuid.New()

	cm := ComputeManifest{
		ManifestName:     "HelloWorld",
		ManifestID:       manifestId,
		PluginDefinition: "HelloWorld",
	}

	computeId := uuid.New()
	eventGenerator := NewEventList([]Event{
		{
			ID:              uuid.New(),
			EventIdentifier: "1",
			Manifests:       []ComputeManifest{cm},
		},
	})

	compute := CloudCompute{
		Name:            "FFRD FIND MISSING GRIDS",
		ID:              computeId,
		JobQueue:        "docker-local",
		Events:          eventGenerator,
		ComputeProvider: computeProvider,
	}

	err := compute.Run()
	if err != nil {
		fmt.Println(err)
	}

	jobsRunning := true
	for jobsRunning {
		time.Sleep(1 * time.Second)
		compute.Status(JobsSummaryQuery{
			QueryLevel: "COMPUTE",
			QueryValue: JobNameParts{
				Compute: compute.ID.String(),
			},
			JobSummaryFunction: func(summaries []JobSummary) {
				count := 0
				for _, summary := range summaries {
					if summary.Status == "RUNNING" {
						count++
					}
				}
				if count == 0 {
					jobsRunning = false
					fmt.Println("Shutting Down")
				}
			},
		})
	}
	fmt.Println("Finished")
}
