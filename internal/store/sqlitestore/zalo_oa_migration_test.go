//go:build sqlite || sqliteonly

package sqlitestore

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func TestEnsureSchema_ZaloBotRetypePreservesCredentials(t *testing.T) {
	db := openTestDBAtVersion(t, 60)
	tenant := seedTenant(t, db, "zalo-migrate")
	agent := seedChannelAgent(t, db, tenant, "zalo-migrate")
	credentials := []byte{0, 1, 255, 42}
	ids := map[string]string{"zalo_oa": uuid.NewString(), "zalo_personal": uuid.NewString()}
	for channelType, id := range ids {
		if _, err := db.Exec(`INSERT INTO channel_instances (id, name, channel_type, agent_id, tenant_id, credentials) VALUES (?, ?, ?, ?, ?, ?)`, id, channelType, channelType, agent, tenant, credentials); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	for original, want := range map[string]string{"zalo_oa": "zalo_bot", "zalo_personal": "zalo_personal"} {
		var channelType string
		var got []byte
		if err := db.QueryRow(`SELECT channel_type, credentials FROM channel_instances WHERE id = ?`, ids[original]).Scan(&channelType, &got); err != nil {
			t.Fatal(err)
		}
		if channelType != want || !bytes.Equal(got, credentials) {
			t.Fatalf("migration corrupted %s: type=%s credentials preserved=%t", original, channelType, bytes.Equal(got, credentials))
		}
	}
}
