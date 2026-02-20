package k8sargo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/argoproj/argo-workflows/v3/pkg/apiclient"
	"github.com/argoproj/argo-workflows/v3/pkg/apiclient/workflow"
	workflowpkg "github.com/argoproj/argo-workflows/v3/pkg/apiclient/workflow"
	workflowtemplatepb "github.com/argoproj/argo-workflows/v3/pkg/apiclient/workflowtemplate"
	"github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/google/uuid"
	cc "github.com/usace-cloud-compute/cloudcompute"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	defaultNamespace string = "argo"
)

type ArgoWorkflowComputeProviderConfig struct {
	Namespace  string `json:"namespace"`
	ServiceUrl string `json:"service-url"`
}

type ArgoWorkflowComputeProvider struct {
	client        apiclient.Client
	serviceClient workflowpkg.WorkflowServiceClient
	ctx           context.Context
	namespace     *string
}

func NewArgoWorkflowComputeProvider(config ArgoWorkflowComputeProviderConfig) (*ArgoWorkflowComputeProvider, error) {
	// var (
	// 	//argoServer = flag.String("argo-server", getEnvOrDefault("ARGO_SERVER", "localhost:2746"), "Argo Server address")
	// 	token = flag.String("token", os.Getenv("ARGO_TOKEN"), "Bearer token for authentication")
	// 	//namespace  = flag.String("namespace", "argo", "namespace for workflow")
	// 	secure   = flag.Bool("secure", true, "whether the Argo Server uses TLS")
	// 	insecure = flag.Bool("insecure-skip-verify", true, "skip TLS certificate verification")
	// )
	// flag.Parse()

	secure := true
	insecure := true
	token := ""

	if config.ServiceUrl == "" {
		return nil, fmt.Errorf("ServiceUrl is a required configuration option")
	}

	if config.Namespace == "" {
		config.Namespace = defaultNamespace
	}

	ctx, client, err := apiclient.NewClientFromOpts(apiclient.Opts{
		ArgoServerOpts: apiclient.ArgoServerOpts{
			URL:                config.ServiceUrl,
			Secure:             secure,
			InsecureSkipVerify: insecure,
		},
		AuthSupplier: func() string {
			if token != "" {
				return token
			}
			return ""
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error creating client: %s", err)
	}

	// Create workflow service client
	serviceClient := client.NewWorkflowServiceClient()

	return &ArgoWorkflowComputeProvider{
		client:        client,
		serviceClient: serviceClient,
		ctx:           ctx,
		namespace:     &config.Namespace,
	}, nil

}

func (a *ArgoWorkflowComputeProvider) SubmitJobs(input cc.SubmitJobsInput) error {

	//within the argo environment, events will be submitted as s single workflow
	//the event id will be used for the workflow name
	eventId := input.Jobs[0].EventID.String()

	//for each job in the cloud compute event, we create an argo DAG Task
	tasks := make([]v1alpha1.DAGTask, len(input.Jobs))

	//argo templates will store the DAG Task and template specs for the job definitions
	templates := []v1alpha1.Template{}

	//build the job dependency graph from the manifest dependencies
	manifestDepsToJobDeps(input.Jobs)

	for i, job := range input.Jobs {

		//look to see if a "job definition" template already exists
		template := getTemplate(templates, job.JobDefinition)

		//if "job definition" template does not exist, clone a saved workflow template
		//and build the job template.
		//@TODO: initially this strategy was used to better support dynamic parameters in jobs
		//but because some parameters are required to be in "podSpecPatch" format,
		// I'm not sure if we need to close these templates anymore
		if template == nil {
			templateClient, err := a.client.NewWorkflowTemplateServiceClient()
			if err != nil {
				return err
			}

			req := &workflowtemplatepb.WorkflowTemplateGetRequest{
				Namespace: *a.namespace,
				Name:      job.JobDefinition,
			}

			wft, err := templateClient.GetWorkflowTemplate(a.ctx, req)
			if err != nil {
				return err
			}

			for _, specTmpl := range wft.Spec.Templates {
				if specTmpl.Name == job.JobDefinition {
					template = specTmpl.DeepCopy()
					templates = append(templates, *template)
				}
			}
		}

		if template == nil {
			return fmt.Errorf("missing template: %s", job.JobDefinition)
		}

		//create the environment variables unique to this task/job
		tmplEnv := make([]corev1.EnvVar, len(job.ContainerOverrides.Environment))
		for i, envVal := range job.ContainerOverrides.Environment {
			tmplEnv[i] = corev1.EnvVar{
				Name:  envVal.Name,
				Value: envVal.Value,
			}
		}

		//marshall to json so that the env vars can be merged into the podSpecPatch
		envJson, err := json.Marshal(tmplEnv)
		if err != nil {
			return err
		}

		vcpu := getResourceOrDefault(job.ContainerOverrides.ResourceRequirements, cc.ResourceTypeVcpu, "1")
		memory := getResourceOrDefault(job.ContainerOverrides.ResourceRequirements, cc.ResourceTypeMemory, "256Mi")

		//start building the DAG Task
		dagTask := v1alpha1.DAGTask{
			Name:         fmt.Sprintf("e-%s", job.ID.String()),
			Template:     job.JobDefinition,
			Dependencies: depsToArgoDeps(job.DependsOn),
			Arguments: v1alpha1.Arguments{
				Parameters: []v1alpha1.Parameter{
					{
						Name:  "VCPU",
						Value: v1alpha1.AnyStringPtr(vcpu),
					},
					{
						Name:  "Memory",
						Value: v1alpha1.AnyStringPtr(memory),
					},
					{
						Name:  "DagTaskEnv",
						Value: v1alpha1.AnyStringPtr(string(envJson)),
					},
				},
			},
		}

		//add any command/argument overrides for each task/job
		if len(job.ContainerOverrides.Command) > 0 {
			dagTask.Arguments.Parameters = append(dagTask.Arguments.Parameters, v1alpha1.Parameter{
				Name:  "ExecCommand",
				Value: v1alpha1.AnyStringPtr(job.ContainerOverrides.Command[0]),
			})
			if len(job.ContainerOverrides.Command) > 1 {
				args := job.ContainerOverrides.Command[1:]
				jsonArgs, err := json.Marshal(args)
				if err != nil {
					return err
				}
				dagTask.Arguments.Parameters = append(dagTask.Arguments.Parameters, v1alpha1.Parameter{
					Name:  "ExecArgs",
					Value: v1alpha1.AnyStringPtr(string(jsonArgs)),
				})
			} else {
				dagTask.Arguments.Parameters = append(dagTask.Arguments.Parameters, v1alpha1.Parameter{
					Name:  "ExecArgs",
					Value: v1alpha1.AnyStringPtr("[]"),
				})
			}

		}

		tasks[i] = dagTask
	}

	//create the workflow which will run the event
	wf := &v1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eventId,
			Namespace: *a.namespace,
		},
		Spec: v1alpha1.WorkflowSpec{
			Entrypoint: "cc-entrypoint",
			Templates: []v1alpha1.Template{
				{
					Name: "cc-entrypoint",
					DAG: &v1alpha1.DAGTemplate{
						Tasks: tasks,
					},
				},
			},
		},
	}

	//append the jobdefinition templates to the workflow
	wf.Spec.Templates = append(wf.Spec.Templates, templates...)

	//send the workflow to argo using the service client
	_, err := a.serviceClient.CreateWorkflow(context.Background(), &workflow.WorkflowCreateRequest{
		Namespace: *a.namespace,
		Workflow:  wf,
	})

	if err != nil {
		return err
	}

	return nil
}

