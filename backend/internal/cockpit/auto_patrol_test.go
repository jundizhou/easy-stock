package cockpit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
)

func TestAutoPatrolDisabledSkipsEveryScheduledPhaseButAllowsManual(t *testing.T) {
	for _, now := range []time.Time{at(20, 8, 45), at(20, 10, 15), at(20, 15, 40), at(24, 20, 30)} {
		t.Run(now.Format("02-15:04"), func(t *testing.T) {
			service, store := newTestService(t, nil, now)
			if !service.Status().AutoPatrolEnabled {
				t.Fatal("automatic patrol should default to enabled")
			}
			if err := service.SetAutoPatrolEnabled(false); err != nil {
				t.Fatal(err)
			}
			service.Tick()
			status := service.Status()
			if status.AutoPatrolEnabled || status.NextRunAt != nil || status.NextRunName != "" || len(store.List("", 0)) != 0 {
				t.Fatalf("disabled patrol still schedules work: %+v", status)
			}
			// Also cover a tick that already selected its slot before the switch changed.
			slot, _ := DueSlot(now, 30)
			if _, err := service.launch(slot, "scheduled", now); !errors.Is(err, errAutoPatrolDisabled) {
				t.Fatalf("late scheduled launch = %v", err)
			}
			brief, err := service.RunManual(slot.Phase)
			if err != nil || brief.Trigger != "manual" {
				t.Fatalf("manual patrol while disabled: %+v, %v", brief, err)
			}
			waitFor(t, func() bool { return !service.Status().Running })
			service.now = func() time.Time { return now.Add(time.Second) }
			if err := service.SetAutoPatrolEnabled(true); err != nil {
				t.Fatal(err)
			}
			if service.Status().NextRunAt == nil {
				t.Fatal("re-enabled patrol must show its next schedule")
			}
			service.Tick()
			waitFor(t, func() bool { return !service.Status().Running })
			briefs := store.List("", 0)
			if len(briefs) != 2 || briefs[0].Trigger != "scheduled" {
				t.Fatalf("re-enabled patrol did not run: %+v", briefs)
			}
			service.Tick()
			if len(store.List("", 0)) != 2 {
				t.Fatal("re-enabling must preserve schedule deduplication")
			}
		})
	}
}

func TestDisablingAutoPatrolLetsCurrentRunFinish(t *testing.T) {
	narrator := &fakeNarrator{available: true, release: make(chan struct{}), text: "巡查完成"}
	service, store := newTestService(t, narrator, at(20, 10, 15))
	service.Tick()
	if err := service.SetAutoPatrolEnabled(false); err != nil {
		t.Fatal(err)
	}
	if status := service.Status(); !status.Running || status.AutoPatrolEnabled {
		t.Fatalf("current run should finish after disabling: %+v", status)
	}
	close(narrator.release)
	waitFor(t, func() bool { return !service.Status().Running })
	if brief := store.List("", 0)[0]; brief.Narrative != "巡查完成" || brief.Status != "ready" {
		t.Fatalf("current patrol was interrupted: %+v", brief)
	}
	service.now = func() time.Time { return at(20, 10, 45) }
	service.Tick()
	if len(store.List("", 0)) != 1 {
		t.Fatal("the next scheduled patrol must remain disabled")
	}
}

func TestAutoPatrolSaveFailureKeepsPreviousState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	settings, err := appsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	store, _ := OpenStore("")
	service := NewService(store, Feed{}, nil, nil, settings)
	if err := service.SetAutoPatrolEnabled(false); err == nil {
		t.Fatal("expected a persistence error")
	}
	if !service.Status().AutoPatrolEnabled {
		t.Fatal("failed save must not change the effective setting")
	}
}
