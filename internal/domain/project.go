package domain

import (
	"fmt"
	"sort"
)

type Observation struct {
	UserID string `json:"user_id"`
	Value  int64  `json:"value"`
}

type CreateProjectInput struct {
	ID         string        `json:"id"`
	Lower      int64         `json:"lower"`
	Upper      int64         `json:"upper"`
	Edges      []int64       `json:"edges"`
	BudgetText string        `json:"total_epsilon"`
	Records    []Observation `json:"records"`
}

type Bin struct {
	Index          int   `json:"index"`
	Lower          int64 `json:"lower"`
	Upper          int64 `json:"upper"`
	UpperInclusive bool  `json:"upper_inclusive"`
}

type Project struct {
	ID     string
	Lower  int64
	Upper  int64
	Bins   []Bin
	Budget Decimal
	Spent  Decimal
}

type Record struct {
	UserID string
	Value  int64
}

type Operation string

const (
	OpCount     Operation = "count"
	OpSum       Operation = "sum"
	OpHistogram Operation = "histogram"
)

func ValidateCreate(in *CreateProjectInput) ([]Record, []Bin, Decimal, error) {
	if in.ID == "" {
		return nil, nil, Decimal{}, fmt.Errorf("project id is required")
	}
	if in.Lower > in.Upper {
		return nil, nil, Decimal{}, fmt.Errorf("lower must be less than or equal to upper")
	}
	if in.Lower == -9223372036854775808 || in.Upper == -9223372036854775808 {
		return nil, nil, Decimal{}, fmt.Errorf("bounds must be greater than -9223372036854775808")
	}
	bins, err := ValidateBins(in.Lower, in.Upper, in.Edges)
	if err != nil {
		return nil, nil, Decimal{}, err
	}
	budget, err := ParseDecimal(in.BudgetText)
	if err != nil {
		return nil, nil, Decimal{}, fmt.Errorf("total_epsilon: %w", err)
	}
	seen := make(map[string]struct{}, len(in.Records))
	records := make([]Record, 0, len(in.Records))
	for i, rec := range in.Records {
		if rec.UserID == "" {
			return nil, nil, Decimal{}, fmt.Errorf("records[%d].user_id is required", i)
		}
		if _, ok := seen[rec.UserID]; ok {
			return nil, nil, Decimal{}, fmt.Errorf("duplicate user_id %q", rec.UserID)
		}
		seen[rec.UserID] = struct{}{}
		records = append(records, Record{UserID: rec.UserID, Value: Clamp(rec.Value, in.Lower, in.Upper)})
	}
	return records, bins, budget, nil
}

func ValidateBins(lower, upper int64, edges []int64) ([]Bin, error) {
	if lower == upper {
		if len(edges) != 1 || edges[0] != lower {
			return nil, fmt.Errorf("degenerate bounds require a single bin edge equal to lower and upper")
		}
		return []Bin{{Index: 0, Lower: lower, Upper: upper, UpperInclusive: true}}, nil
	}
	if len(edges) < 2 {
		return nil, fmt.Errorf("at least two bin edges are required")
	}
	if edges[0] != lower || edges[len(edges)-1] != upper {
		return nil, fmt.Errorf("bin edges must start at lower and end at upper")
	}
	for i := 1; i < len(edges); i++ {
		if edges[i] <= edges[i-1] {
			return nil, fmt.Errorf("bin edges must be strictly increasing")
		}
	}
	bins := make([]Bin, 0, len(edges)-1)
	for i := 0; i < len(edges)-1; i++ {
		bins = append(bins, Bin{Index: i, Lower: edges[i], Upper: edges[i+1], UpperInclusive: i == len(edges)-2})
	}
	return bins, nil
}

func Clamp(v, lower, upper int64) int64 {
	if v < lower {
		return lower
	}
	if v > upper {
		return upper
	}
	return v
}

func ParseOperation(s string) (Operation, error) {
	switch Operation(s) {
	case OpCount, OpSum, OpHistogram:
		return Operation(s), nil
	default:
		return "", fmt.Errorf("operation must be count, sum, or histogram")
	}
}

func BinForValue(bins []Bin, value int64) int {
	return sort.Search(len(bins), func(i int) bool {
		if bins[i].UpperInclusive {
			return value <= bins[i].Upper
		}
		return value < bins[i].Upper
	})
}
