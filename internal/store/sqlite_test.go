package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"private-release/internal/domain"
)

func TestProjectAndAtomicBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	in := &domain.CreateProjectInput{
		ID: "p1", Lower: 0, Upper: 10, Edges: []int64{0, 5, 10}, BudgetText: "1.00",
		Records: []domain.Observation{{UserID: "a", Value: 4}, {UserID: "b", Value: 10}},
	}
	if err := db.CreateProject(in); err != nil {
		t.Fatal(err)
	}

	calls := 0
	mech := func(records []domain.Record, _ LoadedProject, _ domain.Decimal) (any, error) {
		calls++
		return map[string]int{"records_seen": len(records)}, nil
	}
	first, err := db.Publish(context.Background(), "p1", domain.OpCount, domain.MustParseDecimal("0.50"), mech)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.Publish(context.Background(), "p1", domain.OpCount, domain.MustParseDecimal("0.50"), mech)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("mechanism calls = %d, want 1", calls)
	}
	firstJSON, _ := json.Marshal(first.Result)
	secondJSON, _ := json.Marshal(second.Result)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("cached result changed: %s vs %s", firstJSON, secondJSON)
	}
	if first.Spent != "0.5" || first.Remaining != "0.5" || second.Spent != "0.5" || second.Remaining != "0.5" {
		t.Fatalf("unexpected accounting: first=%+v second=%+v", first, second)
	}
	if _, err := db.Publish(context.Background(), "p1", domain.OpSum, domain.MustParseDecimal("0.51"), mech); err == nil {
		t.Fatal("expected budget rejection")
	}
	if _, spent, err := db.Project("p1"); err != nil || spent.String() != "0.5" {
		t.Fatalf("failed request changed spent to %s: %v", spent, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	cached, err := reopened.Publish(context.Background(), "p1", domain.OpCount, domain.MustParseDecimal("0.50"), mech)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || cached.Remaining != "0.5" {
		t.Fatalf("restart did not preserve cache/accounting: calls=%d result=%+v", calls, cached)
	}
}

func TestRejectsDuplicateAndInvalidRecords(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.CreateProject(&domain.CreateProjectInput{
		ID: "p", Lower: 0, Upper: 1, Edges: []int64{0, 1}, BudgetText: "1",
		Records: []domain.Observation{{UserID: "a", Value: 0}, {UserID: "a", Value: 1}},
	})
	if err == nil {
		t.Fatal("duplicate user was accepted")
	}
}
