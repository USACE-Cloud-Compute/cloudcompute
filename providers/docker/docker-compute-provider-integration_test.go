package docker

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	. "github.com/usace-cloud-compute/cloudcompute"
)

func TestDockerComputeProvider_Integration(t *testing.T) {
	// 1. Setup the provider with a high concurrency for testing
	config := DockerComputeProviderConfig{
		Concurrency:  2,
		StartMonitor: 0, // Disable monitor to keep logs clean
	}
	dcp := NewDockerComputeProvider(config)

	t.Run("End-to-End: Register and Execute Job", func(t *testing.T) {
		// 2. Register a "Plugin" (Docker Image)
		// Using alpine because it is tiny and fast to pull
		imageName := "alpine:latest"
		pluginName := "test-alpine-plugin"

		err := dcp.registry.Register(&Plugin{
			Name:        pluginName,
			ImageAndTag: imageName,
			Command:     []string{"echo", "hello world"},
			ComputeEnvironment: PluginComputeEnvironment{
				VCPU:   "1",
				Memory: "256",
			},
		})
		assert.NoError(t, err)

		// 3. Submit a Job using the registered plugin
		jobID := uuid.New()
		input := SubmitJobsInput{
			Jobs: []*Job{
				{
					ID:            jobID,
					JobDefinition: pluginName,
					JobName:       "integration-test-job",
				},
			},
			SubmissionIdMap: make(map[uuid.UUID]string),
		}

		_, err = dcp.SubmitJobs(input)
		assert.NoError(t, err)

		// 4. Verify Status transitions
		// We poll the status to see if it moves from Submitted -> Succeeded
		// This functionally tests the internal DockerJobRunner we wrote earlier
		assert.Eventually(t, func() bool {
			var summaries []JobSummary
			query := JobsSummaryQuery{
				JobSummaryFunction: func(s []JobSummary) {
					summaries = s
				},
			}
			dcp.Status("default", query)

			for _, s := range summaries {
				if s.JobId == jobID.String() {
					return s.Status == string(Succeeded)
				}
			}
			return false
		}, 30*time.Second, 500*time.Millisecond, "Job did not reach Succeeded status in time")
	})

	t.Run("Functional: Terminate Running Job", func(t *testing.T) {
		// Register a plugin that sleeps so we have time to kill it
		pluginName := "sleepy-plugin"
		dcp.registry.Register(&Plugin{
			Name:        pluginName,
			ImageAndTag: "alpine:latest",
			Command:     []string{"sleep", "60"},
			ComputeEnvironment: PluginComputeEnvironment{
				VCPU:   "1",
				Memory: "256",
			},
		})

		jobID := uuid.New()
		event := SubmitJobsInput{
			Jobs:            []*Job{{ID: jobID, JobDefinition: pluginName}},
			SubmissionIdMap: make(map[uuid.UUID]string),
		}
		dcp.SubmitJobs(event)

		// Give it a moment to start
		time.Sleep(1 * time.Second)

		// Terminate the job
		err := dcp.TerminateJobs(TerminateJobInput{
			Reason:   "Testing termination",
			JobQueue: "docker-queue",
			Query: JobsSummaryQuery{
				QueryLevel: SUMMARY_COMPUTE,
				QueryValue: JobNameParts{
					Compute: event.SubmissionIdMap[jobID],
				},
			},
		})
		assert.NoError(t, err)

		// Verify status becomes Failed (or Terminated depending on your logic)
		assert.Eventually(t, func() bool {
			var summaries []JobSummary
			dcp.Status("default", JobsSummaryQuery{
				JobSummaryFunction: func(s []JobSummary) { summaries = s },
			})
			for _, s := range summaries {
				if s.JobId == jobID.String() {
					return s.Status == string(Failed)
				}
			}
			return false
		}, 5*time.Second, 500*time.Millisecond)
	})
}
