package cloudcompute

import (
	"sync"

	"github.com/google/uuid"
)

type JobQueue interface {
	Add(job *DockerJob)

	// //Gets the first job matching the status
	// Get(status JobStatus) *DockerJob

	//Gets the next runable job and sets the status to Starting
	GetNextRunnable() *DockerJob

	//Gets all jobs matching the set of statuses
	//Calling with no status argument will return all jobs
	Jobs(statuses ...JobStatus) []*DockerJob

	//Get a job by ID
	GetJob(jobId uuid.UUID) *DockerJob

	UpdateJobs() []uuid.UUID
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
	//jq.queue[job.Job.ManifestID.String()] = job
	jq.queue[job.Job.ID.String()] = job
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

func (jq *InMemoryJobQueue) UpdateJobs() []uuid.UUID {
	pendingThatCanStart := []uuid.UUID{}

	// First, collect all jobs that need status updates
	jobsToUpdate := make([]*DockerJob, 0, len(jq.queue))
	jq.Lock()
	for _, job := range jq.queue {
		jobsToUpdate = append(jobsToUpdate, job)
	}
	jq.Unlock()

	for _, job := range jobsToUpdate {
		depCount := len(job.Job.DependsOn)
		if depCount == 0 && job.Status == Submitted {
			job.Status = Runnable
		}
		if depCount > 0 {
			switch job.Status {
			case Submitted:
				job.Status = Pending
			case Pending:
				deps := job.Job.DependsOn
				depJobs := jq.getJobDeps(deps)
				if depJobs.hasUnfinishedDependencies() {
					job.Status = Pending
				} else {
					job.Status = Runnable
					//pendingThatCanStart = append(pendingThatCanStart, job.Job.ManifestID)
					pendingThatCanStart = append(pendingThatCanStart, job.Job.ID)
				}
			}
		}
	}
	return pendingThatCanStart
}

func (jq *InMemoryJobQueue) GetNextRunnable() *DockerJob {
	jq.Lock()
	defer jq.Unlock()
	for i, job := range jq.queue {
		if job.Status == Runnable {
			jq.queue[i].Status = Starting
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

func (jq *InMemoryJobQueue) getJobs(jobids []string) []*DockerJob {
	jobs := make([]*DockerJob, len(jobids))
	for i, ids := range jobids {
		if job, ok := jq.queue[ids]; ok {
			jobs[i] = job
		}
	}
	return jobs
}

func (jq *InMemoryJobQueue) getJobDeps(jobids []string) JobDeps {
	jq.Lock()
	defer jq.Unlock()

	jobs := make([]*DockerJob, len(jobids))
	for i, ids := range jobids {
		for _, job := range jq.queue {
			if *job.Job.SubmittedJob.JobId == ids {
				jobs[i] = job
			}
		}
	}
	return JobDeps(jobs)
}

func containsStatus(statusList []JobStatus, status JobStatus) bool {
	for _, v := range statusList {
		if v == status {
			return true
		}
	}
	return false
}

var unfinishedDepStatusList []JobStatus = []JobStatus{Submitted, Pending, Runnable, Starting, Running}

type JobDeps []*DockerJob

func (jds JobDeps) hasUnfinishedDependencies() bool {
	for _, jd := range jds {
		for _, status := range unfinishedDepStatusList {
			if status == jd.Status {
				return true
			}
		}
	}
	return false
}
