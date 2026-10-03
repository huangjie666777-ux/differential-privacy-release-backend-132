package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"private-release/internal/decimal"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mkProject(t *testing.T, s *Store, budget string) Project {
	t.Helper()
	b, err := decimal.Parse(budget)
	if err != nil {
		t.Fatal(err)
	}
	p := Project{ID: "p1", Lower: 0, Upper: 10, Bins: 2, Budget: b, BudgetKey: b.Key()}
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImmutableProject(t *testing.T) {
	s := openTest(t)
	mkProject(t, s, "1")
	b, _ := decimal.Parse("2")
	if err := s.CreateProject(Project{ID: "p1", Budget: b, BudgetKey: b.Key()}); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("want ErrProjectExists, got %v", err)
	}
}

func TestDuplicateUser(t *testing.T) {
	s := openTest(t)
	mkProject(t, s, "1")
	if err := s.AddContribution("p1", "u1", 5); err != nil {
		t.Fatal(err)
	}
	if err := s.AddContribution("p1", "u1", 6); !errors.Is(err, ErrDuplicateUser) {
		t.Fatalf("want ErrDuplicateUser, got %v", err)
	}
}

func TestPublishReuseAndBudget(t *testing.T) {
	s := openTest(t)
	mkProject(t, s, "1")
	ctx := context.Background()
	eps, _ := decimal.Parse("0.5")

	calls := 0
	compute := func() ([]byte, error) { calls++; return []byte(`{"count":42}`), nil }

	o1, err := s.Publish(ctx, "p1", "count", eps, compute)
	if err != nil {
		t.Fatal(err)
	}
	if o1.Reused || o1.Spent.Key() != eps.Key() {
		t.Fatalf("first publish: %+v", o1)
	}
	// Numerically equal epsilon in another notation reuses, no charge.
	eps2, _ := decimal.Parse("5e-1")
	o2, err := s.Publish(ctx, "p1", "count", eps2, compute)
	if err != nil {
		t.Fatal(err)
	}
	if !o2.Reused {
		t.Fatal("equal epsilon must reuse stored result")
	}
	if calls != 1 {
		t.Fatalf("compute called %d times, want 1", calls)
	}
	if o2.Spent.Key() != o1.Spent.Key() {
		t.Fatalf("reuse must not charge: %s vs %s", o2.Spent, o1.Spent)
	}

	// A different query charges; 0.5+0.6 > 1 must be rejected.
	eps3, _ := decimal.Parse("0.6")
	_, err = s.Publish(ctx, "p1", "sum", eps3, compute)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("want budget error, got %v", err)
	}
	// Failed query must not charge.
	o3, err := s.Publish(ctx, "p1", "count", eps, compute)
	if err != nil {
		t.Fatal(err)
	}
	if o3.Spent.Key() != o1.Spent.Key() {
		t.Fatalf("failed query charged budget: %s", o3.Spent)
	}
}

func TestConcurrentSameQueryPublishesOnce(t *testing.T) {
	s := openTest(t)
	mkProject(t, s, "1")
	ctx := context.Background()
	eps, _ := decimal.Parse("0.25")

	var mu sync.Mutex
	calls := 0
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Publish(ctx, "p1", "count", eps, func() ([]byte, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				return []byte(`{"count":1}`), nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("compute called %d times, want 1", calls)
	}
}

func TestPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.sqlite")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mkProject(t, s1, "1")
	eps, _ := decimal.Parse("0.5")
	if _, err := s1.Publish(context.Background(), "p1", "count", eps, func() ([]byte, error) {
		return []byte(`{"count":7}`), nil
	}); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	o, err := s2.Publish(context.Background(), "p1", "count", eps, func() ([]byte, error) {
		t.Fatal("must reuse persisted result after restart")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !o.Reused || string(o.Result) != `{"count":7}` {
		t.Fatalf("got %+v", o)
	}
	if o.Remaining.String() != "0.500000000000" {
		t.Fatalf("remaining %s", o.Remaining)
	}
}
