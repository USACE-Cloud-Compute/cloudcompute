package docker

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"

	//"github.com/testcontainers/testcontainers-go/wait"
	. "github.com/usace-cloud-compute/cloudcompute"
)

func TestSomething3(t *testing.T) {
	fmt.Println("asdfasdf")
}

func TestDockerRunMonitor_FunctionalScenarios(t *testing.T) {
	ctx := context.Background()
	cli, _ := client.NewClientWithOpts(client.FromEnv)

	// SCENARIO 1: Large Log Volume (Stress Test)
	t.Run("Stress Test: Large Log Volume", func(t *testing.T) {
		req := testcontainers.ContainerRequest{
			Image: "alpine",
			// Generate 1000 lines of text
			Cmd: []string{"sh", "-c", "for i in $(seq 1 1000); do echo \"Line $i\"; done"},
		}
		container, _ := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		defer container.Terminate(ctx)

		dr := &DockerJobRunner{client: cli, djob: &DockerJob{Job: &Job{ID: uuid.New()}, Status: Running}}
		drm := NewDockerRunMonitor(dr, container.GetContainerID())

		drm.Wait() // This must not hang
		assert.Equal(t, Succeeded, dr.djob.Status)
	})

	// SCENARIO 2: Invalid Container ID (Error Handling)
	t.Run("Error: Invalid Container ID", func(t *testing.T) {
		dr := &DockerJobRunner{client: cli, djob: &DockerJob{Job: &Job{ID: uuid.New()}, Status: Running}}

		// Pass an ID that doesn't exist
		drm := NewDockerRunMonitor(dr, "non-existent-id-123")

		assert.Nil(t, drm, "Monitor should be nil on invalid ID")
		assert.Equal(t, Failed, dr.djob.Status)
	})

	// SCENARIO 3: Slow Container / Exit Timing
	t.Run("Timing: Container Exits Before Logs Finish", func(t *testing.T) {
		req := testcontainers.ContainerRequest{
			Image: "alpine",
			// Exit immediately but try to print something
			Cmd: []string{"sh", "-c", "echo 'final message' && exit 0"},
		}
		container, _ := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		defer container.Terminate(ctx)

		dr := &DockerJobRunner{client: cli, djob: &DockerJob{Job: &Job{ID: uuid.New()}, Status: Running}}
		drm := NewDockerRunMonitor(dr, container.GetContainerID())

		drm.Wait()
		assert.Equal(t, Succeeded, dr.djob.Status)
	})
}
