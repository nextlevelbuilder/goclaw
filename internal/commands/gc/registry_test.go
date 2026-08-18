package gc

import (
	"reflect"
	"testing"
)

func TestRegistry_RegisterLookup(t *testing.T) {
	r := NewRegistry()
	r.Register(KindPlan, "plan")
	r.Register(KindFix, "fix")

	slug, ok := r.Lookup(KindPlan)
	if !ok || slug != "plan" {
		t.Errorf("Lookup(plan) = %q, %v; want plan, true", slug, ok)
	}
	slug, ok = r.Lookup(KindFix)
	if !ok || slug != "fix" {
		t.Errorf("Lookup(fix) = %q, %v; want fix, true", slug, ok)
	}
}

func TestRegistry_LookupUnknown(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Lookup(KindReview); ok {
		t.Error("Lookup(review) on empty registry should be false")
	}
	r.Register(KindCook, "cook")
	if _, ok := r.Lookup(KindReview); ok {
		t.Error("Lookup(review) for unregistered kind should be false")
	}
}

func TestRegistry_KnownKinds(t *testing.T) {
	r := NewRegistry()
	if got := r.KnownKinds(); len(got) != 0 {
		t.Errorf("empty registry KnownKinds = %v, want empty", got)
	}

	r.Register(KindPlan, "plan")
	r.Register(KindReview, "review")
	r.Register(KindCook, "cook")
	r.Register(KindFix, "fix")

	got := r.KnownKinds()
	want := []CommandKind{KindPlan, KindFix, KindCook, KindReview}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("KnownKinds = %v, want %v", got, want)
	}
}

func TestRegistry_RegisterIgnoresEmpty(t *testing.T) {
	r := NewRegistry()
	r.Register(KindPlan, "")
	r.Register("", "plan")
	if _, ok := r.Lookup(KindPlan); ok {
		t.Error("registering empty slug must not create a mapping")
	}
}

func TestRegistry_RegisterReplaces(t *testing.T) {
	r := NewRegistry()
	r.Register(KindPlan, "plan")
	r.Register(KindPlan, "deep-plan")
	slug, ok := r.Lookup(KindPlan)
	if !ok || slug != "deep-plan" {
		t.Errorf("Lookup(plan) = %q, %v; want deep-plan, true", slug, ok)
	}
}