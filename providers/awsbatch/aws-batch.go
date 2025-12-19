// Package awsbatch provides an implementation of the CloudCompute provider interface
// for AWS Batch.
package awsbatch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	. "github.com/usace-cloud-compute/cc-go-sdk"
	. "github.com/usace-cloud-compute/cloudcompute"
)

// awsLogGroup is the CloudWatch log group name used for AWS Batch job logs
var awsLogGroup string = "/aws/batch/job"
var ctx context.Context = context.Background()

//options are any set of valid AWS Batch config options.
//for example, to set max retries to unlimited:
/*
	input.Options=[]func(o *config.LoadOptions) error{
		config.WithRetryer(func() aws.Retryer {
			return retry.AddWithMaxAttempts(retry.NewStandard(), 0)
		}))
	}
*/

// AwsBatchProviderInput contains configuration options for creating an AWS Batch provider
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

// AwsBatchProvider implements the CloudCompute provider interface for AWS Batch
type AwsBatchProvider struct {
	// client is the AWS Batch service client
	client *batch.Client
	// logs is the CloudWatch Logs service client for retrieving job logs
	logs *cloudwatchlogs.Client
	// executionRole is the IAM role ARN used for job execution
	executionRole string
}

// NewAwsBatchProviderInput creates a new AwsBatchProviderInput with default retry configuration
func NewAwsBatchProviderInput(executionRole string, batchRegion string, configProfile string) AwsBatchProviderInput {
	return AwsBatchProviderInput{
		ExecutionRole: executionRole,
		BatchRegion:   batchRegion,
		ConfigProfile: configProfile,
		Options: []func(o *config.LoadOptions) error{
			config.WithRetryer(func() aws.Retryer {
				return retry.AddWithMaxAttempts(retry.NewStandard(), 0)
			}),
		},
	}
}

// NewAwsBatchProvider creates a new AWS Batch provider instance
// It initializes the AWS Batch and CloudWatch Logs clients
func NewAwsBatchProvider(input AwsBatchProviderInput) (*AwsBatchProvider, error) {

	options := []func(o *config.LoadOptions) error{
		config.WithRegion(input.BatchRegion),
	}

	if input.ConfigProfile != "" {
		options = append(options, config.WithSharedConfigProfile(input.ConfigProfile))
	}

	if len(input.Options) > 0 {
		options = append(options, input.Options...)
	}
	cfg, err := config.LoadDefaultConfig(context.TODO(), options...)

	if err != nil {
		log.Println("Failed to load an AWS Config")
		return nil, err
	}

	svc := batch.NewFromConfig(cfg)
	logs := cloudwatchlogs.NewFromConfig(cfg)

	return &AwsBatchProvider{svc, logs, input.ExecutionRole}, nil
}

// SubmitJob submits a job to AWS Batch
// It takes a Job struct and returns an error if submission fails
func (abp *AwsBatchProvider) SubmitJob(job *Job) error {
	var retryStrategy *types.RetryStrategy
	var timeout *types.JobTimeout

	if job.RetryAttemts > 0 {
		retryStrategy = &types.RetryStrategy{Attempts: &job.RetryAttemts}
	}

	if job.JobTimeout > 0 {
		timeout = &types.JobTimeout{AttemptDurationSeconds: &job.JobTimeout}
	}

	input := &batch.SubmitJobInput{
		JobDefinition:      &job.JobDefinition,
		JobName:            &job.JobName,
		JobQueue:           &job.JobQueue,
		DependsOn:          toBatchDependency(job.DependsOn),
		ContainerOverrides: toBatchContainerOverrides(job.ContainerOverrides),
		Parameters:         job.Parameters,
		Tags:               job.Tags,
		RetryStrategy:      retryStrategy,
		Timeout:            timeout,
	}

	submitResult, err := abp.client.SubmitJob(ctx, input)
	if err != nil {
		log.Printf("Failed to submit batch job: %s using definition %s on queue %s.\n", job.JobName, job.JobDefinition, job.JobQueue)
		return err
	}

	job.SubmittedJob = &SubmitJobResult{
		JobId:        submitResult.JobId,
		ResourceName: submitResult.JobArn,
	}

	return nil
}

