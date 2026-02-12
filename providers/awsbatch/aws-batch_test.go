package awsbatch

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	. "github.com/usace-cloud-compute/cloudcompute"
)

func setupTestProvider(t *testing.T) *AwsBatchProvider {
	executionRole := os.Getenv("AWS_BATCH_EXECUTION_ROLE")
	region := os.Getenv("AWS_REGION")
	profile := os.Getenv("AWS_BATCH_PROFILE")
	input := NewAwsBatchProviderInput(executionRole, region, profile)
	p, err := NewAwsBatchProvider(input)
	require.NoError(t, err)
	return p
}

func TestAWSBatchComputeProvider(t *testing.T) {
	subTestAWSBatchComputeProvider(t, false)
}

func TestAWSBatchComputeProviderWithTermination(t *testing.T) {
	subTestAWSBatchComputeProvider(t, true)
}

func subTestAWSBatchComputeProvider(t *testing.T, terminate bool) {
	statusList := []string{"SUBMITTED", "PENDING", "RUNNABLE", "STARTING", "RUNNING", "SUCCEDED", "FAILED"}
	awsbatch := setupTestProvider(t)
	pluginName := os.Getenv("TEST_PLUGIN_NAME")
	jobQueue := os.Getenv("AWS_BATCH_QUEUE")
	plugin := Plugin{
		Name:        pluginName,
		ImageAndTag: "busybox",
		Command: []string{"/bin/sh",
			"-c",
			"echo 'Starting Compute in CC'; sleep 30; echo 'Finished Compute'"},
		ComputeEnvironment: PluginComputeEnvironment{Memory: "512", VCPU: "1"},
	}

	computeId := uuid.New()
	var jobId uuid.UUID
	var eventId uuid.UUID
	var manifestID uuid.UUID
	var jobName string
	var revision int32 = -1
	var submissionId string

	t.Run("register plugin", func(t *testing.T) {
		reg, err := awsbatch.RegisterPlugin(&plugin)
		assert.NoError(t, err)
		assert.Contains(t, reg.ResourceName, pluginName)
		revision = reg.Revision
	})

	t.Run("run plugin", func(t *testing.T) {
		jobId = uuid.New()
		eventId = uuid.New()
		manifestID = uuid.New()
		jobName = fmt.Sprintf("%s_c_%s_e_%s_j_%s", "cc", computeId.String(), eventId.String(), jobId.String())
		job := Job{
			ID:            jobId,
			EventID:       eventId,
			ManifestID:    manifestID,
			JobName:       jobName,
			JobQueue:      jobQueue,
			JobDefinition: fmt.Sprintf("%s:%d", pluginName, revision),
		}

		event := SubmitJobsInput{
			Jobs:            []*Job{&job},
			SubmissionIdMap: make(map[uuid.UUID]string),
		}

		err := awsbatch.SubmitJobs(event)
		submissionId = event.SubmissionIdMap[manifestID]
		fmt.Println(submissionId)
		assert.NoError(t, err, "Submit Jobs should succeed for the test job")
	})

	t.Run("status tests", func(t *testing.T) {

		//for compute
		err := awsbatch.Status(jobQueue, JobsSummaryQuery{
			QueryLevel: SUMMARY_COMPUTE,
			QueryValue: JobNameParts{
				Compute: computeId.String(),
			},
			JobSummaryFunction: func(summaries []JobSummary) {
				status := summaries[0].Status
				assert.Contains(t, statusList, status)
			},
		})
		assert.NoError(t, err, "failed to get status.")

		//for event
		err = awsbatch.Status(jobQueue, JobsSummaryQuery{
			QueryLevel: SUMMARY_EVENT,
			QueryValue: JobNameParts{
				Compute: computeId.String(),
				Event:   eventId.String(),
			},
			JobSummaryFunction: func(summaries []JobSummary) {
				status := summaries[0].Status
				assert.Contains(t, statusList, status)
			},
		})
		assert.NoError(t, err, "failed to get status.")

		//for job
		err = awsbatch.Status(jobQueue, JobsSummaryQuery{
			QueryLevel: SUMMARY_JOB,
			QueryValue: JobNameParts{
				Compute: computeId.String(),
				Event:   eventId.String(),
				Job:     jobId.String(),
			},
			JobSummaryFunction: func(summaries []JobSummary) {
				status := summaries[0].Status
				assert.Contains(t, statusList, status)
			},
		})
		assert.NoError(t, err, "failed to get status.")

		if terminate {
			t.Run("termination if toggled", func(t *testing.T) {
				svj := SubmittedVendorJob{
					SubmittedJobId: submissionId,
					JobName:        jobName,
				}
				err := awsbatch.TerminateJobs(TerminateJobInput{
					Reason:     "Cleanly terminating independent test",
					JobQueue:   jobQueue,
					VendorJobs: VendorJobs{svj},
				})
				assert.NoError(t, err)
			})
		}
		//wait for finished status
		for {
			time.Sleep(time.Second * 30)
			status := ""
			err = awsbatch.Status(jobQueue, JobsSummaryQuery{
				QueryLevel: SUMMARY_JOB,
				QueryValue: JobNameParts{
					Compute: computeId.String(),
					Event:   eventId.String(),
					Job:     jobId.String(),
				},
				JobSummaryFunction: func(summaries []JobSummary) {
					status = summaries[0].Status
					t.Log(status)
				},
			})
			if status == "SUCCEEDED" || status == "FAILED" {
				break
			}
		}

	})

	t.Run("log test", func(t *testing.T) {
		logs, err := awsbatch.JobLog(submissionId, nil)
		if err != nil {
			// Log group might not exist yet, which is valid for a brand new job
			assert.Contains(t, err.Error(), "ResourceNotFoundException")
		} else {
			fmt.Println(logs.Logs)
			assert.NotNil(t, logs)
		}
	})

	t.Run("unregister plugin", func(t *testing.T) {
		jobDefinition := fmt.Sprintf("%s:%d", pluginName, revision)
		err := awsbatch.UnregisterPlugin(jobDefinition)
		assert.NoError(t, err, "Unregister should succeed for a freshly created plugin")
	})
}

