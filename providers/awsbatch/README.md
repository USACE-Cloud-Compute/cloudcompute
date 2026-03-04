# Cloud Compute AWS Batch Compute Provider

The cloud compute aws batch provider is for production compute in AWS. This provider implements the CloudCompute interface for AWS Batch, enabling job submission, monitoring, and management within the AWS Batch compute environment.

## Overview

The AWS Batch compute provider allows you to submit and manage compute jobs using AWS Batch. It provides integration with AWS services including:
- AWS Batch for job execution
- CloudWatch Logs for job logging
- IAM for authentication and authorization

## Getting Started

### Prerequisites

Before using the AWS Batch provider, ensure you have:
- AWS credentials configured (via AWS CLI, environment variables, or IAM roles)
- An AWS Batch compute environment and job queue created
- Required IAM permissions for AWS Batch operations

### Creating a Provider Instance

```golang
// Create a provider input with required configuration
cpi := awsbatch.NewAwsBatchProviderInput(
    "arn:aws-us-gov:iam::00000000009:role/ecsTaskExecutionRole",
    "us-east-1",
    "prod_compute",
)

// Create the AWS Batch provider instance
computeProvider, err := awsbatch.NewAwsBatchProvider(cpi)
if err != nil {
    log.Fatal(err)
}
```

## Provider Configuration

### AwsBatchProviderInput

The `AwsBatchProviderInput` struct contains all necessary configuration for creating an AWS Batch provider:

```golang
type AwsBatchProviderInput struct {
    // ExecutionRole is the ARN of the IAM role that AWS Batch will assume to run jobs
    ExecutionRole string

    // BatchRegion is the AWS region where the Batch service is located
    BatchRegion string

    // ConfigProfile is the AWS configuration profile to use (optional)
    ConfigProfile string

    // Options are additional AWS configuration options
    Options []func(o *config.LoadOptions) error
}
```

## Core Methods

### SubmitJobs

Submits one or more jobs to AWS Batch:

```golang
func (abp *AwsBatchProvider) SubmitJobs(event SubmitJobsInput) error
```

The `SubmitJobsInput` contains a list of jobs to submit and a map for tracking submission IDs.

### RegisterPlugin

Registers a plugin with AWS Batch by creating a job definition:

```golang
func (abp *AwsBatchProvider) RegisterPlugin(plugin *Plugin) (PluginRegistrationOutput, error)
```

### UnregisterPlugin

Removes a plugin from AWS Batch by deregistering its job definition:

```golang
func (abp *AwsBatchProvider) UnregisterPlugin(nameAndRevision string) error
```

### TerminateJobs

Terminates jobs submitted to AWS Batch job queues:

```golang
func (abp *AwsBatchProvider) TerminateJobs(input TerminateJobInput) error
```

### QueueSummary

Retrieves a summary of all jobs in a specific queue:

```golang
func (abp *AwsBatchProvider) QueueSummary(jobQueue string, query JobsSummaryQuery) error
```

### Status

Retrieves job status information based on a query:

```golang
func (abp *AwsBatchProvider) Status(jobQueue string, query JobsSummaryQuery) error
```

### JobLog

Retrieves logs for a specific job:

```golang
func (abp *AwsBatchProvider) JobLog(submittedJobId string, token *string) (JobLogOutput, error)
```

## Job Structure

Jobs submitted to AWS Batch are represented by the `Job` struct:

```golang
type Job struct {
    ID                   uuid.UUID
    EventID              uuid.UUID
    ManifestID           uuid.UUID
    PayloadID            uuid.UUID
    JobName              string
    JobQueue             string
    JobDefinition        string
    ContainerOverrides   ContainerOverrides
    DependsOn            []string
    ManifestDependencies []uuid.UUID
    Parameters           map[string]string
    Tags                 map[string]string
    RetryAttemts         int32
    JobTimeout           int32            //duration in seconds
    SubmittedJob         *SubmitJobResult //reference to the job information from the compute environment
}
```

## Error Handling

The AWS Batch provider returns standard Go errors for all operations. Common error scenarios include:
- Invalid AWS credentials
- Non-existent job queues or job definitions
- Permission denied errors
- Network connectivity issues

## Example Usage

```golang
// Create provider
cpi := awsbatch.NewAwsBatchProviderInput(
    "arn:aws:iam::123456789012:role/ExecutionRole",
    "us-west-2",
    "",
)
provider, err := awsbatch.NewAwsBatchProvider(cpi)
if err != nil {
    log.Fatal(err)
}

// Submit jobs
jobs := []*Job{
    {
        JobName:       "my-job",
        JobQueue:      "my-queue",
        JobDefinition: "my-definition",
        ContainerOverrides: ContainerOverrides{
            Command: []string{"/bin/sh", "-c", "echo hello"},
        },
    },
}
submitInput := SubmitJobsInput{
    Jobs: jobs,
    SubmissionIdMap: make(map[uuid.UUID]string),
}
err = provider.SubmitJobs(submitInput)
if err != nil {
    log.Printf("Error submitting jobs: %v", err)
}
```

## Testing

The AWS Batch provider includes comprehensive tests that mock the underlying AWS services. Tests cover:
- Provider creation and configuration
- Job submission
- Plugin registration and unregistration
- Job log retrieval
- Error handling scenarios

## AWS Integration Details

### IAM Requirements

The execution role specified in `AwsBatchProviderInput` must have the following permissions:
- `batch:SubmitJob`
- `batch:RegisterJobDefinition`
- `batch:DeregisterJobDefinition`
- `batch:TerminateJob`
- `batch:CancelJob`
- `batch:ListJobs`
- `batch:DescribeJobs`
- `logs:GetLogEvents`

### CloudWatch Logs

Job logs are stored in CloudWatch Logs under the `/aws/batch/job` log group. The provider automatically retrieves logs for submitted jobs.

## Performance Considerations

- The provider uses AWS SDK v2 for efficient API calls
- Retry strategies are configured for resilient operation
- Connection pooling is handled by the AWS SDK
- Consider using batch operations for submitting multiple jobs

## Security

- Uses AWS IAM roles for authentication
- Supports AWS configuration profiles for different environments
- All AWS API calls are made over HTTPS
- Credentials are managed by the AWS SDK, not stored in the provider

## Troubleshooting

### Common Issues

1. **Authentication failures**: Ensure AWS credentials are properly configured
2. **Invalid job definitions**: Verify job definitions exist in AWS Batch
3. **Permission denied**: Check IAM role permissions
4. **Network connectivity**: Verify AWS region and connectivity

### Logging

The provider logs detailed information about job submission and errors to help with debugging.

## License

This provider is part of the Cloud Compute project and follows the project's licensing terms.