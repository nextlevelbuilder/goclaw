//go:build integration

package integration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func newTestChannel(tenantID uuid.UUID, handle string) *store.MpChannel {
	return &store.MpChannel{
		TenantID:     tenantID,
		Handle:       handle,
		BrandKey:     "test-brand",
		TZ:           "Asia/Seoul",
		HasMascot:    true,
		Launched:     true,
		Enabled:      true,
		SubsBaseline: 1000,
	}
}

func TestMeowStore_ChannelCRUDAndUpsert(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGMeowStore(db)

	ch := newTestChannel(tenantID, "@CrudChan")
	if err := ms.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel insert: %v", err)
	}
	if ch.ID == uuid.Nil {
		t.Fatal("expected channel ID populated after upsert")
	}

	got, err := ms.GetChannel(ctx, tenantID, ch.ID)
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if got.Handle != "@CrudChan" || got.SubsBaseline != 1000 {
		t.Fatalf("unexpected channel: %+v", got)
	}

	// Upsert again (same tenant+handle) updates in place, keeps id.
	ch.SubsBaseline = 2000
	if err := ms.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel update: %v", err)
	}
	byHandle, err := ms.GetChannelByHandle(ctx, tenantID, "@CrudChan")
	if err != nil {
		t.Fatalf("GetChannelByHandle: %v", err)
	}
	if byHandle.ID != ch.ID {
		t.Fatalf("upsert changed id: %s vs %s", byHandle.ID, ch.ID)
	}
	if byHandle.SubsBaseline != 2000 {
		t.Fatalf("upsert did not update subs_baseline: %d", byHandle.SubsBaseline)
	}

	list, err := ms.ListChannels(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(list))
	}
}

func TestMeowStore_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)
	ms := pg.NewPGMeowStore(db)
	ctxA := tenantCtx(tenantA)

	ch := newTestChannel(tenantA, "@IsoChan")
	if err := ms.UpsertChannel(ctxA, ch); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	post := &store.MpContentPost{
		TenantID:      tenantA,
		ChannelID:     ch.ID,
		ScheduledDate: time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC),
		Status:        store.MpPostDraft,
		KoText:        "안녕하세요",
		EnText:        "hello",
	}
	if err := ms.CreatePost(ctxA, post); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	// Tenant B must NOT see tenant A's channel or post (resolves to NotFound).
	if _, err := ms.GetChannel(tenantCtx(tenantB), tenantB, ch.ID); err != store.ErrMeowChannelNotFound {
		t.Fatalf("cross-tenant GetChannel: expected ErrMeowChannelNotFound, got %v", err)
	}
	if _, err := ms.GetPost(tenantCtx(tenantB), tenantB, post.ID); err != store.ErrMeowPostNotFound {
		t.Fatalf("cross-tenant GetPost: expected ErrMeowPostNotFound, got %v", err)
	}
	// Tenant A still resolves both.
	if _, err := ms.GetPost(ctxA, tenantA, post.ID); err != nil {
		t.Fatalf("same-tenant GetPost: %v", err)
	}

	// Cross-tenant child WRITES must be rejected (not just reads). Tenant B must
	// not be able to attach a post/metric/order to tenant A's channel/post.
	ctxB := tenantCtx(tenantB)
	badPost := &store.MpContentPost{
		TenantID: tenantB, ChannelID: ch.ID,
		ScheduledDate: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), Status: store.MpPostDraft,
	}
	if err := ms.CreatePost(ctxB, badPost); err != store.ErrMeowChannelNotFound {
		t.Fatalf("cross-tenant CreatePost: expected ErrMeowChannelNotFound, got %v", err)
	}
	badMetric := &store.MpChannelMetric{
		TenantID: tenantB, ChannelID: ch.ID,
		Date: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), SubscriberCount: 1,
	}
	if err := ms.UpsertMetric(ctxB, badMetric); err != store.ErrMeowChannelNotFound {
		t.Fatalf("cross-tenant UpsertMetric: expected ErrMeowChannelNotFound, got %v", err)
	}
	badOrder := &store.MpSmmOrder{TenantID: tenantB, PostID: post.ID, ServiceID: "views", Qty: 1}
	if err := ms.CreateOrder(ctxB, badOrder); err != store.ErrMeowPostNotFound {
		t.Fatalf("cross-tenant CreateOrder: expected ErrMeowPostNotFound, got %v", err)
	}
}

