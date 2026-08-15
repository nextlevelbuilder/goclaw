package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCronCRUD_CreateGetUpdateToggleDelete(t *testing.T) {
	cron := newFakeCronStore()
	srv := newTestMCPServer()
	registerCronCRUDTools(srv, cron)

	created := callTool(t, srv, "goclaw_cron_create", map[string]any{
		"name":          "daily-report",
		"schedule_kind": "every",
		"every_ms":      float64(60000),
		"message":       "run report",
	})
	require.False(t, toolIsError(created), toolResultText(created))
	assert.Contains(t, toolResultText(created), "daily-report")

	var jobID string
	for id := range cron.jobs {
		jobID = id
	}

	got := callTool(t, srv, "goclaw_cron_get", map[string]any{"job_id": jobID})
	require.False(t, toolIsError(got))
	assert.Contains(t, toolResultText(got), "daily-report")

	notFound := callTool(t, srv, "goclaw_cron_get", map[string]any{"job_id": "missing"})
	assert.True(t, toolIsError(notFound))

	updated := callTool(t, srv, "goclaw_cron_update", map[string]any{"job_id": jobID, "name": "renamed"})
	require.False(t, toolIsError(updated))
	assert.Equal(t, "renamed", cron.jobs[jobID].Name)

	toggled := callTool(t, srv, "goclaw_cron_toggle", map[string]any{"job_id": jobID, "enabled": false})
	require.False(t, toolIsError(toggled))
	assert.False(t, cron.jobs[jobID].Enabled)

	deleted := callTool(t, srv, "goclaw_cron_delete", map[string]any{"job_id": jobID})
	require.False(t, toolIsError(deleted))

	deleteAgain := callTool(t, srv, "goclaw_cron_delete", map[string]any{"job_id": jobID})
	assert.True(t, toolIsError(deleteAgain))
}

func TestCronRun_JobNotFound(t *testing.T) {
	cron := newFakeCronStore()
	srv := newTestMCPServer()
	registerCronCRUDTools(srv, cron)

	result := callTool(t, srv, "goclaw_cron_run", map[string]any{"job_id": "missing", "mode": "force"})
	assert.True(t, toolIsError(result))
}

func TestCronList_ExcludesDisabledByDefault(t *testing.T) {
	cron := newFakeCronStore()
	srv := newTestMCPServer()
	registerCronCRUDTools(srv, cron)

	created := callTool(t, srv, "goclaw_cron_create", map[string]any{
		"name": "job1", "schedule_kind": "every", "every_ms": float64(1000), "message": "hi",
	})
	require.False(t, toolIsError(created))
	var jobID string
	for id := range cron.jobs {
		jobID = id
	}
	callTool(t, srv, "goclaw_cron_toggle", map[string]any{"job_id": jobID, "enabled": false})

	list := callTool(t, srv, "goclaw_cron_list", map[string]any{})
	require.False(t, toolIsError(list))
	assert.NotContains(t, toolResultText(list), "job1")

	listAll := callTool(t, srv, "goclaw_cron_list", map[string]any{"include_disabled": true})
	require.False(t, toolIsError(listAll))
	assert.Contains(t, toolResultText(listAll), "job1")
}

func TestCronStatus(t *testing.T) {
	cron := newFakeCronStore()
	srv := newTestMCPServer()
	registerCronCRUDTools(srv, cron)

	result := callTool(t, srv, "goclaw_cron_status", map[string]any{})
	require.False(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "status")
}
