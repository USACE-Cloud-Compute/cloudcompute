package cloudcompute

import (
	"errors"
	"fmt"
	"log"

	. "github.com/usace/cc-go-sdk"
	"github.com/usace/cloudcompute/utils"

	"github.com/google/uuid"
)

// CloudCompute is a compute submission for a single dag for a set of events
// The compute environment Job Queue and Job Definitions must exist before a CloudCompute
// can be initiated.
type CloudCompute struct {
	//Compute Identifier
	ID uuid.UUID `json:"id"`

	//User friendly Name for the compute
	Name string `json:"name"`

	//JobQueue to push the events to
	JobQueue string `json:"jobQueue"`

	//Event generator
	Events EventGenerator `json:"events"`

	//compute provider for the compute (typically AwsBatchProvider)
	ComputeProvider ComputeProvider `json:"computeProvider"`

	//Job store is for saving the set of jobs being sent to the compute environment to a
	//user defined storage location.  Typically something like a relational database
	JobStore CcJobStore
}

// Runs a Compute on the ComputeProvider submitting jobs sequentially
// currently an event number of -1 represents an invalid event received from the event generator.
// These events are skipped
func (cc *CloudCompute) Run() error {
	return cc.RunParralel(1)
}

// Runs a Compute on the ComputeProvider submitting jobs in parallel based on the concurrency requested
// currently an event number of -1 represents an invalid event received from the event generator.
// These events are skipped
func (cc *CloudCompute) RunParralel(concurrency int) error {
	//cc.submissionIdMap = make(map[uuid.UUID]string)

	cr := NewConcurrentRunner(concurrency)

	for cc.Events.HasNextEvent() {

		event, eventErr := cc.Events.NextEvent()

		cr.Run(func() {

			event.submissionIdMap = make(map[uuid.UUID]string) //reduce the scope of the idmap to a single event

			//the event generator returns an error if it cannot generate a valid event
			//processing will continue when a valid event is generated.  An example would be
			//generating events from csv that includes invalid values (emply starting or ending values)
			if eventErr != nil {
				log.Printf("error generating next event: %s\n", eventErr)
				return
			}

			//go func(event Event) {
			for _, manifest := range event.Manifests {
				if len(manifest.Inputs.PayloadAttributes) > 0 || len(manifest.Inputs.DataSources) > 0 || len(manifest.Actions) > 0 {
					err := manifest.WritePayload() //guarantees the payload id written to the manifest
					if err != nil {
						log.Printf("error writing payload for event %s: %s:\n", event.EventIdentifier, err)
						return
					}
				}
				env := append(manifest.Inputs.Environment,
					KeyValuePair{CcPayloadId, manifest.payloadID.String()},
					KeyValuePair{CcManifestId, manifest.ManifestID.String()},
				)

				if !env.HasKey(CcEventIdentifier) {
					env = append(env, KeyValuePair{CcEventIdentifier, fmt.Sprint(event.EventIdentifier)})
					env = append(env, KeyValuePair{CcEventNumber, fmt.Sprint(event.EventIdentifier)})
				}

				//the manifest substitution is will be removed in future versions.
				//it is only supported now to ease the transition to payloadId vs manifestId
				if !env.HasKey(CcManifestId) {
					env = append(env, KeyValuePair{CcManifestId, manifest.ManifestID.String()})
				}

				env = append(env, KeyValuePair{CcPluginDefinition, manifest.PluginDefinition}) //@TODO do we need this?
				jobID := uuid.New()
				job := Job{
					ID:            jobID,
					EventID:       event.ID,
					ManifestID:    manifest.ManifestID,
					JobName:       fmt.Sprintf("%s_C_%s_E_%s_J_%s", CcProfile, cc.ID.String(), event.ID.String(), jobID),
					JobQueue:      cc.JobQueue,
					JobDefinition: manifest.PluginDefinition,
					DependsOn:     event.mapDependencies(&manifest),
					Parameters:    manifest.Inputs.Parameters,
					Tags:          manifest.Tags,
					RetryAttemts:  manifest.RetryAttemts,
					JobTimeout:    manifest.JobTimeout,
					ContainerOverrides: ContainerOverrides{
						Environment:          env,
						Command:              manifest.Command,
						ResourceRequirements: manifest.ResourceRequirements,
					},
				}
				err := cc.ComputeProvider.SubmitJob(&job)
				if err != nil {
					log.Printf("error submitting job for event %s: %s:\n", event.EventIdentifier, err)
					return //@TODO what happens if a set submit ok then one fails?  How do we cancel? See notes below
				}
				if cc.JobStore != nil {
					err := cc.JobStore.SaveJob(cc.ID, manifest.payloadID, event.EventIdentifier, &job)
					if err != nil {
						log.Printf("error saving job for event %s: %s:\n", event.EventIdentifier, err)
						return //@TODO should we terminate everything if we cannot save to the compute store?
					}
				}
				event.submissionIdMap[manifest.ManifestID] = *job.SubmittedJob.JobId
			}
		})
	} //end of event loop
	cr.Wait()
	return nil
}