func TestMeowStore_UpsertChannelPreservesRuntimeFields(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGMeowStore(db)

	ch := newTestChannel(tenantID, "@PreserveChan")
	ch.SubsBaseline = 100
	if err := ms.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel insert: %v", err)
	}

	// Simulate runtime mutations an operator/resolver would make.
	if _, err := db.Exec(
		`UPDATE mp_channels SET chat_id = $2, enabled = false, smm_tier = 'gold', smm_caps = '{"views":50}'
		 WHERE id = $1`, ch.ID, "-1001234567"); err != nil {
		t.Fatalf("simulate runtime mutation: %v", err)
	}

	// Re-seed (as a startup re-run would) with refreshed registry fields.
	reseed := newTestChannel(tenantID, "@PreserveChan")
	reseed.SubsBaseline = 999
	reseed.ButtonSet = json.RawMessage(`[{"label":"X","url":"https://x.example"}]`)
	if err := ms.UpsertChannel(ctx, reseed); err != nil {
		t.Fatalf("UpsertChannel re-seed: %v", err)
	}

	got, err := ms.GetChannel(ctx, tenantID, ch.ID)
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	// Runtime-managed fields PRESERVED.
	if got.ChatID == nil || *got.ChatID != "-1001234567" {
		t.Errorf("chat_id not preserved on re-seed: %v", got.ChatID)
	}
	if got.Enabled {
		t.Error("enabled should stay false (operator-disabled) after re-seed")
	}
	if got.SMMTier != "gold" {
		t.Errorf("smm_tier not preserved: %q", got.SMMTier)
	}
	// Registry-authoritative fields REFRESHED.
	if got.SubsBaseline != 999 {
		t.Errorf("subs_baseline not refreshed: %d", got.SubsBaseline)
	}
	if string(got.ButtonSet) == "[]" {
		t.Error("button_set should have been refreshed from registry")
	}
}

func TestMeowStore_PublishExactlyOnce(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGMeowStore(db)

	ch := newTestChannel(tenantID, "@OnceChan")
	if err := ms.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	day := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	mk := func() *store.MpContentPost {
		return &store.MpContentPost{TenantID: tenantID, ChannelID: ch.ID, ScheduledDate: day, Status: store.MpPostDraft}
	}
	p1, p2 := mk(), mk()
	if err := ms.CreatePost(ctx, p1); err != nil {
		t.Fatalf("CreatePost p1: %v", err)
	}
	if err := ms.CreatePost(ctx, p2); err != nil {
		t.Fatalf("CreatePost p2 (both draft is allowed): %v", err)
	}

	// First publish of the channel-day succeeds.
	if err := ms.UpdatePostStatus(ctx, tenantID, p1.ID, store.MpPostPublished, ptrInt64(123), "t.me/x/1"); err != nil {
		t.Fatalf("publish p1: %v", err)
	}
	// Second publish for the SAME channel-day must be rejected by the partial unique index.
	if err := ms.UpdatePostStatus(ctx, tenantID, p2.ID, store.MpPostPublishing, nil, ""); err == nil {
		t.Fatal("expected exactly-once violation publishing a second post for the same channel-day, got nil")
	}
}

