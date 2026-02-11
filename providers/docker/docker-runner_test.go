package docker

import (
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	. "github.com/usace-cloud-compute/cloudcompute"
)

func TestDockerJobRunner_RunAndTerminate(t *testing.T) {
	// Create real client
	cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())

	t.Run("Run: Pulls and Executes Alpine", func(t *testing.T) {
		job := &DockerJob{
			Job: &Job{ID: uuid.New()},
			Plugin: &Plugin{
				ImageAndTag:        "alpine:latest",
				Command:            []string{"echo", "hello-world"},
				ComputeEnvironment: PluginComputeEnvironment{Memory: "128", VCPU: "1"},
			},
		}

		runner := &DockerJobRunner{
			client: cli,
			djob:   job,
		}

		err := runner.Run()
		assert.NoError(t, err)
		time.Sleep(2 * time.Second) //wait for job to finish
		assert.Equal(t, Succeeded, job.Status)

		// Cleanup container
		runner.Close()
	})

	t.Run("Terminate: Stops a Sleeping Container", func(t *testing.T) {
		job := &DockerJob{
			Job: &Job{ID: uuid.New()},
			Plugin: &Plugin{
				ImageAndTag:        "alpine:latest",
				Command:            []string{"sleep", "30"},
				ComputeEnvironment: PluginComputeEnvironment{Memory: "128", VCPU: "1"},
			},
		}

		runner := &DockerJobRunner{
			client: cli,
			djob:   job,
		}

		// Run in background because Run() blocks until completion/monitoring
		errCh := make(chan error, 1)
		go func() {
			errCh <- runner.Run()
		}()

		// Wait for it to start
		time.Sleep(2 * time.Second)
		assert.Equal(t, Running, job.Status)

		// Terminate
		err := runner.Terminate()
		assert.NoError(t, err)
		assert.Equal(t, Failed, job.Status)

		runner.Close()
	})
}
