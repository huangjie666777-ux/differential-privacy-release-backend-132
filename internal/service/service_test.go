package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"private-release/internal/domain"
	"private-release/internal/store"
)

func TestConcurrentIdenticalQueryPublishedOnce(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	in := &domain.CreateProjectInput{ID: "p", Lower: 0, Upper: 5, Edges: []int64{0, 5}, BudgetText: "1", Records: []domain.Observation{{UserID: "u", Value: 1}}}
	if err := svc.CreateProject(in); err != nil {
		t.Fatal(err)
	}

	const n = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([][]byte, n)
	errs := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			result, err := svc.Query(context.Background(), "p", QueryRequest{Operation: "count", Epsilon: "0.5"})
			if err != nil {
				errs <- err
				return
			}
			results[i], _ = json.Marshal(result)
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i < n; i++ {
		if string(results[i]) != string(results[0]) {
			t.Fatalf("concurrent query was republished: %s vs %s", results[i], results[0])
		}
	}
	project, err := svc.Project("p")
	if err != nil {
		t.Fatal(err)
	}
	if project.SpentEpsilon != "0.5" {
		t.Fatalf("spent epsilon = %s, want 0.5", project.SpentEpsilon)
	}
}
