package cloudcompute

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	. "github.com/usace/cloudcompute"
)

type JobStatus string

const (
	Submitted JobStatus = "SUBMITTED"
	Pending   JobStatus = "PENDING"
	Runnable  JobStatus = "RUNNABLE"
	Starting  JobStatus = "STARTING"
	Running   JobStatus = "RUNNING"
	Suceeded  JobStatus = "SUCCEEDED"
	Failed    JobStatus = "FAILED"
)

type DockerJob struct {
	Job            *Job
	Plugin         *Plugin
	Status         JobStatus
	Log            string
	runner         *DockerJobRunner
	SecretsManager *SecretsManager
	//JobDefinition *Plugin //@TODO need to add????

}

type MonitorFunction func(statuses map[JobStatus]int)

type DockerComputeManagerConfig struct {
	Concurrency int
}

type DockerComputeManager struct {
	wg          *sync.WaitGroup
	concurrency int
	queue       JobQueue
	queueEvents chan string
	control     chan struct{}
	limiter     chan struct{}
}

func NewDockerComputeManager(config DockerComputeManagerConfig) *DockerComputeManager {
	//wg := sync.WaitGroup{}
	//wg.Add(1)
	dcm := DockerComputeManager{
		//wg:          &wg,
		queue:       NewInMemoryJobQueue(),
		concurrency: config.Concurrency,
		queueEvents: make(chan string),
		control:     make(chan struct{}),
		limiter:     make(chan struct{}, config.Concurrency),
	}
	//start the runner
	go dcm.runner()
	return &dcm
}

func (dcm *DockerComputeManager) AddJob(djob *DockerJob) {
	dcm.queue.Add(djob)
	dcm.queueEvents <- fmt.Sprintf("SUBMITTED JOB: %s", djob.Job.ManifestID)
}

func (dcm *DockerComputeManager) runner() {
	log.Println("Starting Runner")
	for {
		select {
		case event := <-dcm.queueEvents:
			fmt.Println(event)
			runnableJob := dcm.queue.Get(Runnable)
			if runnableJob == nil {
				continue //@TODO not really sure this could actually happen...but just in case
			}
			dcm.limiter <- struct{}{}
			go func(dockerJob *DockerJob) {
				defer func() {
					<-dcm.limiter
					dcm.queueEvents <- fmt.Sprintf("FINISHED JOB: %s", dockerJob.Job.ManifestID)
				}()
				dockerJob.Status = Starting
				runner, err := NewRunner(dockerJob)
				if err != nil {
					dockerJob.Status = Failed
				}
				dockerJob.runner = runner //create a reference in the docker job for termination
				defer runner.Close()

				err = runner.Run()
				if err != nil {
					log.Println(err)
				}

			}(runnableJob)

		case <-dcm.control:
			dcm.wg.Done()
			log.Println("Stopping Runner")
			return
		}
	}
}

func (dcm *DockerComputeManager) StartMonitor(interval int, monitorFunc MonitorFunction) {
	go func() {
		for {
			time.Sleep(time.Duration(interval) * time.Second)
			statuses := make(map[JobStatus]int)
			for _, job := range dcm.queue.Jobs() {
				count := statuses[job.Status]
				statuses[job.Status] = count + 1
			}
			if monitorFunc != nil {
				monitorFunc(statuses)
			} else {
				log.Println(statuses)
			}
		}
	}()
}

func (dcm *DockerComputeManager) Wait() {
	dcm.wg.Wait()
}

func (dcm *DockerComputeManager) TerminateJobs(input TerminateJobInput) {
	for _, job := range dcm.queue.Jobs() {
		if strings.Contains(job.Job.JobName, input.Query.QueryValue.Compute) {
			job.runner.Terminate()
		}
	}

	// for _, job := range dcm.queue.Jobs() {
	// 	if input.VendorJobs.IncludesJob(job.Job.ID.String()) {
	// 		job.runner.Terminate()
	// 	}
	// }
}
