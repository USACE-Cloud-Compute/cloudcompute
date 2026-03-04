# Cloud Compute Docker Provider

The Cloud Compute Docker Provider is a local development implementation that allows running compute jobs using Docker containers. It's designed to provide a lightweight, local alternative to cloud-based compute providers like AWS Batch, making it ideal for development, testing, and local execution of compute jobs.

## Overview

The Docker compute provider enables developers to run compute jobs locally using Docker containers. It supports:
- Concurrent execution of jobs with configurable limits
- Plugin-based job definitions using Docker images
- Job dependency management
- Job monitoring and status tracking
- Secrets management for secure environment variables
- Integration with the Cloud Compute framework's standard interfaces

## Architecture

The Docker compute provider is built on several core components:

1. **DockerComputeProvider**: The main interface that implements the ComputeProvider interface
2. **DockerComputeManager**: Manages job queuing, execution, and concurrency control
3. **DockerJobRunner**: Executes individual jobs using Docker
4. **PluginRegistry**: Stores and retrieves job definitions (plugins)
5. **SecretsManager**: Handles secure environment variable management

## Usage

### Creating a Docker Compute Provider

```go
// Create a provider with default configuration
computeProvider := NewDockerComputeProvider(DockerComputeProviderConfig{})

// Create a provider with custom concurrency
computeProvider := NewDockerComputeProvider(DockerComputeProviderConfig{
    Concurrency: 4,
})

// Create a provider with a secrets manager
sm := NewSecretManager("")
sm.AddSecret("arn:local:secretsmanager:secret:prod/CloudCompute/ASDFASDF:AWS_ACCESS_KEY_ID::", "ASFASDFASDFASDF")
sm.AddSecret("arn:local:secretsmanager:secret:prod/CloudCompute/ASDFASDF:AWS_SECRET_ACCESS_KEY::", "ASDFFASDFASDFASDF")

computeProvider2 := NewDockerComputeProvider(DockerComputeProviderConfig{
    Concurrency:    1,
    SecretsManager: sm,
})
```

### Registering Plugins

Plugins define the Docker images and execution parameters for jobs:

```go
// Register a plugin
plugin := &Plugin{
    Name:        "TestPlugin",
    ImageAndTag: "hello-world:latest",
    Command:     []string{"/hello"},
    ComputeEnvironment: PluginComputeEnvironment{
        VCPU:   "1",
        Memory: "512",
    },
}

_, err := provider.RegisterPlugin(plugin)
```

### Submitting Jobs

Jobs are submitted using the SubmitJobs method:

```go
// Create a job
job := &Job{
    ID:            uuid.New(),
    EventID:       uuid.New(),
    ManifestID:    manifestId,
    PayloadID:     uuid.New(),
    JobName:       "test-job",
    JobQueue:      "docker-local",
    JobDefinition: "TestPlugin",
    ContainerOverrides: ContainerOverrides{
        Environment: KeyValuePairs{
            {Name: "TEST_VAR", Value: "test_value"},
        },
    },
}

// Submit the job
input := SubmitJobsInput{
    Jobs: []*Job{job},
}

err := provider.SubmitJobs(input)
```

### Job Dependencies

Jobs can depend on other jobs using the DependsOn field:

```go
job2 := &Job{
    ID:            uuid.New(),
    EventID:       uuid.New(),
    ManifestID:    manifestId2,
    PayloadID:     uuid.New(),
    JobName:       "test-job-2",
    JobQueue:      "docker-local",
    JobDefinition: "TestPlugin2",
    DependsOn:     []string{manifestId1.String()}, // job2 depends on job1
    ContainerOverrides: ContainerOverrides{
        Environment: KeyValuePairs{
            {Name: "TEST_VAR", Value: "test_value_2"},
        },
    },
}
```

## Configuration Options

The DockerComputeProviderConfig struct accepts the following configuration options:

- **Concurrency**: Number of concurrent containers to allow on the host (default: 1)
- **StartMonitor**: If greater than 0, starts a monitor job with the specified interval in seconds
- **MonitorFunction**: Optional custom monitor function (default prints to StdOut)
- **SecretsManager**: Optional in-memory secrets manager for secure environment variables
- **DockerPullProgressFactory**: Optional factory for Docker pull progress UI instances

## Features

### Concurrency Control
The provider supports configurable concurrency limits to control how many Docker containers run simultaneously. This helps manage system resources and prevent overloading.

### Job Monitoring
A built-in monitoring system can track job status and provide periodic updates. You can customize the monitoring interval and function.

### Secrets Management
The provider supports secrets management for secure environment variable handling, allowing you to inject sensitive information into containers.

### Plugin System
Plugins define the Docker images and execution parameters for jobs, enabling flexible job definitions that can be reused across multiple jobs.

### Dependency Management
Jobs can specify dependencies on other jobs, enabling complex workflows with proper execution ordering.

## Methods

### SubmitJobs
Submits one or more jobs for execution. The jobs are queued and executed according to the configured concurrency limit.

### TerminateJobs
Terminates jobs based on a query or list of job identifiers.

### Status
Retrieves job summaries based on a query, providing information about job status and execution.

### JobLog
Retrieves logs for a specific job (currently not implemented in Docker provider).

### RegisterPlugin
Registers a plugin definition that can be referenced by jobs.

### UnregisterPlugin
Removes a plugin from the registry.

## Example Usage

```go
package main

import (
    "github.com/usace-cloud-compute/cloudcompute"
    "github.com/google/uuid"
)

func main() {
    // Create a Docker compute provider with concurrency of 2
    provider := cloudcompute.NewDockerComputeProvider(cloudcompute.DockerComputeProviderConfig{
        Concurrency: 2,
    })

    // Register a plugin
    plugin := &cloudcompute.Plugin{
        Name:        "HelloWorldPlugin",
        ImageAndTag: "hello-world:latest",
        Command:     []string{"/hello"},
        ComputeEnvironment: cloudcompute.PluginComputeEnvironment{
            VCPU:   "1",
            Memory: "512",
        },
    }

    _, err := provider.RegisterPlugin(plugin)
    if err != nil {
        panic(err)
    }

    // Create and submit a job
    manifestId := uuid.New()
    job := &cloudcompute.Job{
        ID:            uuid.New(),
        EventID:       uuid.New(),
        ManifestID:    manifestId,
        PayloadID:     uuid.New(),
        JobName:       "hello-world-job",
        JobQueue:      "docker-local",
        JobDefinition: "HelloWorldPlugin",
        ContainerOverrides: cloudcompute.ContainerOverrides{
            Environment: cloudcompute.KeyValuePairs{
                {Name: "ENV_VAR", Value: "value"},
            },
        },
    }

    input := cloudcompute.SubmitJobsInput{
        Jobs: []*cloudcompute.Job{job},
    }

    err = provider.SubmitJobs(input)
    if err != nil {
        panic(err)
    }
}
```

## Testing

The Docker compute provider includes comprehensive tests covering:
- Provider creation and configuration
- Job submission with and without dependencies
- Plugin registration
- Job termination
- Status queries
- Concurrent job execution

Tests can be run using standard Go testing commands:
```bash
cd providers/docker
go test -v
```

## Limitations

- This provider is designed for local development and testing
- Not suitable for production workloads
- Limited to Docker container execution
- Some methods (like JobLog) are not fully implemented