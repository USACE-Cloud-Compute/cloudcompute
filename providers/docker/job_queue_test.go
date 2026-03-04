package docker

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	. "github.com/usace-cloud-compute/cloudcompute"
)

func TestJobQueueOperations(t *testing.T) {
	// Create a new job queue
	queue := NewInMemoryJobQueue()

	// Test Add operation
	job := &DockerJob{
		Job: &Job{
			ID: uuid.New(),
		},
		Status: Submitted,
	}

	queue.Add(job)
	jobs := queue.Jobs()
	assert.Len(t, jobs, 1)
	assert.Equal(t, job, jobs[0])

	// Test Jobs() with no status filter
	jobsAll := queue.Jobs()
	assert.Len(t, jobsAll, 1)
	assert.Equal(t, job, jobsAll[0])

	// Test Jobs() with status filter
	jobsSubmitted := queue.Jobs(Submitted)
	assert.Len(t, jobsSubmitted, 1)
	assert.Equal(t, job, jobsSubmitted[0])

	// Test Jobs() with multiple status filters
	jobsMultiple := queue.Jobs(Submitted, Running)
	assert.Len(t, jobsMultiple, 1)
	assert.Equal(t, job, jobsMultiple[0])
}

func TestJobQueueGetNextRunnable(t *testing.T) {
	queue := NewInMemoryJobQueue()

	// Add a job with Runnable status
	job := &DockerJob{
		Job: &Job{
			ID: uuid.New(),
		},
		Status: Runnable,
	}

	queue.Add(job)

	// Get next runnable job
	runnableJob := queue.GetNextRunnable()
	assert.NotNil(t, runnableJob)
	assert.Equal(t, Starting, runnableJob.Status)
}

func TestJobQueueGetNextRunnableWithNoRunnable(t *testing.T) {
	queue := NewInMemoryJobQueue()

	// Add a job with Submitted status
	job := &DockerJob{
		Job: &Job{
			ID: uuid.New(),
		},
		Status: Submitted,
	}

	queue.Add(job)

	// Get next runnable job (should return nil)
	runnableJob := queue.GetNextRunnable()
	assert.Nil(t, runnableJob)
}

func TestJobQueueGetJob(t *testing.T) {
	queue := NewInMemoryJobQueue()

	// Add a job
	jobId := uuid.New()
	job := &DockerJob{
		Job: &Job{
			ID: jobId,
		},
		Status: Submitted,
	}

	queue.Add(job)

	// Get job by ID
	foundJob := queue.GetJob(jobId)
	assert.NotNil(t, foundJob)
	assert.Equal(t, job, foundJob)

	// Try to get non-existent job
	nonExistentId := uuid.New()
	foundJob2 := queue.GetJob(nonExistentId)
	assert.Nil(t, foundJob2)
}

func TestJobQueueUpdateJobs(t *testing.T) {
	queue := NewInMemoryJobQueue()

	// Add a job with no dependencies
	job1 := &DockerJob{
		Job: &Job{
			ID: uuid.New(),
		},
		Status: Submitted,
	}

	// Add a job with dependencies
	job2 := &DockerJob{
		Job: &Job{
			ID:        uuid.New(),
			DependsOn: []string{"some-dependency-id"},
		},
		Status: Submitted,
	}

	queue.Add(job1)
	queue.Add(job2)

	// Update jobs
	pendingThatCanStart := queue.UpdateJobs()
	assert.NotNil(t, pendingThatCanStart)

	// Verify job statuses
	jobs := queue.Jobs()
	assert.Len(t, jobs, 2)

	// The job with no dependencies should be Runnable
	// The job with dependencies should be Pending
}

