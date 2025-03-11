package cloudcompute

import (
	"sync"

	"github.com/google/uuid"
)

type JobQueue interface {
	Add(job *DockerJob)

	//Gets the first job matching the status
	Get(status JobStatus) *DockerJob

	//Gets all jobs matching the set of statuses
	//Calling with no status argument will return all jobs
	Jobs(statuses ...JobStatus) []*DockerJob

	//Get a job by ID
	GetJob(jobId uuid.UUID) *DockerJob
}

func NewInMemoryJobQueue() JobQueue {
	return &InMemoryJobQueue{queue: make(map[string]*DockerJob)}
}

type InMemoryJobQueue struct {
	sync.Mutex
	queue map[string]*DockerJob
}

func (jq *InMemoryJobQueue) Add(job *DockerJob) {
	jq.Lock()
	defer jq.Unlock()
	jq.queue[job.Job.ManifestID.String()] = job
}

func (jq *InMemoryJobQueue) Jobs(statuses ...JobStatus) []*DockerJob {
	jobs := []*DockerJob{}
	for _, job := range jq.queue {
		if job != nil {
			if statuses == nil || containsStatus(statuses, job.Status) {
				jobs = append(jobs, job)
			}
		}
	}
	return jobs
}

func (jq *InMemoryJobQueue) Get(status JobStatus) *DockerJob {
	for _, job := range jq.queue {
		if status == job.Status {
			return job
		}
	}
	return nil
}

func (jq *InMemoryJobQueue) GetJob(id uuid.UUID) *DockerJob {
	for _, job := range jq.queue {
		if job.Job.ID == id {
			return job
		}
	}
	return nil
}

func containsStatus(statusList []JobStatus, status JobStatus) bool {
	for _, v := range statusList {
		if v == status {
			return true
		}
	}
	return false
}
