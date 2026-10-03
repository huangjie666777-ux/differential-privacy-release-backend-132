// Package dp implements the differential privacy mechanisms: clamping,
// binning and pure-epsilon Laplace noising using Google's DP library.
package dp

import (
	"errors"
	"fmt"

	"github.com/google/differential-privacy/go/v3/noise"
)

// Config describes an immutable project's public parameters.
type Config struct {
	Lower int64
	Upper int64
	Bins  int64
}

// Validate checks the public parameter constraints.
func (c Config) Validate() error {
	if c.Lower > c.Upper {
		return fmt.Errorf("lower bound %d exceeds upper bound %d", c.Lower, c.Upper)
	}
	if c.Bins < 1 {
		return errors.New("bins must be at least 1")
	}
	return nil
}

// Clamp clips a value into [Lower, Upper].
func (c Config) Clamp(v int64) int64 {
	if v < c.Lower {
		return c.Lower
	}
	if v > c.Upper {
		return c.Upper
	}
	return v
}

// SumSensitivity is the L1 sensitivity of the bounded sum under
// add/remove-one-user adjacency: max(|lower|, |upper|).
func (c Config) SumSensitivity() int64 {
	l, u := c.Lower, c.Upper
	if l < 0 {
		l = -l
	}
	if u < 0 {
		u = -u
	}
	if l > u {
		return l
	}
	return u
}

// Bin describes one histogram bucket. Bins are contiguous, cover
// [Lower, Upper] and each value belongs to exactly one bin.
type Bin struct {
	Index int64   `json:"index"`
	Start float64 `json:"start"` // inclusive
	End   float64 `json:"end"`   // exclusive, except the last bin which is inclusive
}

// Bins computes the fixed equal-width bin layout.
func (c Config) BinLayout() []Bin {
	n := c.Bins
	lo, hi := float64(c.Lower), float64(c.Upper)
	width := (hi - lo + 1) / float64(n)
	out := make([]Bin, n)
	for i := int64(0); i < n; i++ {
		out[i] = Bin{Index: i, Start: lo + float64(i)*width, End: lo + float64(i+1)*width}
	}
	return out
}

// BinOf returns the index of the bin owning value v (already clamped).
func (c Config) BinOf(v int64) int64 {
	n := c.Bins
	lo, hi := float64(c.Lower), float64(c.Upper)
	width := (hi - lo + 1) / float64(n)
	idx := int64((float64(v) - lo) / width)
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return idx
}

// Mechanism wraps the secure Laplace noise from the official DP library.
type Mechanism struct {
	noise noise.Noise
}

// NewMechanism returns a pure-epsilon Laplace mechanism.
func NewMechanism() *Mechanism {
	return &Mechanism{noise: noise.Laplace()}
}

// NoisyCount releases a DP count. Sensitivity: L0=1, Linf=1.
func (m *Mechanism) NoisyCount(count int64, epsilon float64) (int64, error) {
	return m.noise.AddNoiseInt64(count, 1, 1, epsilon, 0)
}

// NoisySum releases a DP sum. Linf sensitivity is max(|lower|,|upper|).
func (m *Mechanism) NoisySum(sum int64, cfg Config, epsilon float64) (int64, error) {
	return m.noise.AddNoiseInt64(sum, 1, cfg.SumSensitivity(), epsilon, 0)
}

// NoisyHistogram releases a DP histogram. The whole histogram has L1
// sensitivity 1 under add/remove-one-user adjacency, so every bin count
// gets independent Laplace noise with L0=1, Linf=1. All bins are returned,
// including empty ones.
func (m *Mechanism) NoisyHistogram(counts []int64, epsilon float64) ([]int64, error) {
	out := make([]int64, len(counts))
	for i, c := range counts {
		n, err := m.noise.AddNoiseInt64(c, 1, 1, epsilon, 0)
		if err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, nil
}
