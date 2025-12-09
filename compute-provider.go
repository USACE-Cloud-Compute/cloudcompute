package cloudcompute

import (
	"errors"
	"regexp"

	"github.com/google/uuid"
)

type ResourceType string

type ComputeProviderType int

const (
	ResourceTypeGpu             ResourceType = "GPU"
	ResourceTypeVcpu            ResourceType = "VCPU"
	ResourceTypeMemory          ResourceType = "MEMORY"
	ResourceTypeAttachedStorage ResourceType = "ATTACHEDSTORAGE"

	SUMMARY_COMPUTE string = "COMPUTE"
	SUMMARY_EVENT   string = "EVENT"
	//SUMMARY_MANIFEST string = "MANIFEST"
	SUMMARY_JOB string = "JOB"

	AWSBATCH    ComputeProviderType = 0
	LOCALDOCKER ComputeProviderType = 1
	KUBERNETES  ComputeProviderType = 2
)

// Input for terminating jobs submitted to a queue.
// the list of jobs to terminate is determined by either
// a StatusQuery with each job in the status query being terminated
// or a list of VendorJobs for termination.
type TerminateJobInput struct {
	//Users reason for terminating the job
	Reason string

	//The Vendor Job Queue the job was submitted to
	JobQueue string

	//Optional. A jobs summary query that will generate a list of jobs to terminate
	//if this value is provided the JobSummaryQuery JobSummaryFunction is ignored
	//and should be left empty
	Query JobsSummaryQuery

	//Optional. A list of VendorJobs to terminate
	VendorJobs VendorJobs

	//Optional.  A function to process the results of each terminated job
	TerminateJobFunction TerminateJobFunction
}

type TerminateJobOutput struct {

	//CloudCompute Job Name
	JobName string

	//Error if returned from the terminate operation
	Err error

	//Vendor Job ID
	JobId string
}

// function to process the results of each job termination
type TerminateJobFunction func(output TerminateJobOutput)

// Interface for a compute provider.
type ComputeProvider interface {
	SubmitJob(job *Job) error
	TerminateJobs(input TerminateJobInput) error
	Status(jobQueue string, query JobsSummaryQuery) error
	JobLog(submittedJobId string, token *string) (JobLogOutput, error)
	RegisterPlugin(plugin *Plugin) (PluginRegistrationOutput, error)
	UnregisterPlugin(nameAndRevision string) error
}

type JobLogOutput struct {
	Logs  []string
	Token *string
}

// Overrides the container command or environment from the base values
// provided in the job description
type ContainerOverrides struct {
	Command              []string
	Environment          KeyValuePairs
	ResourceRequirements []ResourceRequirement
}

type ResourceRequirement struct {
	Type  ResourceType `json:"resource_type"`
	Value string       `json:"value"`
}

// This is a single "job" or unit of compute for a ComputeProvider
// Essentually it is a mapping of a single Manifest
type Job struct {
	ID                 uuid.UUID
	EventID            uuid.UUID
	ManifestID         uuid.UUID
	JobName            string
	JobQueue           string
	JobDefinition      string
	ContainerOverrides ContainerOverrides
	DependsOn          []string //compute provider dependencies
	Parameters         map[string]string
	Tags               map[string]string
	RetryAttemts       int32
	JobTimeout         int32            //duration in seconds
	SubmittedJob       *SubmitJobResult //reference to the job information from the compute environment
}

// Vendor job data used for terminating jobs on the vendor's compute environment
// @TODO, why can't this be replaced by SubmitJobResult?
type VendorJob interface {
	ID() string
	Name() string
}

type VendorJobs []VendorJob

func (vj VendorJobs) IncludesJob(id string) bool {
	for _, v := range vj {
		if v.ID() == id {
			return true
		}
	}
	return false
}

// Vendor job information
type SubmitJobResult struct {

	//Vendor ID
	JobId *string

	//Vendor Resource Name
	ResourceName *string //ARN in AWS
}

// function to process the results of a JobSummary request
// summaries are processed in batches of JobSummaries
// but will continue until all jobs are reported.  In AWS
// this processes the slice of summaries for the initial
// request and all subsequenct continutation tokens
type JobSummaryFunction func(summaries []JobSummary)

type JobSummary struct {
	//identifier for the compute environment being used.  e.g. AWS Batch Job ID
	JobId string

	//cloud compute job name
	JobName string

	//unix timestamp in milliseconds for when the job was created
	CreatedAt *int64

	//unix timestamp in milliseconds for when the job was started
	StartedAt *int64

	//status string value
	Status string

	//human readable string of the status
	StatusDetail *string

	//unix timestamp in milliseconds for when the job was stopped
	StoppedAt *int64

	//Compute Vendor resource name for the job.  e.g. the Job ARN for AWS
	ResourceName string
}

func (js JobSummary) ID() string {
	return js.JobId
}

func (js JobSummary) Name() string {
	return js.JobName
}

type JobsSummaryQuery struct {
	//The Level to request.  Must be one of three values
	//COMPUTE/EVENT/MANIFEST as represented by the SUMMARY_{level} constants
	QueryLevel string

	//the GUIDs representing the referenced levels
	//The value must include preceding levels, so COMPUTE level must have at least the compute GUID
	//EVENT level must have the compuyte and event levels and MANIFEST level has all three guids
	QueryValue JobNameParts

	//a required function to process each job returned in the query
	JobSummaryFunction JobSummaryFunction
}

///////////////////

type JobsLogQuery struct {
	QueryLevel       string
	QueryValue       JobNameParts
	MatchValue       string //regex to match and only print matching lines
	JsScriptFunction any
}

/////////////////

type JobNameParts struct {
	Compute string
	Event   string
	Job     string
}

func (jnp *JobNameParts) Parse(jobname string) error {
	re := regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	guids := re.FindAllString(jobname, -1)
	if len(guids) != 3 {
		return errors.New("Invalid Job Name")
	}
	jnp.Compute = guids[0]
	jnp.Event = guids[1]
	jnp.Job = guids[2]
	return nil
}

type KeyValuePair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type KeyValuePairs []KeyValuePair

func (kvps *KeyValuePairs) HasKey(key string) bool {
	for _, kvp := range *kvps {
		if kvp.Name == key {
			return true
		}
	}
	return false
}

// designed to behave like a system env.  Value returned if key is found
// empty string returned otherwise
func (kvps *KeyValuePairs) GetVal(key string) string {
	for _, kvp := range *kvps {
		if kvp.Name == key {
			return kvp.Value
		}
	}
	return ""
}

func (kvps *KeyValuePairs) SetVal(key string, val string) {
	for i, kvp := range *kvps {
		if kvp.Name == key {
			(*kvps)[i].Value = val
			return
		}
	}
	// doesn't exist, add a new value
	*kvps = append(*kvps, KeyValuePair{
		Name:  key,
		Value: val,
	})
}

func (kvps *KeyValuePairs) Merge(newKvps *KeyValuePairs) {
	for _, kvp := range *newKvps {
		kvps.SetVal(kvp.Name, kvp.Value)
	}
}

func MapToKeyValuePairs(mapdata map[string]string) KeyValuePairs {
	kvps := KeyValuePairs{}
	for k, v := range mapdata {
		kvps.SetVal(k, v)
	}
	return kvps
}
