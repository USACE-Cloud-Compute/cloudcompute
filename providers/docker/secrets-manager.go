package cloudcompute

import (
	. "github.com/usace/cloudcompute"
)

func NewSecretManager(key string) *SecretsManager {
	return &SecretsManager{}
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