// func TestRegisterPlugin(t *testing.T) {
// 	p := setupTestProvider(t)

// 	name := os.Getenv("TEST_PLUGIN_NAME")

// 	reg, err := p.RegisterPlugin(&Plugin{
// 		Name:        name,
// 		ImageAndTag: "busybox",
// 		Command: []string{"/bin/sh",
// 			"-c",
// 			"echo 'Starting Compute in CC'; sleep 30; echo 'Finished Compute'"},
// 		ComputeEnvironment: PluginComputeEnvironment{Memory: "512", VCPU: "1"},
// 	})
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	assert.NoError(t, err)
// 	assert.Contains(t, reg.ResourceName, name)

// 	// Test Unregister
// 	// err = p.UnregisterPlugin(reg.ResourceName)
// 	// assert.NoError(t, err, "Unregister should succeed for a freshly created plugin")
// }

// func TestUnRegisterPlugin(t *testing.T) {
// 	p := setupTestProvider(t)

// 	name := os.Getenv("TEST_PLUGIN_NAME")
// 	revision := os.Getenv("TEST_PLUGIN_REVISION")

// 	// Test Unregister
// 	jobDefinition := fmt.Sprintf("%s:%s", name, revision)
// 	err := p.UnregisterPlugin(jobDefinition)

// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	assert.NoError(t, err, "Unregister should succeed for a freshly created plugin")

// }

// func TestSubmitJobs(t *testing.T) {
// 	p := setupTestProvider(t)
// 	name := os.Getenv("TEST_PLUGIN_NAME")
// 	revision := os.Getenv("TEST_PLUGIN_REVISION")

// 	job := Job{
// 		ID:            uuid.New(),
// 		EventID:       uuid.New(),
// 		ManifestID:    uuid.New(),
// 		JobName:       fmt.Sprintf("TEST-%s-%s", name, revision),
// 		JobQueue:      os.Getenv("AWS_BATCH_QUEUE"),
// 		JobDefinition: fmt.Sprintf("%s:%s", name, revision),
// 	}

// 	event := SubmitJobsInput{
// 		Jobs:            []*Job{&job},
// 		SubmissionIdMap: make(map[uuid.UUID]string),
// 	}

// 	err := p.SubmitJobs(event)
// 	assert.NoError(t, err, "Submit Jobs should succeed for the test job")
// }

/*
func TestJobStatus(t *testing.T) {
	p := getTestProvider(t)
	jobID := setupTestJob(t, p)

	status, err := p.Status(jobID)
	assert.NoError(t, err)
	assert.NotEmpty(t, status)
	// Typical [AWS Batch Statuses](https://docs.aws.amazon.com)
	assert.Contains(t, []string{"SUBMITTED", "PENDING", "RUNNABLE", "STARTING", "RUNNING"}, status)
}

func TestTerminateJobs(t *testing.T) {
	p := getTestProvider(t)
	jobID := setupTestJob(t, p)

	err := p.TerminateJobs(TerminateJobInput{
		IDs: []string{jobID}, Reason: "Cleanly terminating independent test",
	})
	assert.NoError(t, err)
}

func TestGetLog(t *testing.T) {
	p := getTestProvider(t)
	jobID := setupTestJob(t, p)

	// GetLog often returns 404/Empty until the job actually runs
	// We verify the call doesn't crash and returns expected structural responses
	logs, err := p.GetLog(jobID)
	if err != nil {
		// Log group might not exist yet, which is valid for a brand new job
		assert.Contains(t, err.Error(), "ResourceNotFoundException")
	} else {
		assert.NotNil(t, logs)
	}
}
*/