// Requests the status of a given compute at the COMPUTE, EVENT, or JOB level
// A JobSummaryFunction is necessary to process the status
func (cc *CloudCompute) Status(query JobsSummaryQuery) error {
	return cc.ComputeProvider.Status(cc.JobQueue, query)
}

// Cancels jobs submitted to compute environment
func (cc *CloudCompute) Cancel(reason string) error {
	input := TerminateJobInput{
		Reason:   reason,
		JobQueue: cc.JobQueue,
		Query: JobsSummaryQuery{
			QueryLevel: SUMMARY_COMPUTE,
			QueryValue: JobNameParts{Compute: cc.ID.String()},
		},
	}
	return cc.ComputeProvider.TerminateJobs(input)
}

/////////////////////////////
//////// MANIFEST ///////////

// ComputeManifest is the information necessary to execute a single job in an event
type ComputeManifest struct {
	ManifestName         string                `json:"manifest_name,omitempty" jsonschema:"title=Manifest Name,description=The name for the compute manifest"`
	ManifestID           uuid.UUID             `json:"manifest_id,omitempty" jsonschema:"-"`
	Command              []string              `json:"command,omitempty" jsonschema:"title=Command Override,description=An optional command override for the plugin"`
	Dependencies         []uuid.UUID           `json:"dependencies,omitempty" jsonschema:"-"`
	Stores               []DataStore           `json:"stores,omitempty" jsonschema:"title=Stores"`
	Inputs               PluginInputs          `json:"inputs,omitempty" jsonschema:"title=Inputs"`
	Outputs              []DataSource          `json:"outputs,omitempty" jsonschema:"title=Outputs"`
	Actions              []Action              `json:"actions,omitempty" jsonschema:"title=Actions"`
	PluginDefinition     string                `json:"plugin_definition,omitempty" jsonschema:"title=Plugin Definition"` //plugin resource name. "name:version"
	Tags                 map[string]string     `json:"tags,omitempty" jsonschema:"title=Tags"`
	RetryAttemts         int32                 `json:"retry_attempts,omitempty" jsonschema:"title=Retry Attempts Override"`
	JobTimeout           int32                 `json:"job_timeout,omitempty" jsonschema:"title=Job Timeout Override"`
	ResourceRequirements []ResourceRequirement `json:"resource_requirements,omitempty" jsonschema:"title=Resource Requirement Overrides"`
	payloadID            uuid.UUID             `json:"-"`
}

// This is a transitional method that will be removed in a future version
// It is intended to facilitate running manifests written prior to
// payloadId
// @Depricated
func (cm *ComputeManifest) GetPayload() uuid.UUID {
	return cm.payloadID
}