func (a *ArgoWorkflowComputeProvider) RegisterPlugin(plugin *cc.Plugin) (cc.PluginRegistrationOutput, error) {
	templateClient, err := a.client.NewWorkflowTemplateServiceClient()
	if err != nil {
		return cc.PluginRegistrationOutput{}, err
	}

	workflowTemplate, err := pluginToWorkflowTemplate(plugin)
	if err != nil {
		return cc.PluginRegistrationOutput{}, err
	}

	req := &workflowtemplatepb.WorkflowTemplateCreateRequest{
		Namespace: *a.namespace,
		Template:  workflowTemplate,
	}

	// Store template in Argo
	resp, err := templateClient.CreateWorkflowTemplate(a.ctx, req)
	if err != nil {
		log.Println(err)
		return cc.PluginRegistrationOutput{}, err
	}
	//fmt.Println(resp.GenerateName)

	return cc.PluginRegistrationOutput{
		Name:         strings.ToLower(resp.Name),
		ResourceName: fmt.Sprintf("%s::%s::%s::%s", resp.ObjectMeta.Namespace, resp.ObjectMeta.Name, resp.ObjectMeta.UID, resp.ObjectMeta.ResourceVersion),
		Revision:     int32(resp.ObjectMeta.Generation),
	}, nil
}

