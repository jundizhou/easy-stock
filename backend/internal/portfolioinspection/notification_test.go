package portfolioinspection

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestPortfolioCompletionHandlerRunsOnlyForPersistedTerminalJobs(t *testing.T) {
	request, results, _, report := scoreFixture()
	encoded, _ := json.Marshal(report)
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gateway := &scoreGateway{content: string(encoded)}
	s := NewService(store, gateway, nil, nil)
	s.ConfigureResearch(func(_ context.Context, _ Holding, _ Request, _ time.Time, _ bool, _ string, _ func(HoldingResult)) (HoldingResult, error) {
		return results[0], nil
	}, nil)
	completed := make(chan Job, 2)
	s.ConfigureCompletion(func(job Job) {
		stored, err := store.Get(context.Background(), job.ID)
		if err != nil || stored.Status != job.Status || stored.CompletedAt.IsZero() {
			t.Errorf("notification before persistence: %v", err)
		}
		completed <- job
	})
	defer s.Close()
	for _, failed := range []bool{false, true} {
		gateway.fail.Store(failed)
		job, err := s.Start(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		waitPortfolio(t, s, job.ID)
		select {
		case result := <-completed:
			want := "succeeded"
			if failed {
				want = "partial"
			}
			if result.ID != job.ID || result.Status != want {
				t.Fatalf("incorrect completion status: %s", result.Status)
			}
		case <-time.After(time.Second):
			t.Fatal("no completion notification")
		}
		if len(completed) != 0 {
			t.Fatal("notification repeated for progress updates")
		}
		s.wg.Wait()
	}
}