// writes a payload to the compute store
func (cm *ComputeManifest) WritePayload() error {
	if cm.payloadID == uuid.Nil {
		payloadId := uuid.New()
		computeStore, err := NewCcStore(cm.ManifestID.String(), payloadId.String())
		if err != nil {
			return err
		}
		p := Payload{
			IOManager: IOManager{
				Attributes: cm.Inputs.PayloadAttributes,
				Stores:     cm.Stores,
				Inputs:     cm.Inputs.DataSources,
				Outputs:    cm.Outputs,
			},
			Actions: cm.Actions,
		}
		err = computeStore.SetPayload(p)
		if err != nil {
			return err
		}
		if cm.Tags == nil {
			cm.Tags = make(map[string]string)
		}
		cm.Tags["payload"] = payloadId.String()
		cm.payloadID = payloadId
	}
	return nil
}

type ComputeManifests []ComputeManifest

func (cm *ComputeManifests) Len() int {
	cms := []ComputeManifest(*cm)
	return len(cms)
}

// gets a reference to a manifest by manifest id in a slice of compute manifests.
// Optionally the caller can request a deep copy of the manifest
// method returns the index position of the manifest, a reference to the
// manifest or a copy of it, and any errors
func (cm *ComputeManifests) GetManifest(id uuid.UUID, deepcopy bool) (int, *ComputeManifest, error) {
	for i, m := range *cm {
		if m.ManifestID == id {
			if !deepcopy {
				return i, &m, nil
			} else {
				manifestcopy := ComputeManifest{}
				err := utils.Copy(&m).To(&manifestcopy)
				return i, &manifestcopy, err
			}
		}
	}
	return -1, nil, errors.New("compute manifest id is not in the list of manifests")
}

// gets a reference to a manifest by manifest index in a slice of compute manifests.
// Optionally the caller can request a deep copy of the manifest
// method a reference to the manifest or a copy of it, and any errors
func (cm *ComputeManifests) GetManifestByIndex(index int, deepcopy bool) (*ComputeManifest, error) {
	manifests := []ComputeManifest(*cm)
	m := manifests[index]
	if !deepcopy {
		return &m, nil
	} else {
		manifestcopy := ComputeManifest{}
		err := utils.Copy(&m).To(&manifestcopy)
		return &manifestcopy, err
	}
}

// Job level inputs that can be injected into a container
type PluginInputs struct {
	Environment       KeyValuePairs     `json:"environment,omitempty"`
	Parameters        map[string]string `json:"parameters,omitempty"`
	DataSources       []DataSource      `json:"data_sources,omitempty"`
	PayloadAttributes PayloadAttributes `json:"payload_attributes,omitempty"`
}

/////////////////////////////
///////// EVENT /////////////

// EVENT is a single run through the DAG
type Event struct {
	ID              uuid.UUID         `json:"id"`
	EventIdentifier string            `json:"event"` //RULES ONLY NUMBERS, STRINGS, DASH, AND UNDERSCORE
	Manifests       []ComputeManifest `json:"manifests"`

	//map of cloud compute job identifier (manifest id) to submitted job identifier (VendorID) in the compute provider
	submissionIdMap map[uuid.UUID]string
}

// Adds a manifest to the Event
func (e *Event) AddManifest(m ComputeManifest) {
	e.Manifests = append(e.Manifests, m)
}

// Adds a manifest at a specific ordinal position in the event.
func (e *Event) AddManifestAt(m ComputeManifest, i int) {
	e.Manifests = append(e.Manifests[:i+1], e.Manifests[i:]...)
	e.Manifests[i] = m
}

// Maps the Dependency identifiers to the compute environment identifiers received from submitted jobs.
func (e *Event) mapDependencies(manifest *ComputeManifest) []string {
	sdeps := make([]string, len(manifest.Dependencies))
	for i, d := range manifest.Dependencies {
		if sdep, ok := e.submissionIdMap[d]; ok {
			sdeps[i] = sdep
		}
	}
	return sdeps
}

/////////////////////////////
///////// PLUGIN ////////////

