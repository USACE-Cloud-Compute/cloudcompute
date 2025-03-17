## Container Compute Docker Provider
The container compute docker provider is intended to support local development of plugins.  

```golang
//create a provider with a job execution concurrency of 4:
computeProvider := NewDockerComputeProvider(DockerComputeProviderConfig{
	Concurrency: 4,
})

//create a provider with a secrets manager
sm := NewSecretManager("")

sm.AddSecret("arn:local:secretsmanager:secret:prod/CloudCompute/ASDFASDF:AWS_ACCESS_KEY_ID::", "ASFASDFASDFASDF")
sm.AddSecret("arn:local:secretsmanager:secret:prod/CloudCompute/ASDFASDF:AWS_SECRET_ACCESS_KEY::", "ASDFFASDFASDFASDF")

computeProvider2 := NewDockerComputeProvider(DockerComputeProviderConfig{
    Concurrency:    1,
    SecretsManager: sm,
})

//once the compute provider is instantiated, create a Plugin
//this is a simple plugin to run the docker hello world image
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

//create a manifest
cm := ComputeManifest{
    ManifestName:     "HelloWorld",
    ManifestID:       uuid.New(),
    PluginDefinition: "DockerHelloWorld",
}

//create an Event Generator
//use a list event generator with a single event and single manifest in the DAG
eventGenerator := NewEventList([]Event{
    {
        ID:              uuid.New(),
        EventIdentifier: "1",
        Manifests:       []ComputeManifest{cm},
    },
})


//set up the compute run
computeId := uuid.New()
compute := CloudCompute{
    Name:            "Run Docker Hello World",
    ID:              computeId,
    JobQueue:        "docker-local",
    Events:          eventGenerator,
    ComputeProvider: computeProvider,
}

//track status every second and shutdown when the job has completed
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

```
