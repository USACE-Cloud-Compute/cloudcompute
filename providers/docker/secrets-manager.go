package cloudcompute

import (
	"os"

	. "github.com/usace-cloud-compute/cloudcompute"
)

type SecretsManager interface {
	AddSecret(key string, val string)
	GetSecret(key string) string
}

// create a new in memory secret manager
// key is reserved for adding an encryption key should it be added later
func NewInMemorySecretsManager(key string) SecretsManager {
	return &InMemorySecretsManager{encryptionKey: []byte(key)}
}

type InMemorySecretsManager struct {
	encryptionKey []byte
	secrets       KeyValuePairs
}

func (sm *InMemorySecretsManager) AddSecret(key string, val string) {
	sm.secrets.SetVal(key, val)
}

func (sm *InMemorySecretsManager) GetSecret(key string) string {
	return sm.secrets.GetVal(key)
}

// env seccret manager

func NewEnvironmentSecretsManager() SecretsManager {
	return &EnvironmentSecretManager{
		secrets: make(map[string]string),
	}
}

type EnvironmentSecretManager struct {
	secrets map[string]string
}

func (sm *EnvironmentSecretManager) AddSecret(key string, val string) {
	sm.secrets[key] = val
}

func (sm *EnvironmentSecretManager) GetSecret(key string) string {
	return os.Getenv(sm.secrets[key])
}
