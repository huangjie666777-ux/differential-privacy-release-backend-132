package domain

import "testing"

func TestValidationClampsAndAssignsBinsUniquely(t *testing.T) {
	records, bins, _, err := ValidateCreate(&CreateProjectInput{
		ID: "p", Lower: 0, Upper: 10, Edges: []int64{0, 5, 10}, BudgetText: "2",
		Records: []Observation{{UserID: "a", Value: -99}, {UserID: "b", Value: 99}, {UserID: "c", Value: 5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Value != 0 || records[1].Value != 10 {
		t.Fatalf("values were not clamped: %+v", records)
	}
	if BinForValue(bins, 0) != 0 || BinForValue(bins, 4) != 0 || BinForValue(bins, 5) != 1 || BinForValue(bins, 10) != 1 {
		t.Fatalf("bin boundaries are not unique: %+v", bins)
	}
}

func TestDecimalExactAccounting(t *testing.T) {
	if got := MustParseDecimal("0.1").Add(MustParseDecimal("0.2")).String(); got != "0.3" {
		t.Fatalf("decimal sum = %q, want 0.3", got)
	}
}