// RegisterPlugin registers a plugin with AWS Batch
// It creates a job definition for the plugin and returns a PluginRegistrationOutput
func (abp *AwsBatchProvider) RegisterPlugin(plugin *Plugin) (PluginRegistrationOutput, error) {
	var timout *types.JobTimeout
	if plugin.ExecutionTimeout != nil {
		timout = &types.JobTimeout{AttemptDurationSeconds: plugin.ExecutionTimeout}
	}
	input := &batch.RegisterJobDefinitionInput{
		JobDefinitionName: &plugin.Name,
		Type:              types.JobDefinitionTypeContainer,
		ContainerProperties: &types.ContainerProperties{
			Command:          plugin.Command,
			Environment:      kvpToBatchKvp(plugin.DefaultEnvironment),
			ExecutionRoleArn: &abp.executionRole,
			Image:            &plugin.ImageAndTag,
			Privileged:       &plugin.Privileged,
			LinuxParameters:  toBatchLinuxParams(&plugin.LinuxParameters),

			//MountPoints:      volumesToBatch(plugin.Volumes),
			//Volumes
			ResourceRequirements: []types.ResourceRequirement{
				{
					Type:  types.ResourceTypeMemory,
					Value: &plugin.ComputeEnvironment.Memory,
				},
				{
					Type:  types.ResourceTypeVcpu,
					Value: &plugin.ComputeEnvironment.VCPU,
				},
			},
			Secrets: credsToBatchSecrets(plugin.Credentials),
		},
		Timeout: timout,
	}
	output, err := abp.client.RegisterJobDefinition(ctx, input)
	pro := PluginRegistrationOutput{}
	if err == nil {
		pro = PluginRegistrationOutput{
			Name:         *output.JobDefinitionName,
			ResourceName: *output.JobDefinitionArn,
			Revision:     *output.Revision,
		}
	}
	return pro, err
}

// UnregisterPlugin removes a plugin from AWS Batch
// It deregisters the job definition with the given name and revision
func (abp *AwsBatchProvider) UnregisterPlugin(nameAndRevision string) error {
	dji := batch.DeregisterJobDefinitionInput{
		JobDefinition: &nameAndRevision,
	}
	_, err := abp.client.DeregisterJobDefinition(ctx, &dji)
	log.Printf("Unable to deregister AWS Batch Job: %s.  Error: %s\n", nameAndRevision, err)
	return err
}

// TerminateJobs terminates jobs submitted to AWS Batch job queues
// It takes a TerminateJobInput and terminates jobs based on the input criteria
func (abp *AwsBatchProvider) TerminateJobs(input TerminateJobInput) error {
	jobs := input.VendorJobs
	if jobs == nil {
		input.Query.JobSummaryFunction = func(summaries []JobSummary) {
			for _, job := range summaries {
				switch job.Status {
				case string(types.JobStatusStarting),
					string(types.JobStatusRunning):
					output := abp.terminateJob(job.JobName, job.JobId, input.Reason)
					if input.TerminateJobFunction != nil {
						input.TerminateJobFunction(output)
					}
				case string(types.JobStatusSubmitted),
					string(types.JobStatusRunnable):
					output := abp.cancelJob(job.JobName, job.JobId, input.Reason)
					if input.TerminateJobFunction != nil {
						input.TerminateJobFunction(output)
					}
				}
			}
		}
		statuserr := abp.Status(input.JobQueue, input.Query)
		if statuserr != nil {
			return statuserr
		}
	} else {
		for _, job := range jobs {
			output := abp.terminateJob(job.Name(), job.ID(), input.Reason)
			if input.TerminateJobFunction != nil {
				input.TerminateJobFunction(output)
			}
		}
		return nil
	}
	return nil
}

// TerminateQueue terminates all jobs in a specific queue
// It takes a TerminateJobInput and terminates all jobs in the specified queue
func (abp *AwsBatchProvider) TerminateQueue(input TerminateJobInput) error {

	input.Query.JobSummaryFunction = func(summaries []JobSummary) {
		for _, job := range summaries {
			output := abp.terminateJob(job.JobName, job.JobId, input.Reason)
			if input.TerminateJobFunction != nil {
				input.TerminateJobFunction(output)
			}
		}
	}
	statuserr := abp.QueueSummary(input.JobQueue, input.Query)
	if statuserr != nil {
		return statuserr
	}

	return nil
}

// terminateJob terminates a specific job
// It takes the job name, job ID, and reason for termination
func (abp *AwsBatchProvider) terminateJob(name string, id string, reason string) TerminateJobOutput {
	tji := batch.TerminateJobInput{
		JobId:  &id,
		Reason: &reason,
	}
	_, err := abp.client.TerminateJob(context.TODO(), &tji)

	return TerminateJobOutput{
		JobName: name,
		JobId:   id,
		Err:     err,
	}
}

// cancelJob cancels a specific job
// It takes the job name, job ID, and reason for cancellation
func (abp *AwsBatchProvider) cancelJob(name string, id string, reason string) TerminateJobOutput {
	tji := batch.CancelJobInput{
		JobId:  &id,
		Reason: &reason,
	}
	_, err := abp.client.CancelJob(context.TODO(), &tji)

	return TerminateJobOutput{
		JobName: name,
		JobId:   id,
		Err:     err,
	}
}