func TestMeowStore_SmmOrderNoDoubleSpend(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGMeowStore(db)

	ch := newTestChannel(tenantID, "@SmmChan")
	if err := ms.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	post := &store.MpContentPost{TenantID: tenantID, ChannelID: ch.ID, ScheduledDate: time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC), Status: store.MpPostPublished}
	if err := ms.CreatePost(ctx, post); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	o1 := &store.MpSmmOrder{TenantID: tenantID, PostID: post.ID, ServiceID: "views", Qty: 100}
	if err := ms.CreateOrder(ctx, o1); err != nil {
		t.Fatalf("CreateOrder first: %v", err)
	}
	// Same (post, service) again must be rejected (no double-spend).
	o2 := &store.MpSmmOrder{TenantID: tenantID, PostID: post.ID, ServiceID: "views", Qty: 100}
	if err := ms.CreateOrder(ctx, o2); err == nil {
		t.Fatal("expected unique violation on duplicate (post,service) order, got nil")
	}
	// A different service for the same post is allowed.
	o3 := &store.MpSmmOrder{TenantID: tenantID, PostID: post.ID, ServiceID: "reactions", Qty: 5}
	if err := ms.CreateOrder(ctx, o3); err != nil {
		t.Fatalf("CreateOrder different service: %v", err)
	}
	got, err := ms.GetOrderByPostService(ctx, tenantID, post.ID, "views")
	if err != nil || got == nil {
		t.Fatalf("GetOrderByPostService: err=%v got=%v", err, got)
	}
}

func TestMeowStore_MetricUpsertDedup(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGMeowStore(db)

	ch := newTestChannel(tenantID, "@MetricChan")
	if err := ms.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	day := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)

	m1 := &store.MpChannelMetric{TenantID: tenantID, ChannelID: ch.ID, Date: day, SubscriberCount: 1000, Delta: 0}
	if err := ms.UpsertMetric(ctx, m1); err != nil {
		t.Fatalf("UpsertMetric first: %v", err)
	}
	// Same channel+date upserts in place (dedup), updating the count.
	m2 := &store.MpChannelMetric{TenantID: tenantID, ChannelID: ch.ID, Date: day, SubscriberCount: 1050, Delta: 50}
	if err := ms.UpsertMetric(ctx, m2); err != nil {
		t.Fatalf("UpsertMetric second: %v", err)
	}
	got, err := ms.GetMetric(ctx, tenantID, ch.ID, day)
	if err != nil || got == nil {
		t.Fatalf("GetMetric: err=%v got=%v", err, got)
	}
	if got.SubscriberCount != 1050 || got.Delta != 50 {
		t.Fatalf("dedup upsert did not update: %+v", got)
	}
	if got.ID != m1.ID {
		t.Fatalf("dedup created a new row (id changed): %s vs %s", got.ID, m1.ID)
	}
}

func TestMeowStore_UpsertDraftPostIdempotent(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGMeowStore(db)

	ch := newTestChannel(tenantID, "@DraftChan")
	if err := ms.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	day := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)

	p1 := &store.MpContentPost{TenantID: tenantID, ChannelID: ch.ID, ScheduledDate: day, KoText: "v1", EnText: "e1"}
	if err := ms.UpsertDraftPost(ctx, p1); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Second ingest of the same channel-day updates the SAME row, not a dup.
	p2 := &store.MpContentPost{TenantID: tenantID, ChannelID: ch.ID, ScheduledDate: day, KoText: "v2", EnText: "e2"}
	if err := ms.UpsertDraftPost(ctx, p2); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if p1.ID != p2.ID {
		t.Fatalf("idempotent upsert should reuse the row: %s vs %s", p1.ID, p2.ID)
	}
	posts, err := ms.ListPostsByChannel(ctx, tenantID, ch.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected exactly 1 draft row, got %d", len(posts))
	}
	if posts[0].KoText != "v2" || posts[0].Status != store.MpPostDraft {
		t.Fatalf("draft not updated in place: %+v", posts[0])
	}
}

