package monitor

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMonitorDigestsKeepRecipientChoicesAndSourceIDs(t *testing.T) {
	e, now := auditEngine(t)
	ctx := context.Background()
	seen := map[string]Event{}
	e.notify = func(_ context.Context, event Event) (string, error) {
		seen[strings.Join(NotificationChannels(event.NotifyChannels), ",")] = event
		return NotifyQueued, nil
	}
	for i, channel := range []string{"feishu", "dingtalk", "feishu"} {
		e.dispatch(ctx, Event{RuleID: channel, Severity: SeverityWarning, Message: channel + " only", NotifyChannels: []string{channel}, Price: float64(i)})
	}
	e.flushDigest(ctx, now.Add(time.Hour))
	if len(seen) != 2 || len(seen["feishu"].NotificationEventIDs) != 2 || len(seen["dingtalk"].NotificationEventIDs) != 1 {
		t.Fatalf("routing or source IDs lost: %+v", seen)
	}
	if strings.Contains(seen["feishu"].Message, "dingtalk") || strings.Contains(seen["dingtalk"].Message, "feishu") {
		t.Fatal("digest crossed recipient boundaries")
	}
}

func TestMonitorRuleCarriesNotificationChannelsIntoTrigger(t *testing.T) {
	e, now := auditEngine(t)
	rule := auditPriceRule(OpGTE)
	rule.Severity = SeverityCritical
	rule.NotifyChannels = []string{"dingtalk"}
	e.notify = func(_ context.Context, event Event) (string, error) {
		if len(event.NotifyChannels) != 1 || event.NotifyChannels[0] != "dingtalk" || event.ID == 0 {
			t.Fatalf("unpersisted or unrouted trigger: %+v", event)
		}
		return NotifySent, nil
	}
	if got := e.evaluateRule(context.Background(), rule, Observation{Price: 11}, "600000.SH", "fixture", now, "2026-10-26"); got != 1 {
		t.Fatalf("triggers=%d", got)
	}
}

func TestMonitorDisabledNotificationsDoNotAccumulateOldDigests(t *testing.T) {
	e, now := auditEngine(t)
	enabled := false
	e.canNotify = func(Event) bool { return enabled }
	calls := 0
	e.notify = func(context.Context, Event) (string, error) { calls++; return NotifyQueued, nil }
	e.dispatch(context.Background(), Event{Severity: SeverityWarning, Message: "while disabled"})
	enabled = true
	e.flushDigest(context.Background(), now.Add(time.Hour))
	if calls != 0 || len(e.pendingDigest) != 0 {
		t.Fatal("enabling notification sent an old disabled-period event")
	}
	items, _ := e.store.ListEvents(context.Background(), EventFilter{})
	if len(items) != 1 || items[0].NotifyStatus != NotifySkipped {
		t.Fatalf("disabled trigger status=%+v", items)
	}
}