// QueueSummary retrieves a summary of all jobs in a specific queue
// It takes a job queue name and a JobsSummaryQuery to filter results
func (abp *AwsBatchProvider) QueueSummary(jobQueue string, query JobsSummaryQuery) error {
	if query.JobSummaryFunction == nil {
		return errors.New("missing jobsummary function, you have no way to process the result")
	}

	var nextToken *string

	statusList := []types.JobStatus{
		types.JobStatusSubmitted,
		types.JobStatusPending,
		types.JobStatusRunnable,
		types.JobStatusStarting,
		types.JobStatusRunning,
	}
	for _, status := range statusList {
		for {
			input := batch.ListJobsInput{
				JobQueue:  &jobQueue,
				JobStatus: status,
				NextToken: nextToken,
			}

			output, err := abp.client.ListJobs(ctx, &input)
			if err != nil {
				return err
			}
			query.JobSummaryFunction(listOutput2JobSummary(output))
			nextToken = output.NextToken
			if nextToken == nil {
				break
			}
		}
	}
	return nil
}

// Status retrieves job status information based on a query
// It takes a job queue name and a JobsSummaryQuery to filter results
func (abp *AwsBatchProvider) Status(jobQueue string, query JobsSummaryQuery) error {
	if query.JobSummaryFunction == nil {
		return errors.New("missing JubSummaryFunction, you have no way to process the result")
	}

	var queryString string
	switch query.QueryLevel {
	case SUMMARY_COMPUTE:
		queryString = fmt.Sprintf("%s_c_%s*", CcProfile, query.QueryValue.Compute)
	case SUMMARY_EVENT:
		queryString = fmt.Sprintf("%s_c_%s_e_%s*", CcProfile, query.QueryValue.Compute, query.QueryValue.Event)
	case SUMMARY_JOB:
		queryString = fmt.Sprintf("%s_c_%s_e_%s_j_%s", CcProfile, query.QueryValue.Compute, query.QueryValue.Event, query.QueryValue.Job)
	}

	eventFilter := types.KeyValuesPair{
		Name:   aws.String("JOB_NAME"),
		Values: []string{queryString},
	}

	var nextToken *string

	for {
		input := batch.ListJobsInput{
			JobQueue:  &jobQueue,
			Filters:   []types.KeyValuesPair{eventFilter},
			NextToken: nextToken,
		}

		output, err := abp.client.ListJobs(ctx, &input)
		if err != nil {
			return err
		}
		query.JobSummaryFunction(listOutput2JobSummary(output))
		nextToken = output.NextToken
		if nextToken == nil {
			break
		}
	}
	return nil
}

// JobLog retrieves logs for a specific job
// It takes a submitted job ID and an optional token for pagination
// @TODO this assumes the logs are rather short.
// Need to update for logs that require pagenation in the AWS SDK
func (abp *AwsBatchProvider) JobLog(submittedJobId string, token *string) (JobLogOutput, error) {

	//allow zero value in the call but set as nil for AWS
	if token != nil && *token == "" {
		token = nil
	}

	jobDesc, err := abp.describeBatchJobs([]string{submittedJobId})
	if err != nil {
		return JobLogOutput{}, err
	}
	var limit int32 = 1000
	cfg := cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  &awsLogGroup,
		LogStreamName: jobDesc.Jobs[0].Container.LogStreamName,
		Limit:         &limit,
		StartFromHead: aws.Bool(true),
		NextToken:     token,
	}
	logevents, err := abp.logs.GetLogEvents(ctx, &cfg)
	if err != nil {
		return JobLogOutput{}, err
	}

	if logevents == nil {

		return JobLogOutput{Logs: []string{"No logs"}}, nil
	}

	out := make([]string, len(logevents.Events))
	for i, v := range logevents.Events {
		t := time.Unix(*v.Timestamp/1000, 0)
		out[i] = fmt.Sprintf("%v: %s", t, *v.Message)
	}

	return JobLogOutput{
		Logs:  out,
		Token: logevents.NextForwardToken,
	}, nil
}

// func (abp *AwsBatchProvider) listBatchJob(job *Job) (*batch.ListJobsOutput, error) {
// 	input := batch.ListJobsInput{
// 		JobQueue:  &job.JobQueue,
// 		JobStatus: types.JobStatusSucceeded,
// 	}

// 	return abp.client.ListJobs(ctx, &input)
// }

