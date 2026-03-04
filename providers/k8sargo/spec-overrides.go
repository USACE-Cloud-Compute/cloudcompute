package k8sargo

import (
	"encoding/json"
	"strings"

	cc "github.com/usace-cloud-compute/cloudcompute"
)

const (
	containerName string = "main"
)

var dynamicSubstitutionFormats = map[string]string{
	"\"${activeDeadlineSeconds}\"": "{{inputs.parameters.ExecutionTimeout}}",
	"\"${env}\"":                   "{{inputs.parameters.DagTaskEnv}}",
	"\"${VCPU}\"":                  "\"{{inputs.parameters.VCPU}}\"",
	"\"${Memory}\"":                "\"{{inputs.parameters.Memory}}\"",
	"\"${command}\"":               "[\"{{inputs.parameters.ExecCommand}}\"]",
	"\"${args}\"":                  "{{=inputs.parameters.ExecArgs}}",
}

type PodSpecPatch struct {
	ActiveDeadlineSeconds string            `json:"activeDeadlineSeconds,omitempty"`
	Containers            []ContainerObject `json:"containers,omitempty"`
	NodeSelector          map[string]string `json:"nodeSelector,omitempty"`
}

// Tier 2 & 3: Collection of Objects
type ContainerObject struct {
	Name        string             `json:"name"` // The Merge Key
	Resources   *ResourceSubObject `json:"resources,omitempty"`
	Env         string             `json:"env,omitempty"`
	ExecCommand string             `json:"command,omitempty"`
	ExecArgs    string             `json:"args,omitempty"`
}

// Tier 3: Sub-Object
type ResourceSubObject struct {
	Limits   FieldMap `json:"limits,omitempty"`
	Requests FieldMap `json:"requests,omitempty"`
}

// Tier 4: Fields (The Leaf values)
type FieldMap map[string]string

func getDefaultPodSpecPatchJson(plugin *cc.Plugin) (string, error) {
	pspec := getDefaultPodSpecPatch(plugin)
	validjson, err := json.Marshal(pspec)
	invalidJson := validToInvalidJson(validjson)
	return string(invalidJson), err

}

// @TODO might be able to do away with this using the {{=...}} operator.
// substitution variables require invalid json strings.  We apply the final form here
func validToInvalidJson(validJson []byte) string {
	invalidJson := string(validJson)
	for k, v := range dynamicSubstitutionFormats {
		invalidJson = strings.ReplaceAll(invalidJson, k, v)
	}
	return invalidJson
}

func getDefaultPodSpecPatch(plugin *cc.Plugin) PodSpecPatch {
	psp := PodSpecPatch{
		Containers: []ContainerObject{
			{
				Name: containerName,
				Env:  "${env}",
				Resources: &ResourceSubObject{
					Limits: FieldMap{
						"cpu":    "${VCPU}",
						"memory": "${Memory}",
					},
					Requests: FieldMap{
						"cpu":    "${VCPU}",
						"memory": "${Memory}",
					},
				},
				ExecCommand: "${command}",
				ExecArgs:    "${args}",
			},
		},
	}

	if plugin.ExecutionTimeout != nil && *plugin.ExecutionTimeout > 0 {
		psp.ActiveDeadlineSeconds = "${activeDeadlineSeconds}"
	}
	return psp
}
