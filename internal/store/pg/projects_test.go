package pg

import (
	"testing"
)

func TestSecretKeyPatternRejectsSecrets(t *testing.T) {
	rejected := []string{
		"GITHUB_TOKEN", "github_token",
		"API_KEY", "MY_API_KEY",
		"DB_PASSWORD", "db_password",
		"CLIENT_SECRET", "client_secret",
		"ACCESS_TOKEN", "access_token",
	}
	for _, key := range rejected {
		if !secretKeyPattern.MatchString(key) {
			t.Errorf("expected %q to be rejected as secret, but it was allowed", key)
		}
	}
}

func TestSecretKeyPatternAllowsSafeKeys(t *testing.T) {
	allowed := []string{
		"PROJECT_ID", "GITHUB_REPO", "GITHUB_OWNER",
		"PROJECT_PATH", "WORKSPACE_DIR", "BRANCH_NAME",
		"ENVIRONMENT", "REGION", "CLUSTER_NAME",
	}
	for _, key := range allowed {
		if secretKeyPattern.MatchString(key) {
			t.Errorf("expected %q to be allowed, but it was rejected as secret", key)
		}
	}
}
