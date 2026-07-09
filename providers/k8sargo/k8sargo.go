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
	templateEntrypointName string = "cc-entrypoint"
	defaultNamespace       string = "argo"
	computeLabel           string = "compute"
	eventLabel             string = "event"
	jobLabel               string = "job"
	ccEventIdentifier      string = "CC_EVENT_IDENTIFIER"
)

var argoToCcStatusMap = map[string]string{
	"Pending":   "PENDING",
	"Running":   "RUNNING",
	"Succeeded": "SUCCEDED",
	"Skipped":   "FAILED",
	"Failed":    "FAILED",
	"Error":     "FAILED",
	"Omitted":   "FAILED",
}

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
	eventIdentifier := input.Jobs[0].ContainerOverrides.Environment.GetVal(ccEventIdentifier)
	eventId := fmt.Sprintf("%s.%s", input.Jobs[0].EventID.String(), eventIdentifier)
	workflowName := eventId
	if input.Jobs[0].PerEventLoopNum > 0 {
		workflowName += fmt.Sprintf(".%d", input.Jobs[0].PerEventLoopNum)
	}

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
					template.Inputs.Parameters = append(template.Inputs.Parameters, wfv1.Parameter{Name: jobLabel})
					template.Metadata.Labels = map[string]string{
						jobLabel: "{{inputs.parameters.job}}",
					}
					//template.Metadata.Annotations = job.Tags
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

		//Dag Task Specific Parameters
		dagTaskParameters := []v1alpha1.Parameter{}

		//marshall to json so that the env vars can be merged into the podSpecPatch
		envJson, err := json.Marshal(tmplEnv)
		if err != nil {
			return err
		}
		dagTaskParameters = append(dagTaskParameters, v1alpha1.Parameter{
			Name:  "DagTaskEnv",
			Value: v1alpha1.AnyStringPtr(string(envJson)),
		})

		for _, resourceRequirement := range job.ContainerOverrides.ResourceRequirements {

			switch resourceRequirement.Type {
			case cc.ResourceTypeVcpu:
				//vcpu := getResourceOrDefault(job.ContainerOverrides.ResourceRequirements, cc.ResourceTypeVcpu, "1")
				dagTaskParameters = append(dagTaskParameters, v1alpha1.Parameter{
					Name:  "VCPU",
					Value: v1alpha1.AnyStringPtr(resourceRequirement.Value),
				})
			case cc.ResourceTypeMemory:
				//memory := getResourceOrDefault(job.ContainerOverrides.ResourceRequirements, cc.ResourceTypeMemory, "1240Mi")
				dagTaskParameters = append(dagTaskParameters, v1alpha1.Parameter{
					Name:  "Memory",
					Value: v1alpha1.AnyStringPtr(resourceRequirement.Value),
				})
			}
		}

		for k, v := range job.Tags {
			dagTaskParameters = append(dagTaskParameters, v1alpha1.Parameter{
				Name:  k,
				Value: v1alpha1.AnyStringPtr(v),
			})
		}

		//start building the DAG Task
		submittedJobName := fmt.Sprintf("j-%s", job.ID.String())
		dagTask := v1alpha1.DAGTask{
			Name:         submittedJobName,
			Template:     job.JobDefinition,
			Dependencies: depsToArgoDeps(job.DependsOn),
			Arguments: v1alpha1.Arguments{
				Parameters: dagTaskParameters,
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
		job.SubmittedJob = &cc.SubmitJobResult{
			JobId: &submittedJobName,
		}
	}

	//create the workflow which will run the event
	wf := &v1alpha1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workflowName,
			Namespace: *a.namespace,
			Labels: map[string]string{
				computeLabel: input.ComputeId.String(),
				eventLabel:   eventId,
			},
		},
		Spec: v1alpha1.WorkflowSpec{
			Entrypoint: templateEntrypointName,
			Templates: []v1alpha1.Template{
				{
					Name: templateEntrypointName,
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

func (a *ArgoWorkflowComputeProvider) TerminateJobs(input cc.TerminateJobInput) error {

	listReq := &workflowpkg.WorkflowListRequest{
		Namespace: *a.namespace,
		ListOptions: &metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", computeLabel, input.Query.QueryValue.Compute),
		},
	}

	workflowList, err := a.serviceClient.ListWorkflows(a.ctx, listReq)
	if err != nil {
		return err
	}

	for _, workflow := range workflowList.Items {
		stopReq := &workflowpkg.WorkflowStopRequest{
			Name:      workflow.Name,
			Namespace: *a.namespace,
			// Target the specific node where the input parameter 'task-guid' matches your ID
			//NodeFieldSelector: "inputs.parameters.task-guid.value=" + guid,
			Message: "Stopping Compute because i screwed up",
		}

		_, err := a.serviceClient.StopWorkflow(a.ctx, stopReq)
		log.Println(err)
	}

	return nil
}

func (a *ArgoWorkflowComputeProvider) JobLog(input cc.JobLogInput) (cc.JobLogOutput, error) {

	output := cc.JobLogOutput{}

	jobSelector := fmt.Sprintf("job=%s", input.VendorJobId) //jobid
	logReq := &workflow.WorkflowLogRequest{
		Namespace: *a.namespace,
		Name:      input.EventId, //eventid
		Selector:  jobSelector,
		LogOptions: &corev1.PodLogOptions{
			Container: "main",
			Follow:    true,
		},
	}

	stream, err := a.serviceClient.WorkflowLogs(a.ctx, logReq)
	if err != nil {
		return output, err
	}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return output, err
		}
		output.Logs = append(output.Logs, resp.Content)
	}

	//@TODO implement continuation tokens for cloud compute log requests
	return output, nil

}

func (a *ArgoWorkflowComputeProvider) Status(jobQueue string, query cc.JobsSummaryQuery) error {
	labelSelector := fmt.Sprintf("%s=%s", computeLabel, query.QueryValue.Compute)

	req := &workflow.WorkflowListRequest{
		Namespace: *a.namespace,
		ListOptions: &metav1.ListOptions{
			LabelSelector: labelSelector,
		},
	}

	wfList, err := a.serviceClient.ListWorkflows(context.Background(), req)
	if err != nil {
		fmt.Printf("Error listing workflows: %v\n", err)
		return err
	}

	summaries := []cc.JobSummary{}

	for _, wf := range wfList.Items {
		createdTime := wf.CreationTimestamp.UnixMilli()
		for _, node := range wf.Status.Nodes {
			if node.Type == wfv1.NodeTypePod || node.Type == wfv1.NodeTypeContainer {
				startTime := node.StartedAt.Time.UnixMilli()
				endTime := node.FinishedAt.Time.UnixMilli()
				jobid := ""
				for _, p := range node.Inputs.Parameters {
					if p.Name == jobLabel {
						jobid = p.GetValue()
					}
				}
				summaries = append(summaries, cc.JobSummary{
					JobId:        jobid,
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
	}
	query.JobSummaryFunction(summaries)
	return nil
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

	if plugin.RetryAttempts > 0 {
		tmpl.RetryStrategy = &wfv1.RetryStrategy{
			Limit: &intstr.IntOrString{IntVal: plugin.RetryAttempts},
		}
	}

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
