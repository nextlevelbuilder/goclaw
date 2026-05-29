package store

import (
	"context"
	"testing"
)

func TestIsSkillVisibleTo(t *testing.T) {
	alice := "alice"
	bob := "bob"
	ctx := WithUserID(context.Background(), alice)

	tests := []struct {
		name       string
		owner      string
		visibility string
		isSystem   bool
		want       bool
	}{
		{"system skill visible to anyone", "system", "private", true, true},
		{"public visible to non-owner", bob, "public", false, true},
		{"empty visibility treated as public", bob, "", false, true},
		{"private visible to owner", alice, "private", false, true},
		{"private hidden from non-owner", bob, "private", false, false},
		{"private with no owner treated as public", "", "private", false, true},
		{"internal requires explicit grant outside this helper", bob, "internal", false, false},
		{"unknown enum fails closed", bob, "team", false, false},
		{"uppercase private matched for owner", alice, "PRIVATE", false, true},
		{"whitespace public treated as public", bob, "  public  ", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSkillVisibleTo(ctx, tt.owner, tt.visibility, tt.isSystem)
			if got != tt.want {
				t.Fatalf("IsSkillVisibleTo(owner=%q, vis=%q, sys=%v) = %v, want %v",
					tt.owner, tt.visibility, tt.isSystem, got, tt.want)
			}
		})
	}
}

func TestFilterVisibleSkills(t *testing.T) {
	ctx := WithUserID(context.Background(), "alice")
	skills := []SkillInfo{
		{Slug: "sys", IsSystem: true, Visibility: "public"},
		{Slug: "mine-private", OwnerID: "alice", Visibility: "private"},
		{Slug: "theirs-private", OwnerID: "bob", Visibility: "private"},
		{Slug: "theirs-public", OwnerID: "bob", Visibility: "public"},
	}
	got := FilterVisibleSkills(ctx, skills)
	gotSlugs := map[string]bool{}
	for _, s := range got {
		gotSlugs[s.Slug] = true
	}
	for _, want := range []string{"sys", "mine-private", "theirs-public"} {
		if !gotSlugs[want] {
			t.Errorf("expected %q in filtered output, got %v", want, gotSlugs)
		}
	}
	if gotSlugs["theirs-private"] {
		t.Errorf("leaked private skill to non-owner: %v", gotSlugs)
	}
}
