package mattermost

import (
	"os"
	"testing"
)

// The fake Mattermost server listens on 127.0.0.1, which the egress policy
// only allows for internal servers.
func TestMain(m *testing.M) {
	allowPrivateFn = func() bool { return true }
	os.Exit(m.Run())
}
