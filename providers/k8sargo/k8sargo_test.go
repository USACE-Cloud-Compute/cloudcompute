package k8sargo

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	. "github.com/usace-cloud-compute/cloudcompute"
)

func setupTestProvider(t *testing.T) *ArgoWorkflowComputeProvider {
	config := ArgoWorkflowComputeProviderConfig{
		Namespace:  "argo",
		ServiceUrl: "localhost:2746",
	}
	provider, err := NewArgoWorkflowComputeProvider(config)
	if err != nil {
		log.Fatalln(err)
	}
	require.NoError(t, err)
	return provider
}

func TestArgoComputeProvider(t *testing.T) {
	subTestArgoComputeProvider(t, false)
}

func subTestArgoComputeProvider(t *testing.T, terminate bool) {
	//statusList := []string{"SUBMITTED", "PENDING", "RUNNABLE", "STARTING", "RUNNING", "SUCCEDED", "FAILED"}
	argoservice := setupTestProvider(t)
	pluginName := os.Getenv("TEST_PLUGIN_NAME")
	//jobQueue := os.Getenv("AWS_BATCH_QUEUE")
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
		reg, err := argoservice.RegisterPlugin(&plugin)
		assert.NoError(t, err)
		assert.Contains(t, reg.ResourceName, pluginName)
		revision = reg.Revision
	})

	t.Run("run plugin", func(t *testing.T) {
		jobId = uuid.New()
		eventId = uuid.New()
		manifestID = uuid.New()
		fmt.Printf("COMPUTE: %s\n", computeId)
		fmt.Printf("JOB: %s\n", jobId)
		fmt.Printf("EVENT: %s\n", eventId)
		fmt.Printf("MANIFEST: %s\n", manifestID)
		jobName = fmt.Sprintf("%s_c_%s_e_%s_j_%s", "cc", computeId.String(), eventId.String(), jobId.String())
		fmt.Println(jobName)
		job := Job{
			ID:            jobId,
			EventID:       eventId,
			ManifestID:    manifestID,
			JobName:       jobName,
			JobQueue:      "",
			JobDefinition: pluginName, //@TODO...do we care about revision?
		}

		event := SubmitJobsInput{
			Jobs:            []*Job{&job},
			SubmissionIdMap: make(map[uuid.UUID]string),
		}

		err := argoservice.SubmitJobs(event)
		submissionId = event.SubmissionIdMap[manifestID]
		fmt.Println(submissionId)
		assert.NoError(t, err, "Submit Jobs should succeed for the test job")
	})
	fmt.Println(revision)

	t.Run("status tests", func(t *testing.T) {

		//for compute
		// err := awsbatch.Status(jobQueue, JobsSummaryQuery{
		// 	QueryLevel: SUMMARY_COMPUTE,
		// 	QueryValue: JobNameParts{
		// 		Compute: computeId.String(),
		// 	},
		// 	JobSummaryFunction: func(summaries []JobSummary) {
		// 		status := summaries[0].Status
		// 		assert.Contains(t, statusList, status)
		// 	},
		// })
		//assert.NoError(t, err, "failed to get status.")

		//for event
		err := argoservice.Status("", JobsSummaryQuery{
			QueryLevel: SUMMARY_EVENT,
			QueryValue: JobNameParts{
				Compute: computeId.String(),
				Event:   eventId.String(),
			},
			JobSummaryFunction: func(summaries []JobSummary) {
				for _, s := range summaries {
					fmt.Println(s)
					//status := summaries[0].Status
				}
				//assert.Contains(t, statusList, status)
			},
		})
		assert.NoError(t, err, "failed to get status.")

		//for job
		// err = awsbatch.Status(jobQueue, JobsSummaryQuery{
		// 	QueryLevel: SUMMARY_JOB,
		// 	QueryValue: JobNameParts{
		// 		Compute: computeId.String(),
		// 		Event:   eventId.String(),
		// 		Job:     jobId.String(),
		// 	},
		// 	JobSummaryFunction: func(summaries []JobSummary) {
		// 		status := summaries[0].Status
		// 		assert.Contains(t, statusList, status)
		// 	},
		// })
		// assert.NoError(t, err, "failed to get status.")
	})

	time.Sleep(time.Second * 120)

	t.Run("log test", func(t *testing.T) {
		logs, err := argoservice.JobLog(JobLogInput{
			VendorJobId: eventId.String(),
		})
		if err != nil {
			// Log group might not exist yet, which is valid for a brand new job
			assert.Contains(t, err.Error(), "ResourceNotFoundException")
		} else {
			fmt.Println(logs.Logs)
			assert.NotNil(t, logs)
		}
	})

	// t.Run("unregister plugin", func(t *testing.T) {
	// 	jobDefinition := pluginName
	// 	err := argoservice.UnregisterPlugin(jobDefinition)
	// 	assert.NoError(t, err, "Unregister should succeed for a freshly created plugin")
	// })

}
