package instagram

import (
	"testing"
)

func TestChannel_New(t *testing.T) {
	validCreds := instagramCreds{
		UserAccessToken: "test_token",
		AppSecret:       "test_secret",
		VerifyToken:     "test_verify",
	}
	validCfg := instagramInstanceConfig{InstagramUserID: "123456"}

	tests := []struct {
		name    string
		creds   instagramCreds
		config  instagramInstanceConfig
		wantErr bool
	}{
		{"valid credentials", validCreds, validCfg, false},
		{"missing user_access_token", instagramCreds{AppSecret: "s", VerifyToken: "v"}, validCfg, true},
		{"missing app_secret", instagramCreds{UserAccessToken: "t", VerifyToken: "v"}, validCfg, true},
		{"missing instagram_user_id", validCreds, instagramInstanceConfig{}, true},
		{"missing verify_token", instagramCreds{UserAccessToken: "t", AppSecret: "s"}, validCfg, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := New(tt.config, tt.creds, nil, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && ch == nil {
				t.Errorf("New() returned nil channel")
			}
		})
	}
}