// Plugin struct is used to interact with the compute environment and create a Job Definition
// this is likely going to be moved to the CCAPI.
// When entering credentials, use the format of the compute provider.
// For example when using AWS Batch: "AWS_ACCESS_KEY_ID", "arn:aws:secretsmanager:us-east-1:01010101010:secret:mysecret:AWS_ACCESS_KEY_ID::
type Plugin struct {
	Name string `json:"name" jsonschema:"title=Name"`
	//Revision           string                   `json:"revision" yaml:"revision"`
	ImageAndTag        string                   `json:"image_and_tag" jsonschema:"title=Image and Tag,description=The docker image and tag"`
	Description        string                   `json:"description" jsonschema:"title=Description"`
	Command            []string                 `json:"command" jsonschema:"title=Command,description=The docker command and arguments to run"`
	ComputeEnvironment PluginComputeEnvironment `json:"compute_environment" jsonschema:"title=Compute Environment,description=CPU and Memory runtime requirements"`
	DefaultEnvironment KeyValuePairs            `json:"environment" jsonschema:"title=Default Environment Variables,description=The list of default environment variables"` //default values for the container environment
	Volumes            []PluginComputeVolumes   `json:"volumes" jsonschema:"title=Volume Mounts,description=Storage volumes that need to be mounted to the plugin when it is run"`
	Credentials        KeyValuePairs            `json:"credentials" jsonschema:"title=Credentials,description=Configures credentials/secrets from the service provider to be injected into the running container.  Note: DO NOT ENTER PASSWORDS OR ACTUAL CREDENTIALS"`
	Parameters         map[string]string        `json:"parameters" jsonschema:"title=Parameters"`
	RetryAttemts       int32                    `json:"retry_attempts" jsonschema:"title=Retry Attempts"`
	ExecutionTimeout   *int32                   `json:"execution_timeout" jsonschema:"title=Execution Timeout (sec)"`
	Privileged         bool                     `json:"privileged" jsonschema:"title=Requires Privileged Execution"` //assign container privileged execution.  for example to mount linux devices
	LinuxParameters    LinuxParameters          `json:"linux_parameters" jsonschema:"title=Linux Parameters"`
	MountPoints        []MountPoint             `json:"mountpoints" jsonschema:"title=MountPoints"`
}

type LinuxParameters struct {
	Devices []LinuxDevice `json:"devices" jsonschema:"title=Devices"`
}

type LinuxDevice struct {
	HostPath      *string `json:"host_path" jsonschema:"title=Host Path"`
	ContainerPath *string `json:"container_path" jsonschema:"title=Container Path"`
}

type MountPoint struct {
	ContainerPath string
	ReadOnly      bool
	SourceVolume  string
}

type PluginComputeEnvironment struct {
	VCPU   string `json:"vcpu" jsonschema:"title=Virtual CPUs"`
	Memory string `json:"memory" jsonschema:"title=Memory in MB"`
}

type PluginComputeVolumes struct {
	Name         string `json:"name" jsonschema:"title=Name"`
	ResourceName string `json:"resource_name" jsonschema:"title=ResourceName,description=The vendor resource name, e.g. ARN for AWS"`
	ReadOnly     bool   `json:"read_only" jsonschema:"title=Read Only,description=Check to make this resource read-only"`
	MountPoint   string `json:"mount_point" jsonschema:"title=Mount Point,description=Path in the running container to mount the volume"` //default is "/data"
}

type PluginRegistrationOutput struct {
	Name         string
	ResourceName string
	Revision     int32
}

type CcJobStore interface {
	SaveJob(computeId uuid.UUID, payloadId uuid.UUID, event string, job *Job) error
}

type CcMessageQueue interface {
	SendMessage(channel string, message []byte) error
	Subscribe(channel string) (<-chan any, error) //amqp.Delivery
}

type ConcurrentRunner struct {
	limit     int
	semaphore chan struct{}
}

func NewConcurrentRunner(limit int) *ConcurrentRunner {
	return &ConcurrentRunner{
		limit:     limit,
		semaphore: make(chan struct{}, limit),
	}
}

func (cr *ConcurrentRunner) Run(cf func()) {
	cr.semaphore <- struct{}{}
	go func() {
		defer func() {
			<-cr.semaphore
		}()
		cf()
	}()
}

func (cr *ConcurrentRunner) Wait() {
	for i := 0; i < cap(cr.semaphore); i++ {
		cr.semaphore <- struct{}{}
	}
}
