package channels

import "testing"

func TestIsNonSecretCredentialKey_ZaloOA(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"app_id", "oa_id", "redirect_uri"} {
		if !IsNonSecretCredentialKey(TypeZaloOA, key) {
			t.Errorf("%s must be unmasked for zalo_oa", key)
		}
	}
	if IsNonSecretCredentialKey(TypeZaloOA, "secret_key") {
		t.Error("secret_key must stay masked")
	}
	if IsNonSecretCredentialKey(TypeZaloBot, "app_id") {
		t.Error("bot channel must not inherit OA unmask list")
	}
}