func (a *ArgoWorkflowComputeProvider) UnregisterPlugin(nameAndRevision string) error {

	templateClient, err := a.client.NewWorkflowTemplateServiceClient()

	_, err = templateClient.DeleteWorkflowTemplate(a.ctx, &workflowtemplatepb.WorkflowTemplateDeleteRequest{
		Name:      nameAndRevision,
		Namespace: *a.namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to unregister %s: %s", nameAndRevision, err)
	}

	return nil
}

// argo workflows will use the event identifier as the Log Request Name, and the pod name for a specific filter
func (a *ArgoWorkflowComputeProvider) JobLog(input cc.JobLogInput) (cc.JobLogOutput, error) {
	req := &workflowpkg.WorkflowLogRequest{
		Namespace: *a.namespace,
		Name:      input.VendorJobId,
		// Optional: specify a specific podName to only get logs for one step
		PodName: input.AltId,
		LogOptions: &corev1.PodLogOptions{
			Container: "main",
			Follow:    false, // Set to false for completed jobs
		},
	}

	//Open the log stream
	stream, err := a.serviceClient.WorkflowLogs(a.ctx, req)
	if err != nil {
		return cc.JobLogOutput{}, fmt.Errorf("failed to open log stream: %v", err)
	}

	// Process the stream
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break // End of logs reached
		}
		if err != nil {
			return cc.JobLogOutput{}, fmt.Errorf("error reading stream: %v", err)
		}

		// Print the log line
		fmt.Println(event.Content)
	}

	//@TODO implement continuation tokens for cloud compute log requests
	return cc.JobLogOutput{}, nil

}

