package privacy

import (
	"fmt"
	"math"

	"github.com/google/differential-privacy/go/v3/dpagg"

	"private-release/internal/domain"
)

func Count(records []domain.Record, epsilon domain.Decimal) (int64, error) {
	c, err := dpagg.NewCount(&dpagg.CountOptions{
		Epsilon:                  epsilon.Float64(),
		MaxPartitionsContributed: 1,
	})
	if err != nil {
		return 0, err
	}
	if err := c.IncrementBy(int64(len(records))); err != nil {
		return 0, err
	}
	return c.Result()
}

func Sum(records []domain.Record, lower, upper int64, epsilon domain.Decimal) (int64, error) {
	if lower == 0 && upper == 0 {
		return 0, nil
	}
	s, err := dpagg.NewBoundedSumInt64(&dpagg.BoundedSumInt64Options{
		Epsilon:                  epsilon.Float64(),
		MaxPartitionsContributed: 1,
		Lower:                    lower,
		Upper:                    upper,
	})
	if err != nil {
		return 0, err
	}
	for _, record := range records {
		if err := s.Add(record.Value); err != nil {
			return 0, err
		}
	}
	return s.Result()
}

func Histogram(records []domain.Record, bins []domain.Bin, epsilon domain.Decimal) ([]int64, error) {
	counts := make([]int64, len(bins))
	for _, record := range records {
		idx := domain.BinForValue(bins, record.Value)
		counts[idx]++
	}

	results := make([]int64, len(bins))
	for i, raw := range counts {
		c, err := dpagg.NewCount(&dpagg.CountOptions{
			Epsilon:                  epsilon.Float64(),
			MaxPartitionsContributed: 1,
		})
		if err != nil {
			return nil, fmt.Errorf("histogram bin %d: %w", i, err)
		}
		if err := c.IncrementBy(raw); err != nil {
			return nil, err
		}
		results[i], err = c.Result()
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func SumSensitivity(lower, upper int64) int64 {
	return max(int64Abs(lower), int64Abs(upper))
}

func int64Abs(v int64) int64 {
	if v == math.MinInt64 {
		return math.MaxInt64
	}
	if v < 0 {
		return -v
	}
	return v
}
