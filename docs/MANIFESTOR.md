One way to execute plugins in the cloud compute environment is to write a manifestor using golang and the containercompute library.  As long as you can use golang, this is a very simple way to run plugins.

Our first example will be a sample that runs the docker hello world using a local docker compute provider:

```golang

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	. "github.com/usace/cloudcompute"
	. "github.com/usace/cloudcompute/providers/docker"
)

func CloudComputeHelloWorld() {
    
    //first create the Docker Compute Provider with no configuration parameters
    //  no params will limit concurrency to 1
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

    //poll status and shut down when all jobs are complete
    jobsRunning := true
    for jobsRunning {
        //sleep one second
        time.Sleep(2 * time.Second)

        //request a compute job summary
        compute.Status(JobsSummaryQuery{
            QueryLevel: "COMPUTE",
            QueryValue: JobNameParts{
                Compute: compute.ID.String(),
            },
            //provide a summary function that loops over the jobs in the compute
            //and if all have stopped running, then exit the polling loop
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

```

Lets run the same plugin in AWS Batch.  First you will need the following environment

```bash
#ACCESS KEY FOR THE CC STORE
CC_AWS_ACCESS_KEY_ID = <your key>

#SECRET KEY FOR THE CC STORE
CC_AWS_SECRET_ACCESS_KEY = <your secret key>

#REGION FOR THE CC STORE
CC_AWS_DEFAULT_REGION = us-east-1

#BUCKET NAME FOR THE CC STORE
CC_AWS_S3_BUCKET = cloud-compute

#OPTIONAL ROOT PREFIX FOR THE CC STORE
CC_ROOT = cc_store
```
When using AWS Batch you would only "register" (effectively creating a Batch Job Definition) your plugin once.
For the sake of consistency we will register it, so the changes to the previous hello world will look like this.

```golang

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	. "github.com/usace/cloudcompute"
	. "github.com/usace/cloudcompute/providers/awsbatch"
)

func CloudComputeAwsBatchHelloWorld() {
    
    //first create the AWS Batch Compute Provider
    cpi := cloudcompute.NewAwsBatchProviderInput(
		"arn:aws-us-gov:iam::000000000000:role/ecsTaskExecutionRole",
		"us-east-1",
		"ffrd_compute",
	)
	computeProvider, err := cloudcompute.NewAwsBatchProvider(cpi)
	if err != nil {
		log.Fatal(err)
	}

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

    //poll status and shut down when all jobs are complete
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
```

These examples were extremely simple and do not use 