// @TODO NOT WORKING OR TESTED
func (a *ArgoWorkflowComputeProvider) TerminateJobs(input cc.TerminateJobInput) error {

	_, err := a.serviceClient.TerminateWorkflow(context.Background(), &workflow.WorkflowTerminateRequest{
		Name:      "test",
		Namespace: *a.namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to terminate: %v", err)
	}
	return nil
}

func (a *ArgoWorkflowComputeProvider) Status(jobQueue string, query cc.JobsSummaryQuery) error {
	wf, err := a.serviceClient.GetWorkflow(context.Background(), &workflowpkg.WorkflowGetRequest{
		Namespace: *a.namespace,
		Name:      query.QueryValue.Event,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Workflow: %s | Phase: %s\n", wf.Name, wf.Status.Phase)
	createdTime := wf.CreationTimestamp.UnixMilli()
	fmt.Println("Individual Task Statuses:")

	// 2. Iterate through Status.Nodes to find individual task results
	//for nopw load into single array
	//summaries:=make([]cc.JobSummary,len(wf.Status.Nodes))
	summaries := []cc.JobSummary{}

	for _, node := range wf.Status.Nodes {
		// Node types include: "Pod", "Container", "DAG", "Steps"
		// Most DAG tasks appear as "Pod" or "Container" types once running
		if node.Type == wfv1.NodeTypePod || node.Type == wfv1.NodeTypeContainer {
			startTime := node.StartedAt.Time.UnixMilli()
			endTime := node.FinishedAt.Time.UnixMilli()
			summaries = append(summaries, cc.JobSummary{
				JobId:        node.ID,
				JobName:      node.DisplayName,
				CreatedAt:    &createdTime,
				StartedAt:    &startTime,
				Status:       argoToCcStatusMap[string(node.Phase)],
				StatusDetail: &node.Message,
				StoppedAt:    &endTime,
				ResourceName: node.TemplateName,
			})
		}
	}
	query.JobSummaryFunction(summaries)
	return nil
}

var argoToCcStatusMap = map[string]string{
	"Pending":   "PENDING",
	"Running":   "RUNNING",
	"Succeeded": "SUCCEDED",
	"Skipped":   "FAILED",
	"Failed":    "FAILED",
	"Error":     "FAILED",
	"Omitted":   "FAILED",
}

func pluginToWorkflowTemplate(plugin *cc.Plugin) (*wfv1.WorkflowTemplate, error) {

	wfTemplate := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: strings.ToLower(plugin.Name),
		},
		Spec: wfv1.WorkflowSpec{},
	}

	//CPU and Memory are required for templates and can be overriden by the DAG
	//therefore they must be configured as Parameters with a k8s podSpecPatch
	podSpecPatch, err := getDefaultPodSpecPatchJson(plugin)
	if err != nil {
		return nil, err
	}

	//get command and args in json format
	var jsonArgs []byte
	if len(plugin.Command) > 1 {
		args := plugin.Command[1:]
		jsonArgs, err = json.Marshal(args)
		if err != nil {
			return nil, err
		}
	} else {
		jsonArgs = []byte("[]")
	}

	//create the CPU and Memory Parameters
	//Subsitition values are VCPU and Memory
	//VCPU is in whole units of cpu threads
	//Memory is in mebibytes (Mi)
	parameters := []wfv1.Parameter{
		{
			Name:  "VCPU",
			Value: wfv1.AnyStringPtr(plugin.ComputeEnvironment.VCPU),
		},
		{
			Name:  "Memory",
			Value: wfv1.AnyStringPtr(fmt.Sprintf("%s%s", plugin.ComputeEnvironment.Memory, "Mi")),
		},
		{
			Name:    "DagTaskEnv",
			Default: wfv1.AnyStringPtr(string("{}")),
		},
		{
			Name:    "ExecCommand",
			Default: wfv1.AnyStringPtr(plugin.Command[0]),
		},
		{
			Name:    "ExecArgs",
			Default: wfv1.AnyStringPtr(string(jsonArgs)),
		},
	}

	// if len(plugin.Command) > 0 {
	// 	parameters = append(parameters, []wfv1.Parameter{

	// 	}...)
	// }

	if plugin.ExecutionTimeout != nil && *plugin.ExecutionTimeout > 0 {
		parameters = append(parameters, wfv1.Parameter{
			Name:  "ExecutionTimeout",
			Value: wfv1.AnyStringPtr(fmt.Sprintf("%d", *plugin.ExecutionTimeout)),
		})
	}

	//build the template
	tmpl := wfv1.Template{
		Name:     strings.ToLower(plugin.Name),
		FailFast: toPtr(true),
		Inputs: wfv1.Inputs{
			Parameters: parameters,
		},
		PodSpecPatch: podSpecPatch,
		Container: &corev1.Container{
			Name:    "main",
			Image:   plugin.ImageAndTag,
			Command: []string{"{{inputs.parameters.ExecCommand}}"},
			Args:    []string{"{{inputs.parameters.ExecArgs}}"},
			Env: slices.Concat(ccEnvToTemplateEnv(plugin.DefaultEnvironment),
				ccCredentialsToTemplateEnv(plugin.Credentials)),
		},
	}

	if plugin.RetryAttemts > 0 {
		tmpl.RetryStrategy = &wfv1.RetryStrategy{
			Limit: &intstr.IntOrString{IntVal: plugin.RetryAttemts},
		}
	}

	// moved this to podspecpathc so that it can be dynamically changed at the job level
	// if plugin.ExecutionTimeout != nil && *plugin.ExecutionTimeout > 0 {
	// 	tmpl.ActiveDeadlineSeconds = &intstr.IntOrString{IntVal: *plugin.ExecutionTimeout}
	// }

	if plugin.Privileged {
		tmpl.Container.SecurityContext = &corev1.SecurityContext{
			Privileged: toPtr(true),
		}

		volumes := make([]corev1.Volume, len(plugin.LinuxParameters.Devices))
		volumeMounts := make([]corev1.VolumeMount, len(plugin.LinuxParameters.Devices))
		for i, v := range plugin.LinuxParameters.Devices {
			volumes[i] = corev1.Volume{
				Name: fmt.Sprintf("linuxparameter-device-%d", i),
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: *v.HostPath,
						Type: (*corev1.HostPathType)(toPtr("Directory")),
					},
				},
			}
			volumeMounts[i] = corev1.VolumeMount{
				Name:      fmt.Sprintf("linuxparameter-device-%d", i),
				MountPath: *v.ContainerPath,
			}
		}

		wfTemplate.Spec.Volumes = volumes
		tmpl.Container.VolumeMounts = volumeMounts
	}

	wfTemplate.Spec.Templates = []wfv1.Template{tmpl}

	return wfTemplate, nil
}

