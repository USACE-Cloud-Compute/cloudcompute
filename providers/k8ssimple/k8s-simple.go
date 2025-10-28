package k8ssimple

import (
	"context"
	"fmt"
	"os/user"

	. "github.com/usace-cloud-compute/cloudcompute"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type KubernetesComputeProviderConfig struct {
	Namespace  string
	Kubeconfig string // Path to kubeconfig file
}

type KubernetesComputeProvider struct {
	clientset *kubernetes.Clientset
	namespace string
}

func NewKubernetesComputeProvider(config KubernetesComputeProviderConfig) (*KubernetesComputeProvider, error) {
	var configPath string

	if config.Kubeconfig != "" {
		configPath = config.Kubeconfig
	} else {
		usr, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("failed to get current user: %v", err)
		}
		configPath = fmt.Sprintf("%s/.kube/config", usr.HomeDir)
	}

	clientConfig, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from flags: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %v", err)
	}

	return &KubernetesComputeProvider{
		clientset: clientset,
		namespace: config.Namespace,
	}, nil
}

func (k *KubernetesComputeProvider) SubmitJob(job *Job) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: job.JobName,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    job.JobName,
					Image:   job.JobDefinition, // Use JobDefinition as the image
					Command: job.ContainerOverrides.Command,
					EnvFrom: []corev1.EnvFromSource{
						{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "your-secret-name",
								},
							},
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	_, err := k.clientset.CoreV1().Pods(k.namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create pod: %v", err)
	}
	return nil
}

func (k *KubernetesComputeProvider) TerminateJobs(input TerminateJobInput) error {
	for _, job := range input.VendorJobs {
		err := k.clientset.CoreV1().Pods(k.namespace).Delete(context.TODO(), job.Name(), metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("failed to delete pod %s: %v", job.Name(), err)
		}
	}
	return nil
}

func (k *KubernetesComputeProvider) Status(jobQueue string, query JobsSummaryQuery) error {
	pods, err := k.clientset.CoreV1().Pods(k.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	for _, pod := range pods.Items {
		js := JobSummary{
			JobId:        pod.Name,
			JobName:      pod.Name,
			Status:       string(pod.Status.Phase),
			ResourceName: pod.Name, // Use pod name as resource name
		}
		query.JobSummaryFunction([]JobSummary{js})
	}
	return nil
}

func (k *KubernetesComputeProvider) JobLog(submittedJobId string, token *string) (JobLogOutput, error) {
	podLogs, err := k.clientset.CoreV1().Pods(k.namespace).GetLogs(submittedJobId, &corev1.PodLogOptions{}).DoRaw(context.TODO())
	if err != nil {
		return JobLogOutput{}, fmt.Errorf("failed to get pod logs: %v", err)
	}

	return JobLogOutput{
		Logs: []string{string(podLogs)},
	}, nil
}

func (k *KubernetesComputeProvider) RegisterPlugin(plugin *Plugin) error {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: plugin.Name,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: "Never",
					Containers: []corev1.Container{
						{
							Name:    plugin.Name,
							Image:   plugin.ImageAndTag,
							Command: plugin.Command,
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "your-secret-name",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := k.clientset.BatchV1().Jobs(k.namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create job: %v", err)
	}
	return nil
}

func (k *KubernetesComputeProvider) UnregisterPlugin(nameAndRevision string) error {
	err := k.clientset.BatchV1().Jobs(k.namespace).Delete(context.TODO(), nameAndRevision, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete job %s: %v", nameAndRevision, err)
	}
	return nil
}

func (k *KubernetesComputeProvider) PluginRegistrationOutput() (PluginRegistrationOutput, error) {
	return PluginRegistrationOutput{}, nil // Placeholder implementation
}

func convertToEnvVars(kvs KeyValuePairs) []corev1.EnvVar {
	envVars := make([]corev1.EnvVar, len(kvs))
	for i, kv := range kvs {
		envVars[i] = corev1.EnvVar{
			Name:  kv.Name,
			Value: kv.Value,
		}
	}
	return envVars
}