func TestJobQueueUpdateJobsWithDependencies(t *testing.T) {
	queue := NewInMemoryJobQueue()

	// Add a job with no dependencies (should become Runnable)
	job1 := &DockerJob{
		Job: &Job{
			ID: uuid.New(),
		},
		Status: Submitted,
	}

	// Add a job with dependencies
	job2 := &DockerJob{
		Job: &Job{
			ID:        uuid.New(),
			DependsOn: []string{job1.Job.ID.String()},
		},
		Status: Submitted,
	}

	queue.Add(job1)
	queue.Add(job2)

	// Update jobs
	pendingThatCanStart := queue.UpdateJobs()
	assert.NotNil(t, pendingThatCanStart)

	// Verify job statuses
	jobs := queue.Jobs()
	assert.Len(t, jobs, 2)

	// Both jobs should be in the queue
	for _, job := range jobs {
		// Job1 should be Runnable (no dependencies)
		// Job2 should be Pending (has dependencies)
		if job.Job.ID == job1.Job.ID {
			assert.Equal(t, Runnable, job.Status)
		} else if job.Job.ID == job2.Job.ID {
			assert.Equal(t, Pending, job.Status)
		}
	}
}

func TestJobQueueJobsWithNilStatus(t *testing.T) {
	queue := NewInMemoryJobQueue()

	// Add jobs
	job1 := &DockerJob{
		Job: &Job{
			ID: uuid.New(),
		},
		Status: Submitted,
	}

	job2 := &DockerJob{
		Job: &Job{
			ID: uuid.New(),
		},
		Status: Running,
	}

	queue.Add(job1)
	queue.Add(job2)

	// Test Jobs() with nil parameter (should return all jobs)
	jobs := queue.Jobs()
	assert.Len(t, jobs, 2)
}

// func TestJobQueueGetJobDeps(t *testing.T) {
// 	queue := NewInMemoryJobQueue()

// 	// Add jobs to queue
// 	job1 := &DockerJob{
// 		Job: &Job{
// 			ID: uuid.New(),
// 			SubmittedJob: &SubmitJobResult{
// 				JobId: &job1.Job.ID.String(),
// 			},
// 		},
// 		Status: Succeeded,
// 	}

// 	job2 := &DockerJob{
// 		Job: &Job{
// 			ID: uuid.New(),
// 			SubmittedJob: &SubmitJobResult{
// 				JobId: &job2.Job.ID.String(),
// 			},
// 		},
// 		Status: Running,
// 	}

// 	queue.Add(job1)
// 	queue.Add(job2)

// 	// Test getJobDeps - this tests the internal method
// 	// Note: This is a bit tricky to test directly since it's internal
// 	// But we can at least ensure it doesn't panic
// 	jobs := queue.Jobs()
// 	assert.Len(t, jobs, 2)
// }

// func TestJobQueueHasUnfinishedDependencies(t *testing.T) {
// 	// Test the JobDeps hasUnfinishedDependencies method
// 	jobs := []*DockerJob{}

// 	// Empty job deps
// 	jobDeps := JobDeps(jobs)
// 	assert.False(t, jobDeps.hasUnfinishedDependencies())

// 	// Add a job with unfinished status
// 	job1 := &DockerJob{
// 		Status: Running,
// 	}
// 	jobs = append(jobs, job1)
// 	jobDeps = JobDeps(jobs)
// 	assert.True(t, jobDeps.hasUnfinishedDependencies())

// 	// Add a job with finished status
// 	job2 := &DockerJob{
// 		Status: Succeeded,
// 	}
// 	jobs = append(jobs, job2)
// 	jobDeps = JobDeps(jobs)
// 	assert.False(t, jobDeps.hasUnfinishedDependencies())
// }

func TestJobQueueContainsStatus(t *testing.T) {
	statusList := []JobStatus{Submitted, Running, Failed}

	assert.True(t, containsStatus(statusList, Submitted))
	assert.True(t, containsStatus(statusList, Running))
	assert.True(t, containsStatus(statusList, Failed))
	assert.False(t, containsStatus(statusList, Succeeded))
	assert.False(t, containsStatus(statusList, Pending))
}
