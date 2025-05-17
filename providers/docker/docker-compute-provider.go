package cloudcompute

import (
	"log"

	"github.com/google/uuid"
	. "github.com/usace/cloudcompute"
)

type DockerComputeProvider struct {
	manager  *DockerComputeManager
	registry PluginRegistry
	sm       SecretsManager
}

// DockerComputeProviderConfig holds the configuration for DockerComputeProvider.
type DockerComputeProviderConfig struct {
	//number of concurrent containers to allow on the host
	Concurrency int

	//starts a monitor job which provides job summary counts similar to the AWS Batch dashboard.
	//If the value provided is greater than 0, the monitor will start using the value as a monitoring interval in seconds
	StartMonitor int

	//optional monitor function.  The default monitor will print the status counts to StdOut
	MonitorFunction MonitorFunction

	//optional in memory secrets manager if a secrets mock is necessary
	SecretsManager SecretsManager
}

// NewDockerComputeProvider creates a new DockerComputeProvider based on the given configuration.
func NewDockerComputeProvider(config DockerComputeProviderConfig) *DockerComputeProvider {
	dcm := NewDockerComputeManager(DockerComputeManagerConfig{
		Concurrency: config.Concurrency,
	})
	if config.StartMonitor > 0 {
		dcm.StartMonitor(config.StartMonitor, config.MonitorFunction)
	}
	registry := NewInMemoryPluginRegistry()

	var secretsManager SecretsManager
	if config.SecretsManager != nil {
		secretsManager = config.SecretsManager
	} else {
		secretsManager = NewInMemorySecretsManager("")
	}
	return &DockerComputeProvider{dcm, registry, secretsManager}
}

// RegisterPlugin registers a plugin with the DockerComputeProvider.
func (dcp *DockerComputeProvider) RegisterPlugin(plugin *Plugin) (PluginRegistrationOutput, error) {
	output := PluginRegistrationOutput{}
	err := dcp.registry.Register(plugin)
	return output, err
}

// SubmitJob submits a job for execution.
func (dcp *DockerComputeProvider) SubmitJob(job *Job) error {
	jobid := uuid.New().String()
	plugin, err := dcp.registry.Get(job.JobDefinition)
	if err != nil {
		return err
	}
	job.SubmittedJob = &SubmitJobResult{
		JobId:        &jobid,
		ResourceName: nil,
	}
	dcp.manager.AddJob(&DockerJob{
		Job:            job,
		Plugin:         plugin,
		Status:         Submitted,
		SecretsManager: dcp.sm,
	})
	return nil
}

// TerminateJobs terminates jobs based on the provided input.
func (dcp *DockerComputeProvider) TerminateJobs(input TerminateJobInput) error {
	dcp.manager.TerminateJobs(input)
	return nil
}

// Status retrieves and returns job summaries based on the query.
func (dcp *DockerComputeProvider) Status(jobQueue string, query JobsSummaryQuery) error {

	summaries := make([]JobSummary, len(dcp.manager.queue.Jobs()))

	for i, job := range dcp.manager.queue.Jobs() {
		summaries[i] = JobSummary{
			JobId:   job.Job.ID.String(),
			JobName: job.Job.JobName,
			Status:  string(job.Status),
		}

	}
	query.JobSummaryFunction(summaries)
	return nil
}

func (dcp *DockerComputeProvider) JobLog(submittedJobId string, token *string) (JobLogOutput, error) {
	log.Println("Not Implemented")
	return JobLogOutput{}, nil
}

func (dcp *DockerComputeProvider) UnregisterPlugin(nameAndRevision string) error {
	log.Println("Not Implemented")
	return nil
}
