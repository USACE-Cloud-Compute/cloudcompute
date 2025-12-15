package cloudcompute

import (
	"fmt"
	"log"
	"strings"
	"time"

	. "github.com/usace-cloud-compute/cloudcompute"
)

type JobStatus string

const (
	Submitted JobStatus = "SUBMITTED"
	Pending   JobStatus = "PENDING"
	Runnable  JobStatus = "RUNNABLE"
	Starting  JobStatus = "STARTING"
	Running   JobStatus = "RUNNING"
	Succeeded JobStatus = "SUCCEEDED"
	Failed    JobStatus = "FAILED"
)

type DockerJob struct {
	Job            *Job
	Plugin         *Plugin
	Status         JobStatus
	Log            string
	runner         *DockerJobRunner
	SecretsManager SecretsManager
	//JobDefinition *Plugin //@TODO need to add????

}

type MonitorFunction func(statuses map[JobStatus]int)

type DockerComputeManagerConfig struct {
	Concurrency               int
	DockerPullProgressFactory DockerPullProgressFactory
}

type DockerComputeManager struct {
	//wg          *sync.WaitGroup
	concurrency               int
	queue                     JobQueue
	queueEvents               chan string
	control                   chan struct{}
	limiter                   chan struct{}
	dockerPullProgressFactory DockerPullProgressFactory
}

func NewDockerComputeManager(config DockerComputeManagerConfig) *DockerComputeManager {

	if config.Concurrency == 0 {
		config.Concurrency = 1
	}

	dcm := DockerComputeManager{
		queue:                     NewInMemoryJobQueue(),
		concurrency:               config.Concurrency,
		queueEvents:               make(chan string),
		control:                   make(chan struct{}),
		limiter:                   make(chan struct{}, config.Concurrency),
		dockerPullProgressFactory: config.DockerPullProgressFactory,
	}
	//start the runner
	go dcm.runner()
	return &dcm
}

func (dcm *DockerComputeManager) AddJob(djob *DockerJob) {
	dcm.queue.Add(djob)
	dcm.queueEvents <- fmt.Sprintf("SUBMITTED JOB: %s", djob.Job.ID)
}

func (dcm *DockerComputeManager) runner() {
	log.Println("Starting Runner")
	for {
		select {
		case event := <-dcm.queueEvents:
			log.Println(event)
			pendingJobsThatCanStart := dcm.queue.UpdateJobs()

			runnableJob := dcm.queue.GetNextRunnable()
			if runnableJob == nil {
				continue //@TODO not really sure this could actually happen...but just in case
			}
			dcm.limiter <- struct{}{}
			go func(dockerJob *DockerJob) {
				defer func() {
					<-dcm.limiter
					dcm.queueEvents <- fmt.Sprintf("FINISHED JOB: %s", dockerJob.Job.ID)
				}()
				//dockerJob.Status = Starting
				runner, err := NewRunner(dockerJob) //where we get pull progress bars
				if err != nil {
					dockerJob.Status = Failed
				}

				//get the docker pull progress function if it exists
				if dcm.dockerPullProgressFactory != nil {
					runner.dpp = dcm.dockerPullProgressFactory.New()
				}

				//create a reference in the docker job for termination
				dockerJob.runner = runner
				defer runner.Close()

				err = runner.Run()
				if err != nil {
					log.Println(err)
				}

			}(runnableJob)

			for _, id := range pendingJobsThatCanStart {
				go func() {
					dcm.queueEvents <- fmt.Sprintf("PENDING JOB: %s CAN START", id)
				}()
			}

		case <-dcm.control:
			//dcm.wg.Done()
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

func (dcm *DockerComputeManager) TerminateJobs(input TerminateJobInput) {
	for _, job := range dcm.queue.Jobs() {
		if strings.Contains(job.Job.JobName, input.Query.QueryValue.Compute) {
			job.runner.Terminate()
		}
	}
}