// describeBatchJobs describes the specified batch jobs
// It takes a slice of job IDs and returns the job descriptions
func (abp *AwsBatchProvider) describeBatchJobs(submittedJobIds []string) (*batch.DescribeJobsOutput, error) {
	input := batch.DescribeJobsInput{
		Jobs: submittedJobIds,
	}
	return abp.client.DescribeJobs(ctx, &input)
}

// toBatchContainerOverrides converts ContainerOverrides to AWS Batch ContainerOverrides
func toBatchContainerOverrides(co ContainerOverrides) *types.ContainerOverrides {

	awskvp := make([]types.KeyValuePair, len(co.Environment))
	awsrr := make([]types.ResourceRequirement, len(co.ResourceRequirements))

	for i, kvp := range co.Environment {
		kvpl := kvp //make local copy of kvp to avoid aws pointing to a single kvp reference
		awskvp[i] = types.KeyValuePair{
			Name:  &kvpl.Name,
			Value: &kvpl.Value,
		}
	}

	for i, rr := range co.ResourceRequirements {
		rrl := rr
		awsrr[i] = types.ResourceRequirement{
			Type:  types.ResourceType(rrl.Type),
			Value: &rrl.Value,
		}
	}

	return &types.ContainerOverrides{
		Command:              co.Command,
		Environment:          awskvp,
		ResourceRequirements: awsrr,
	}
}

// toBatchDependency converts job dependency strings to AWS Batch JobDependency
func toBatchDependency(jobDependency []string) []types.JobDependency {
	batchDeps := make([]types.JobDependency, len(jobDependency))
	for i, d := range jobDependency {
		depCopy := d
		batchDeps[i] = types.JobDependency{
			JobId: &depCopy,
		}
	}
	return batchDeps
}

// listOutput2JobSummary converts AWS Batch ListJobsOutput to JobSummary slice
func listOutput2JobSummary(output *batch.ListJobsOutput) []JobSummary {
	js := make([]JobSummary, len(output.JobSummaryList))
	for i, s := range output.JobSummaryList {
		js[i] = JobSummary{
			JobId:        *s.JobId,
			JobName:      *s.JobName,
			CreatedAt:    s.CreatedAt,
			StartedAt:    s.StartedAt,
			Status:       string(s.Status),
			StatusDetail: s.StatusReason,
			StoppedAt:    s.StoppedAt,
			ResourceName: *s.JobArn,
		}
	}
	return js
}

// kvpToBatchKvp converts KeyValuePair slice to AWS Batch KeyValuePair slice
func kvpToBatchKvp(kvps []KeyValuePair) []types.KeyValuePair {
	bkvps := make([]types.KeyValuePair, len(kvps))
	for i, kvp := range kvps {
		name, value := kvp.Name, kvp.Value
		bkvps[i] = types.KeyValuePair{
			Name:  &name,
			Value: &value,
		}
	}
	return bkvps
}

// credsToBatchSecrets converts KeyValuePair slice to AWS Batch Secret slice
func credsToBatchSecrets(creds []KeyValuePair) []types.Secret {
	secrets := make([]types.Secret, len(creds))
	for i, s := range creds {
		nlocal, vlocal := s.Name, s.Value
		secrets[i] = types.Secret{
			Name:      &nlocal,
			ValueFrom: &vlocal,
		}
	}
	return secrets
}

// toBatchLinuxParams converts LinuxParameters to AWS Batch LinuxParameters
func toBatchLinuxParams(lp *LinuxParameters) *types.LinuxParameters {

	if len(lp.Devices) == 0 {
		return nil
	}
	bds := []types.Device{}
	for _, v := range lp.Devices {
		bds = append(bds, types.Device{
			HostPath:      v.HostPath,
			ContainerPath: v.ContainerPath,
		})
	}
	return &types.LinuxParameters{
		Devices: bds,
	}
}

// paramsMapToKvp converts a map to AWS Batch KeyValuePair slice
func paramsMapToKvp(params map[string]string) []types.KeyValuePair {
	pout := make([]types.KeyValuePair, len(params))
	i := 0
	for k, v := range params {
		klocal, vlocal := k, v
		pout[i] = types.KeyValuePair{
			Name:  &klocal,
			Value: &vlocal,
		}
		i++
	}
	return pout
}

/*
func volumesToBatch(volumes []PluginComputeVolumes) ([]types.MountPoint, []types.Volume) {
	mps := make([]types.MountPoint, len(volumes))
	bvs:=make([]types.Volume,len(volumes))
	for i, v := range volumes {
		mps[i] = types.MountPoint{
			ContainerPath: &v.MountPoint,
			ReadOnly:      &v.ReadOnly,
			SourceVolume:  &v.ResourceName,
		}
		bvs[i]=types.Volume{
			Name: &v.Name,
			EfsVolumeConfiguration: ,
		}
	}
	return mps
}
*/
