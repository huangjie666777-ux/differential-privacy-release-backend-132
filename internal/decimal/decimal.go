package decimal

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Value is an exact, positive decimal number.
type Value struct {
	r *big.Rat
}

// Parse parses a decimal string (e.g. "1", "0.5", "1e-3") into an exact
// positive Value. Non-positive, non-finite or malformed inputs are rejected.
func Parse(s string) (Value, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Value{}, errors.New("empty number")
	}
	if strings.ContainsAny(s, "/") {
		return Value{}, fmt.Errorf("invalid number %q", s)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return Value{}, fmt.Errorf("invalid number %q", s)
	}
	if r.Sign() <= 0 {
		return Value{}, fmt.Errorf("number must be positive, got %q", s)
	}
	return Value{r: r}, nil
}

// Rat exposes the exact rational value.
func (v Value) Rat() *big.Rat { return new(big.Rat).Set(v.r) }

// FromKey reconstructs a Value previously serialized with Key.
func FromKey(key string) (Value, error) {
	r, ok := new(big.Rat).SetString(key)
	if !ok {
		return Value{}, fmt.Errorf("invalid key %q", key)
	}
	return Value{r: r}, nil
}

// Key returns a canonical string such that two numerically equal values
// always share the same key.
func (v Value) Key() string { return v.r.RatString() }

// String returns a plain decimal rendering for display.
func (v Value) String() string { return v.r.FloatString(12) }

// Float64 converts to float64 for the noise mechanism.
func (v Value) Float64() float64 {
	f, _ := v.r.Float64()
	return f
}

// Add returns the exact sum of two values.
func Add(a, b Value) Value {
	return Value{r: new(big.Rat).Add(a.r, b.r)}
}

// Sum adds up a slice of values.
func Sum(vs []Value) Value {
	acc := new(big.Rat)
	for _, v := range vs {
		acc.Add(acc, v.r)
	}
	return Value{r: acc}
}

// Zero is the additive identity.
func Zero() Value { return Value{r: new(big.Rat)} }

// Cmp compares two values: -1, 0 or +1.
func (v Value) Cmp(o Value) int { return v.r.Cmp(o.r) }

// Sub returns a-b.
func Sub(a, b Value) Value {
	return Value{r: new(big.Rat).Sub(a.r, b.r)}
}
