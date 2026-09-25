//go:build integration

package integration

import (
	"bytes"
	"os"
	"testing"
)

func TestZaloOARetypeMigration(t *testing.T) {
	up, err := os.ReadFile("../../migrations/000098_zalo_oa_retype_channel.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/000098_zalo_oa_retype_channel.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := testDB(t).Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	// Shadow the real table so this data migration cannot alter another test's rows.
	if _, err := tx.Exec(`CREATE TEMP TABLE channel_instances (id text PRIMARY KEY, channel_type text, credentials bytea) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	credentials := []byte{0, 1, 255, 42} // opaque encrypted bytes, not parsed JSON
	if _, err := tx.Exec(`INSERT INTO channel_instances VALUES ('bot', 'zalo_oa', $1), ('personal', 'zalo_personal', $1)`, credentials); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		sql  []byte
		want string
	}{{up, "zalo_bot"}, {up, "zalo_bot"}, {down, "zalo_oa"}} {
		if _, err := tx.Exec(string(step.sql)); err != nil {
			t.Fatal(err)
		}
		for id, want := range map[string]string{"bot": step.want, "personal": "zalo_personal"} {
			var channelType string
			var got []byte
			if err := tx.QueryRow(`SELECT channel_type, credentials FROM channel_instances WHERE id = $1`, id).Scan(&channelType, &got); err != nil {
				t.Fatal(err)
			}
			if channelType != want || !bytes.Equal(got, credentials) {
				t.Fatalf("migration corrupted %s: type=%s, credentials preserved=%t", id, channelType, bytes.Equal(got, credentials))
			}
		}
	}
}
