package cloudcompute

//@TODO change package to docker!!!!

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	. "github.com/usace-cloud-compute/cloudcompute"
)

func TestDockerConcurrency(t *testing.T) {
	//first create the Docker Compute Provider with concurrency of 4
	computeProvider := NewDockerComputeProvider(DockerComputeProviderConfig{
		Concurrency: 4,
	})

	//create a plugin referencing the debian slim
	//and mounting a shell script into the container
	plugin := &Plugin{
		Name:        "DockerHelloWorld",
		ImageAndTag: "debian:bullseye-slim",
		Command:     []string{"/mnt/test/test-script2.sh"},
		ComputeEnvironment: PluginComputeEnvironment{
			VCPU:   "1",
			Memory: "512",
		},
		MountPoints: []MountPoint{
			{
				ContainerPath: "/mnt/test",
				SourceVolume:  "testdata",
			},
		},
	}

	//when using the docker provider, you will need to register the plugin each time you run
	computeProvider.RegisterPlugin(plugin)

	//start the manifest
	for event := 1; event <= 25; event++ {
		manifestId := uuid.New()

		cm := ComputeManifest{
			ManifestName:     "HelloWorld",
			ManifestID:       manifestId,
			PluginDefinition: "DockerHelloWorld",
		}

		computeId := uuid.New()

		//use a list event generator with a single event
		eventGenerator := NewEventList([]Event{
			{
				ID:              uuid.New(),
				EventIdentifier: strconv.Itoa(event),
				Manifests:       []ComputeManifest{cm},
			},
		})

		//set up the compute run
		compute := CloudCompute{
			Name:            "Run Docker Hello World",
			ID:              computeId,
			JobQueue:        "docker-local",
			Events:          eventGenerator,
			ComputeProvider: computeProvider,
		}

		//start the compute
		err := compute.Run()
		if err != nil {
			fmt.Println(err)
		}
	}

	time.Sleep(500 * time.Second)
	fmt.Println("Finished")
}

func TestDockerHelloWorld(t *testing.T) {
	//first create the Docker Compute Provider with no configuration parameters
	//  no params will limit concurrencty to 1
	computeProvider := NewDockerComputeProvider(DockerComputeProviderConfig{})

	//create a plugin referencing the docker hello world image
	plugin := &Plugin{
		Name:        "DockerHelloWorld",
		ImageAndTag: "hello-world:latest",
		Command:     []string{"/hello"},
		ComputeEnvironment: PluginComputeEnvironment{
			VCPU:   "1",
			Memory: "512",
		},
	}

	//when using the docker provider, you will need to register the plugin each time you run
	computeProvider.RegisterPlugin(plugin)

	//start the manifest
	manifestId := uuid.New()

	cm := ComputeManifest{
		ManifestName:     "HelloWorld",
		ManifestID:       manifestId,
		PluginDefinition: "DockerHelloWorld",
	}

	computeId := uuid.New()

	//use a list event generator with a single event
	eventGenerator := NewEventList([]Event{
		{
			ID:              uuid.New(),
			EventIdentifier: "1",
			Manifests:       []ComputeManifest{cm},
		},
	})

	//set up the compute run
	compute := CloudCompute{
		Name:            "Run Docker Hello World",
		ID:              computeId,
		JobQueue:        "docker-local",
		Events:          eventGenerator,
		ComputeProvider: computeProvider,
	}

	//start the compute
	err := compute.Run()
	if err != nil {
		fmt.Println(err)
	}

	//track status
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
