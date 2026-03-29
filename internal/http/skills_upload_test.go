package http

import (
	"context"
	"reflect"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func captureEventNames(msgBus *bus.MessageBus) *[]string {
	names := []string{}
	msgBus.Subscribe("test", func(event bus.Event) { names = append(names, event.Name) })
	return &names
}

func stubUploadDepFns(
	t *testing.T,
	installFn func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error),
	checkFn func(*skills.SkillManifest) (bool, []string),
) {
	t.Helper()
	prevInstall := installUploadedSkillDeps
	prevCheck := checkUploadedSkillDeps
	installUploadedSkillDeps = installFn
	checkUploadedSkillDeps = checkFn
	t.Cleanup(func() {
		installUploadedSkillDeps = prevInstall
		checkUploadedSkillDeps = prevCheck
	})
}

func TestReconcileUploadedSkillDeps_SkipsAutoInstallOutsideMasterTenant(t *testing.T) {
	msgBus := bus.New()
	handler := &SkillsHandler{msgBus: msgBus}
	events := captureEventNames(msgBus)
	called := false
	stubUploadDepFns(t, func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
		called = true
		return nil, nil
	}, func(*skills.SkillManifest) (bool, []string) { return false, nil })

	state := handler.reconcileUploadedSkillDeps(context.Background(), "demo", &skills.SkillManifest{}, []string{"pip:requests"}, false)
	if called {
		t.Fatal("expected auto-install to be skipped")
	}
	if got := state.status; got != "archived" {
		t.Fatalf("status = %v, want archived", got)
	}
	if !reflect.DeepEqual(state.missing, []string{"pip:requests"}) {
		t.Fatalf("missing = %#v", state.missing)
	}
	response := state.response
	state.emit(handler, "demo")
	if got := response["deps_warning"]; got != "missing dependencies: pip:requests" {
		t.Fatalf("deps_warning = %v", got)
	}
	if !reflect.DeepEqual(response["missing_deps"], []string{"pip:requests"}) {
		t.Fatalf("missing_deps = %#v", response["missing_deps"])
	}
	if !reflect.DeepEqual(*events, []string{protocol.EventSkillDepsChecked}) {
		t.Fatalf("events = %v", *events)
	}
}

func TestReconcileUploadedSkillDeps_AutoInstallSuccessClearsMissingDeps(t *testing.T) {
	msgBus := bus.New()
	handler := &SkillsHandler{msgBus: msgBus}
	events := captureEventNames(msgBus)
	stubUploadDepFns(t,
		func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
			return &skills.InstallResult{Pip: []string{"requests"}}, nil
		},
		func(*skills.SkillManifest) (bool, []string) { return true, nil },
	)

	state := handler.reconcileUploadedSkillDeps(context.Background(), "demo", &skills.SkillManifest{}, []string{"pip:requests"}, true)
	if got := state.status; got != "active" {
		t.Fatalf("status = %v, want active", got)
	}
	if len(state.missing) != 0 {
		t.Fatalf("missing = %v, want none", state.missing)
	}
	response := state.response
	state.emit(handler, "demo")
	if got := response["deps_installed"]; got != true {
		t.Fatalf("deps_installed = %v, want true", got)
	}
	wantEvents := []string{
		protocol.EventSkillDepsInstalling,
		protocol.EventSkillDepsInstalled,
		protocol.EventSkillDepsChecked,
	}
	if !reflect.DeepEqual(*events, wantEvents) {
		t.Fatalf("events = %v, want %v", *events, wantEvents)
	}
}

func TestReconcileUploadedSkillDeps_AutoInstallFailureArchivesSkill(t *testing.T) {
	msgBus := bus.New()
	handler := &SkillsHandler{msgBus: msgBus}
	events := captureEventNames(msgBus)
	stubUploadDepFns(t,
		func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
			return &skills.InstallResult{Errors: []string{"pip failed"}}, nil
		},
		func(*skills.SkillManifest) (bool, []string) { return false, []string{"pip:requests"} },
	)

	state := handler.reconcileUploadedSkillDeps(context.Background(), "demo", &skills.SkillManifest{}, []string{"pip:requests"}, true)
	if got := state.status; got != "archived" {
		t.Fatalf("status = %v, want archived", got)
	}
	if !reflect.DeepEqual(state.missing, []string{"pip:requests"}) {
		t.Fatalf("missing = %#v", state.missing)
	}
	response := state.response
	state.emit(handler, "demo")
	if got := response["deps_warning"]; got != "auto-install failed for: pip:requests" {
		t.Fatalf("deps_warning = %v", got)
	}
	if !reflect.DeepEqual(response["deps_errors"], []string{"pip failed"}) {
		t.Fatalf("deps_errors = %#v", response["deps_errors"])
	}
	wantEvents := []string{
		protocol.EventSkillDepsInstalling,
		protocol.EventSkillDepsInstalled,
		protocol.EventSkillDepsChecked,
	}
	if !reflect.DeepEqual(*events, wantEvents) {
		t.Fatalf("events = %v, want %v", *events, wantEvents)
	}
}
