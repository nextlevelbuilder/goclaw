package workstation

import (
	"regexp"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// workstationKeyRe validates workstation_key format.
// Must start with alphanumeric and contain only lowercase letters, digits, hyphens.
// Max length 100 characters (enforced by DB VARCHAR(100)).
var workstationKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)

// ValidateWorkstationKey returns true if key matches the required format.
func ValidateWorkstationKey(key string) bool {
	return workstationKeyRe.MatchString(key)
}

// ValidateBackend returns true if the backend type is recognized.
func ValidateBackend(backend store.WorkstationBackend) bool {
	return backend == store.BackendSSH || backend == store.BackendDocker
}
