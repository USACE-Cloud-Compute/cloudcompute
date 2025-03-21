package cloudcompute

import (
	"os"

	. "github.com/usace/cloudcompute"
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

// env sxecret manager
type EnvironmentSecretManager struct{}

func (sm *EnvironmentSecretManager) AddSecret(key string, val string) {
	os.Setenv(key, val) //@TODO this fails silently..
}

func (sm *EnvironmentSecretManager) GetSecret(key string) string {
	return os.Getenv(key)
}
