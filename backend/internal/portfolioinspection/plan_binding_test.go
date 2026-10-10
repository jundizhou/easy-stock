package portfolioinspection

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestPlanHistoryFiltersBeforeLimit(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	for i := 0; i < 35; i++ {
		planID := "other"
		if i == 0 {
			planID = "original"
		}
		if i == 1 {
			planID = ""
		}
		_, err := store.Save(ctx, Job{ID: fmt.Sprint(i), Status: "succeeded", Request: Request{PortfolioPlanID: planID}, UpdatedAt: time.Unix(int64(i+1000), 0)})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		filter string
		want   string
	}{{"original", "0"}, {"", "1"}, {"other", "34"}} {
		jobs, err := store.List(ctx, 1, test.filter)
		if err != nil || len(jobs) != 1 || jobs[0].ID != test.want {
			t.Fatalf("filter %q: %+v %v", test.filter, jobs, err)
		}
	}
	jobs, err := store.List(ctx, 12)
	if err != nil || len(jobs) != 12 {
		t.Fatalf("all: %d %v", len(jobs), err)
	}
	jobs, err = store.List(ctx, 12, "deleted")
	if err != nil || len(jobs) != 0 {
		t.Fatalf("unknown plan: %+v %v", jobs, err)
	}
}

func TestBindLegacyPlanPreservesResearchAndCannotReassign(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	req, results, _, conclusion := scoreFixture()
	original, err := store.Save(ctx, Job{ID: "legacy", Status: "partial", Request: req, Results: results, Report: &Report{Request: req, Conclusion: conclusion}})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{store: store}
	bound, err := svc.BindPlan(ctx, original.ID, "plan-a", "长线组合")
	if err != nil {
		t.Fatal(err)
	}
	if bound.Request.PortfolioPlanID != "plan-a" || bound.Report.Request.PortfolioPlanID != "plan-a" || bound.Request.PortfolioPlanName != "长线组合" {
		t.Fatalf("lost binding: %+v", bound)
	}
	if !reflect.DeepEqual(original.Results, bound.Results) || !reflect.DeepEqual(original.Report.Conclusion, bound.Report.Conclusion) || !reflect.DeepEqual(original.Request.Holdings, bound.Request.Holdings) {
		t.Fatal("binding changed historical analysis")
	}
	if _, err := svc.BindPlan(ctx, original.ID, "plan-b", "另一方案"); err == nil {
		t.Fatal("reassigned a bound report")
	}
	if _, err := svc.BindPlan(ctx, original.ID, "plan-a", "重命名"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(ctx, Job{ID: "running", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BindPlan(ctx, "running", "plan-a", "长线组合"); err == nil {
		t.Fatal("bound a running report")
	}
	stored, err := store.Get(ctx, original.ID)
	if err != nil || stored.Request.PortfolioPlanID != "plan-a" {
		t.Fatalf("not persisted: %+v %v", stored, err)
	}
}
