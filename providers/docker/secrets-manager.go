package cloudcompute

import (
	. "github.com/usace/cloudcompute"
)

// create a new in memory secret manager
// key is reserved for adding an encryption key should it be added later
func NewSecretManager(key string) *SecretsManager {
	return &SecretsManager{key: []byte(key)}
}

type SecretsManager struct {
	key     []byte
	secrets KeyValuePairs
}

func (sm *SecretsManager) AddSecret(key string, val string) {
	sm.secrets.SetVal(key, val)
}

func (sm *SecretsManager) GetSecret(key string) string {
	return sm.secrets.GetVal(key)
}