func ccEnvToTemplateEnv(pluginEnv cc.KeyValuePairs) []corev1.EnvVar {
	tmplEnv := make([]corev1.EnvVar, len(pluginEnv))
	for i, v := range pluginEnv {
		tEnv := corev1.EnvVar{
			Name:  v.Name,
			Value: v.Value,
		}
		tmplEnv[i] = tEnv
	}
	return tmplEnv
}

func ccCredentialsToTemplateEnv(pluginCreds cc.KeyValuePairs) []corev1.EnvVar {
	tmplEnv := make([]corev1.EnvVar, len(pluginCreds))
	for i, v := range pluginCreds {
		secretParts := strings.Split(v.Value, "::")
		tEnv := corev1.EnvVar{
			Name: v.Name,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretParts[0],
					},
					Key: secretParts[1],
				},
			},
		}
		tmplEnv[i] = tEnv
	}
	return tmplEnv
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func depsToArgoDeps(deps []string) []string {
	argoDeps := make([]string, len(deps))
	for i, dep := range deps {
		argoDeps[i] = fmt.Sprintf("e-%s", dep)
	}
	return argoDeps
}

func manifestDepsToJobDeps(jobs []*cc.Job) {
	for i, job := range jobs {
		if len(job.ManifestDependencies) > 0 {
			jobDependencies := make([]string, len(job.ManifestDependencies))
			for k, manifestDependency := range job.ManifestDependencies {
				id := getJobIdFromManifestId(manifestDependency, jobs)
				jobDependencies[k] = id
			}
			jobs[i].DependsOn = jobDependencies
		}
	}
}

func getJobIdFromManifestId(manifestId uuid.UUID, jobs []*cc.Job) string {
	for _, j := range jobs {
		if j.ManifestID == manifestId {
			return j.ID.String()
		}
	}
	return ""
}

// func depsToArgoDeps(job *cc.Job, jobs []*cc.Job) []string {
// 	if len(job.DependsOn) > 0 {
// 		argoDeps := make([]string, len(job.DependsOn))
// 		for i, dep := range job.DependsOn {
// 			argoDeps[i] = fmt.Sprintf("e-%s", dep)
// 		}
// 		return argoDeps
// 	} else if len(job.ManifestDependencies) > 0 {
// 		argoDeps := make([]string, len(job.ManifestDependencies))
// 		for i, dep := range job.ManifestDependencies {

// 			argoDeps[i] = fmt.Sprintf("e-%s", dep.String())
// 		}
// 		return argoDeps
// 	}
// 	return []string{}
// }

// func getJobByManifest(manifestId uuid.UUID, jobs []*cc.Job) *cc.Job {
// 	for i, j := range jobs {
// 		if j.ManifestID == manifestId {
// 			return jobs[i]
// 		}
// 	}
// 	return nil
// }

func getResourceOrDefault(resources []cc.ResourceRequirement, resourceType cc.ResourceType, defaultVal string) string {
	for _, resource := range resources {
		if resourceType == resource.Type {
			return resource.Value
		}
	}
	return defaultVal
}

func getTemplate(templates []v1alpha1.Template, name string) *v1alpha1.Template {
	for _, t := range templates {
		if t.Name == name {
			return &t
		}
	}
	return nil
}

func toPtr[T any](t T) *T {
	return &t
}
