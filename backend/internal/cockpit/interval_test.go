package cockpit

import (
	"path/filepath"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
)

func TestPatrolIntervalsRespectSessionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		minutes, count                   int
		lastMorning, lastAfternoon, next string
	}{
		{5, 44, "11:25", "14:55", "10:15"},
		{10, 22, "11:20", "14:50", "10:20"},
		{15, 16, "11:25", "14:55", "10:25"},
		{30, 8, "11:10", "14:40", "10:40"},
		{60, 4, "10:40", "14:10", "10:40"},
	} {
		slots := intradaySlots(tc.minutes)
		if len(slots) != tc.count || clockText(slots[0]) != "09:40" || clockText(slots[len(slots)/2]) != "13:10" || clockText(slots[len(slots)/2-1]) != tc.lastMorning || clockText(slots[len(slots)-1]) != tc.lastAfternoon {
			t.Fatalf("interval %d: unexpected slots %v", tc.minutes, slots)
		}
		for _, minute := range slots {
			if minute >= 11*60+30 && minute < 13*60 || minute >= 15*60 {
				t.Fatalf("interval %d schedules outside trading hours: %s", tc.minutes, clockText(minute))
			}
		}
		for _, check := range []struct {
			now  time.Time
			want string
		}{
			{at(20, 10, 12), tc.next},
			{at(20, 11, 30), "13:10"},
			{at(20, 12, 30), "13:10"},
			{at(20, 14, 56), "15:35"},
			{at(20, 8, 0), "08:40"},
			{at(24, 10, 0), "20:00"},
		} {
			next, _ := NextRun(check.now, tc.minutes)
			if next.Format("15:04") != check.want {
				t.Fatalf("interval %d at %v: next=%v, want %s", tc.minutes, check.now, next, check.want)
			}
		}
		// Every advertised intraday time must also be eligible when the scheduler reaches it.
		for _, minute := range slots {
			now := at(20, minute/60, minute%60)
			slot, ok := DueSlot(now, tc.minutes)
			if !ok || slot.Key != clockText(minute) || !slot.RunAt.Equal(now) {
				t.Fatalf("interval %d at %v: due=%+v, ok=%v", tc.minutes, now, slot, ok)
			}
		}
	}
}

func TestIntervalChangeStartsAtNextSlotEvenAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	settings, err := appsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := OpenStore("")
	service := NewService(store, Feed{}, nil, nil, settings)
	service.now = func() time.Time { return at(20, 10, 16) }
	interval := 15
	if err := service.UpdateAutoPatrol(nil, &interval); err != nil {
		t.Fatal(err)
	}
	if got := service.Status(); got.IntervalMinutes != 15 || got.NextRunAt == nil || !got.NextRunAt.Equal(at(20, 10, 25)) {
		t.Fatalf("updated schedule = %+v", got)
	}
	service.Tick()
	if len(store.List("", 0)) != 0 {
		t.Fatal("changing the interval must not backfill a past slot")
	}

	settings, err = appsettings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service = NewService(store, Feed{}, nil, nil, settings)
	service.now = func() time.Time { return at(20, 10, 17) }
	service.Tick()
	if len(store.List("", 0)) != 0 || service.Status().IntervalMinutes != 15 {
		t.Fatal("restart lost the interval or its effective time")
	}
	service.now = func() time.Time { return at(20, 10, 25) }
	service.Tick()
	waitFor(t, func() bool { return !service.Status().Running })
	if briefs := store.List("", 0); len(briefs) != 1 || briefs[0].Key != "2026-10-20|10:25" {
		t.Fatalf("new interval did not run: %+v", briefs)
	}
	service.Tick()
	if len(store.List("", 0)) != 1 {
		t.Fatal("same slot ran twice")
	}

	service.now = func() time.Time { return at(20, 10, 26) }
	interval = 60
	if err := service.UpdateAutoPatrol(nil, &interval); err != nil {
		t.Fatal(err)
	}
	service.Tick()
	if len(store.List("", 0)) != 1 {
		t.Fatal("longer interval replayed an earlier slot")
	}
	service.now = func() time.Time { return at(20, 10, 40) }
	service.Tick()
	waitFor(t, func() bool { return !service.Status().Running })
	if briefs := store.List("", 0); len(briefs) != 2 || briefs[0].Key != "2026-10-20|10:40" {
		t.Fatalf("longer interval did not run: %+v", briefs)
	}
}
