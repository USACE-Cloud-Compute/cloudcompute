# k8sargo Compute Provider

The k8sargo compute provider is an implementation of the CloudCompute framework that uses Argo Workflows on Kubernetes to execute compute jobs. This provider allows users to submit jobs that are executed as Argo Workflow DAGs, leveraging Kubernetes for orchestration and resource management.

## Overview

The k8sargo compute provider bridges the CloudCompute framework with Argo Workflows, enabling the execution of compute jobs on Kubernetes clusters. It translates CloudCompute job definitions into Argo Workflow templates and executes them as DAGs, supporting job dependencies, resource requirements, and environment variables.

## Architecture

The provider consists of two main files:

1. `k8sargo.go` - Main implementation containing the `ArgoWorkflowComputeProvider` struct and its methods
2. `spec-overrides.go` - Helper functions for handling pod spec patching and JSON substitution

## Key Components

### ArgoWorkflowComputeProvider Struct

```go
type ArgoWorkflowComputeProvider struct {
    client        apiclient.Client
    serviceClient workflowpkg.WorkflowServiceClient
    ctx           context.Context
    namespace     *string
}
```

### Main Methods

#### `NewArgoWorkflowComputeProvider(config ArgoWorkflowComputeProviderConfig) (*ArgoWorkflowComputeProvider, error)`

Creates a new Argo Workflow compute provider instance. It initializes the connection to the Argo Server using command-line flags or environment variables.

Configuration parameters:
- `ARGO_SERVER` - Argo Server address (default: localhost:2746)
- `ARGO_TOKEN` - Bearer token for authentication
- `namespace` - Kubernetes namespace for workflow execution (default: "argo")

#### `SubmitJob(input cc.SubmitJobInput) error`

Submits jobs to Argo Workflows as a DAG. This method:

1. Processes each job in the input to create Argo Workflow tasks
2. Retrieves or creates workflow templates for each job definition
3. Configures resource requirements (CPU, memory) for each task
4. Sets up environment variables and command overrides
5. Creates a workflow DAG with the appropriate dependencies
6. Submits the workflow to the Argo server

#### `RegisterPlugin(plugin *cc.Plugin) (cc.PluginRegistrationOutput, error)`

Registers a plugin with the Argo Workflow system by creating a workflow template. This method:

1. Converts the CloudCompute plugin definition into an Argo Workflow template
2. Sets up parameter substitution for CPU, memory, command, and environment variables
3. Creates a workflow template in the Argo server
4. Returns registration information including the template name and resource name

#### `UnregisterPlugin(nameAndRevision string) error`

Removes a workflow template from the Argo server.

#### `JobLog(submittedJobId string, token *string) (cc.JobLogOutput, error)`

Retrieves logs for a submitted job. This method opens a log stream from the Argo server and processes the log events.

## Configuration

The provider can be configured through:

1. Command-line flags:
   - `--argo-server` - Argo Server address
   - `--token` - Authentication token
   - `--namespace` - Kubernetes namespace
   - `--secure` - Enable TLS (default: true)
   - `--insecure-skip-verify` - Skip TLS certificate verification (default: true)

2. Environment variables:
   - `ARGO_SERVER` - Argo Server address
   - `ARGO_TOKEN` - Authentication token

## Resource Management

The provider supports dynamic resource management through:

- CPU and memory requirements defined in the job specifications
- Resource limits and requests in the pod spec patching system
- Parameter substitution for resource values in workflow templates

## Environment Variables

Environment variables are passed through to the workflow tasks using:
- `DagTaskEnv` parameter for task-specific environment variables
- Template-level environment variables from the plugin definition
- Default environment variables from the plugin

## Dependencies

The k8sargo provider depends on:
- Argo Workflows v3 API client
- Kubernetes client libraries
- CloudCompute framework

## Usage Example

```go
// Create a new Argo Workflow compute provider
config := k8sargo.ArgoWorkflowComputeProviderConfig{
    Namespace: "argo",
}
provider, err := k8sargo.NewArgoWorkflowComputeProvider(config)
if err != nil {
    // handle error
}

// Register a plugin
plugin := &cc.Plugin{
    Name: "my-plugin",
    ImageAndTag: "my-image:latest",
    Command: []string{"/app/run"},
    ComputeEnvironment: cc.PluginComputeEnvironment{
        VCPU: "1",
        Memory: "256",
    },
}
registration, err := provider.RegisterPlugin(plugin)
if err != nil {
    // handle error
}

// Submit jobs
jobInput := cc.SubmitJobInput{
    Jobs: []*cc.Job{
        {
            JobDefinition: registration.Name,
            ContainerOverrides: cc.ContainerOverrides{
                Environment: cc.KeyValuePairs{
                    {Name: "ENV1", Value: "value1"},
                },
                ResourceRequirements: []cc.ResourceRequirement{
                    {Type: cc.ResourceTypeVcpu, Value: "2"},
                    {Type: cc.ResourceTypeMemory, Value: "512Mi"},
                },
            },
        },
    },
}
err = provider.SubmitJob(jobInput)
```

## Limitations

1. Requires a running Argo Workflows server
2. Depends on Kubernetes cluster access
3. Uses specific Argo Workflow API versions
4. Environment variable handling requires careful configuration for security


todo:
 - large block support
   - how does argo map to batch with respect to instance types and queues
   - how can argo/k8s pods request a larger block chunk of the nodes resources, or do they share the same block fs?
     1) create a new node type
     2) determine how to execute on specific node types
 - volume mounts (NFS/CIFS)
   - cc.PluginComputeVolumes
   - how to mount a network file system?
 - volume mounts (Linux Device)
   - cc.LinuxParameters
   - how to mount a host volume (node) to a running container(pod)?
     - 1) create storage mount(fs) and mount rw to node
     - 2) if that is mounted, mount into a running pod