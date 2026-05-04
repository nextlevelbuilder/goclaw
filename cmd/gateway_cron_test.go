package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestBuildCronExtraPromptLocales(t *testing.T) {
	job := &store.CronJob{
		ID:             "job-id",
		Name:           "morning-briefing",
		UserID:         "alice",
		Deliver:        true,
		DeliverChannel: "telegram-atlas",
		DeliverTo:      "8707445232",
		TenantID:       uuid.New(),
	}

	cases := []struct {
		name     string
		ctxLoc   string // explicit ctx locale, "" = use cascade default
		envLoc   string // GOCLAW_DEFAULT_LOCALE override
		wantSub  string // substring that must appear in result
	}{
		{name: "ctx locale ko wins", ctxLoc: "ko", wantSub: "[크론 작업]"},
		{name: "ctx locale vi wins", ctxLoc: "vi", wantSub: "Tác vụ định kỳ"},
		{name: "ctx locale zh wins", ctxLoc: "zh", wantSub: "计划任务"},
		{name: "ctx locale en wins", ctxLoc: "en", wantSub: "[Cron Job]"},
		{name: "no ctx locale, env=ko", envLoc: "ko", wantSub: "[크론 작업]"},
		{name: "no ctx locale, env=vi", envLoc: "vi", wantSub: "Tác vụ định kỳ"},
		{name: "no ctx locale, no env → en", wantSub: "[Cron Job]"},
		{name: "ctx wins over env", ctxLoc: "ko", envLoc: "vi", wantSub: "[크론 작업]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOCLAW_DEFAULT_LOCALE", tc.envLoc)
			t.Setenv("LC_ALL", "")
			t.Setenv("LC_MESSAGES", "")
			t.Setenv("LANG", "")
			i18n.ResetDefaultForTest()

			ctx := context.Background()
			if tc.ctxLoc != "" {
				ctx = store.WithLocale(ctx, tc.ctxLoc)
			}
			got := buildCronExtraPrompt(ctx, job)
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("expected %q in result, got:\n%s", tc.wantSub, got)
			}
			// Sanity: output references job name + ID.
			if !strings.Contains(got, job.Name) || !strings.Contains(got, job.ID) {
				t.Errorf("result missing job identity: %s", got)
			}
		})
	}
}

func TestBuildCronExtraPromptWithoutDelivery(t *testing.T) {
	job := &store.CronJob{
		ID:      "job-id",
		Name:    "no-delivery-job",
		UserID:  "alice",
		Deliver: false,
	}
	t.Setenv("GOCLAW_DEFAULT_LOCALE", "ko")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	i18n.ResetDefaultForTest()

	got := buildCronExtraPrompt(context.Background(), job)
	if !strings.Contains(got, "전달 대상이 설정되어 있지 않으니") {
		t.Errorf("expected no-delivery Korean phrasing, got:\n%s", got)
	}
}
