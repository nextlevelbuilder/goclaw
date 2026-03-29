package http

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type depStateWriterStub struct {
	status        string
	storedMissing []string
}

func (s *depStateWriterStub) StoreMissingDeps(_ context.Context, _ uuid.UUID, missing []string) error {
	s.storedMissing = append([]string(nil), missing...)
	return nil
}

func (s *depStateWriterStub) UpdateSkill(_ context.Context, _ uuid.UUID, updates map[string]any) error {
	if status, _ := updates["status"].(string); status != "" {
		s.status = status
	}
	return nil
}

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
	writer := &depStateWriterStub{}
	events := captureEventNames(msgBus)
	called := false
	stubUploadDepFns(t, func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
		called = true
		return nil, nil
	}, func(*skills.SkillManifest) (bool, []string) { return false, nil })

	response, err := handler.reconcileUploadedSkillDeps(context.Background(), writer, uuid.New(), "demo", &skills.SkillManifest{}, []string{"pip:requests"}, false)
	if err != nil {
		t.Fatalf("reconcileUploadedSkillDeps returned error: %v", err)
	}
	if called {
		t.Fatal("expected auto-install to be skipped")
	}
	if got := response["status"]; got != "archived" {
		t.Fatalf("status = %v, want archived", got)
	}
	if got := response["deps_warning"]; got != "missing dependencies: pip:requests" {
		t.Fatalf("deps_warning = %v", got)
	}
	if !reflect.DeepEqual(response["missing_deps"], []string{"pip:requests"}) {
		t.Fatalf("missing_deps = %#v", response["missing_deps"])
	}
	if writer.status != "archived" || !reflect.DeepEqual(writer.storedMissing, []string{"pip:requests"}) {
		t.Fatalf("writer state = status:%q missing:%v", writer.status, writer.storedMissing)
	}
	if !reflect.DeepEqual(*events, []string{protocol.EventSkillDepsChecked}) {
		t.Fatalf("events = %v", *events)
	}
}

func TestReconcileUploadedSkillDeps_AutoInstallSuccessClearsMissingDeps(t *testing.T) {
	msgBus := bus.New()
	handler := &SkillsHandler{msgBus: msgBus}
	writer := &depStateWriterStub{}
	events := captureEventNames(msgBus)
	stubUploadDepFns(t,
		func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
			return &skills.InstallResult{Pip: []string{"requests"}}, nil
		},
		func(*skills.SkillManifest) (bool, []string) { return true, nil },
	)

	response, err := handler.reconcileUploadedSkillDeps(context.Background(), writer, uuid.New(), "demo", &skills.SkillManifest{}, []string{"pip:requests"}, true)
	if err != nil {
		t.Fatalf("reconcileUploadedSkillDeps returned error: %v", err)
	}
	if got := response["status"]; got != "active" {
		t.Fatalf("status = %v, want active", got)
	}
	if got := response["deps_installed"]; got != true {
		t.Fatalf("deps_installed = %v, want true", got)
	}
	if writer.status != "active" || len(writer.storedMissing) != 0 {
		t.Fatalf("writer state = status:%q missing:%v", writer.status, writer.storedMissing)
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
	writer := &depStateWriterStub{}
	events := captureEventNames(msgBus)
	stubUploadDepFns(t,
		func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
			return &skills.InstallResult{Errors: []string{"pip failed"}}, nil
		},
		func(*skills.SkillManifest) (bool, []string) { return false, []string{"pip:requests"} },
	)

	response, err := handler.reconcileUploadedSkillDeps(context.Background(), writer, uuid.New(), "demo", &skills.SkillManifest{}, []string{"pip:requests"}, true)
	if err != nil {
		t.Fatalf("reconcileUploadedSkillDeps returned error: %v", err)
	}
	if got := response["deps_warning"]; got != "auto-install failed for: pip:requests" {
		t.Fatalf("deps_warning = %v", got)
	}
	if !reflect.DeepEqual(response["deps_errors"], []string{"pip failed"}) {
		t.Fatalf("deps_errors = %#v", response["deps_errors"])
	}
	if writer.status != "archived" || !reflect.DeepEqual(writer.storedMissing, []string{"pip:requests"}) {
		t.Fatalf("writer state = status:%q missing:%v", writer.status, writer.storedMissing)
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