func TestMeowStore_ClaimPostForPublish(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGMeowStore(db)

	ch := newTestChannel(tenantID, "@ClaimChan")
	if err := ms.UpsertChannel(ctx, ch); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	day := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	mk := func(status string) *store.MpContentPost {
		p := &store.MpContentPost{TenantID: tenantID, ChannelID: ch.ID, ScheduledDate: day, Status: status}
		if err := ms.CreatePost(ctx, p); err != nil {
			t.Fatalf("CreatePost(%s): %v", status, err)
		}
		return p
	}

	// Two approved posts for the same channel-day.
	mk(store.MpPostApproved)
	mk(store.MpPostApproved)

	// First claim flips exactly one to 'publishing'.
	claimed, err := ms.ClaimPostForPublish(ctx, tenantID, ch.ID, day, false)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if claimed.Status != store.MpPostPublishing {
		t.Fatalf("claimed post not 'publishing': %s", claimed.Status)
	}
	// Second claim finds the day already in-flight → no-op sentinel.
	if _, err := ms.ClaimPostForPublish(ctx, tenantID, ch.ID, day, false); err != store.ErrMeowNoClaimablePost {
		t.Fatalf("second claim: expected ErrMeowNoClaimablePost, got %v", err)
	}
	// After publishing, still nothing claimable (exactly-once).
	if err := ms.UpdatePostStatus(ctx, tenantID, claimed.ID, store.MpPostPublished, ptrInt64(1), "t.me/x/1"); err != nil {
		t.Fatalf("mark published: %v", err)
	}
	if _, err := ms.ClaimPostForPublish(ctx, tenantID, ch.ID, day, false); err != store.ErrMeowNoClaimablePost {
		t.Fatalf("post-publish claim: expected ErrMeowNoClaimablePost, got %v", err)
	}

	// Force claims a draft-only day; non-force does not.
	day2 := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	d := &store.MpContentPost{TenantID: tenantID, ChannelID: ch.ID, ScheduledDate: day2, Status: store.MpPostDraft}
	if err := ms.CreatePost(ctx, d); err != nil {
		t.Fatalf("CreatePost draft: %v", err)
	}
	if _, err := ms.ClaimPostForPublish(ctx, tenantID, ch.ID, day2, false); err != store.ErrMeowNoClaimablePost {
		t.Fatalf("non-force claim of draft: expected ErrMeowNoClaimablePost, got %v", err)
	}
	forced, err := ms.ClaimPostForPublish(ctx, tenantID, ch.ID, day2, true)
	if err != nil || forced == nil || forced.ID != d.ID {
		t.Fatalf("force claim of draft: err=%v post=%v", err, forced)
	}
}

func TestMeowStore_ReportIdempotency(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := pg.NewPGMeowStore(db)

	if sent, _ := ms.ReportAlreadySent(ctx, tenantID, store.MpReportDaily, "2026-06-14"); sent {
		t.Fatal("report should not be marked sent yet")
	}
	if err := ms.RecordReportSent(ctx, tenantID, store.MpReportDaily, "2026-06-14"); err != nil {
		t.Fatalf("RecordReportSent: %v", err)
	}
	// Recording the same period again is a no-op (idempotent), not an error.
	if err := ms.RecordReportSent(ctx, tenantID, store.MpReportDaily, "2026-06-14"); err != nil {
		t.Fatalf("RecordReportSent repeat should be idempotent: %v", err)
	}
	sent, err := ms.ReportAlreadySent(ctx, tenantID, store.MpReportDaily, "2026-06-14")
	if err != nil || !sent {
		t.Fatalf("ReportAlreadySent: err=%v sent=%v", err, sent)
	}
	// A different period is independent.
	if sent, _ := ms.ReportAlreadySent(ctx, tenantID, store.MpReportDaily, "2026-06-15"); sent {
		t.Fatal("different period should not be marked sent")
	}
}

func ptrInt64(v int64) *int64 { return &v }
