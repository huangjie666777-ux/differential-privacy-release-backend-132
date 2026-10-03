package domain

import (
	"fmt"
	"math"
	"math/big"
	"strings"
)

type Decimal struct {
	value *big.Rat
	text  string
}

func ParseDecimal(s string) (Decimal, error) {
	return parse(s, false)
}

func ParseNonNegativeDecimal(s string) (Decimal, error) {
	return parse(s, true)
}

func parse(s string, allowZero bool) (Decimal, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Decimal{}, fmt.Errorf("decimal is required")
	}
	if !isCanonicalDecimal(s) {
		return Decimal{}, fmt.Errorf("decimal %q must use an optional sign and digits with an optional decimal point", s)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return Decimal{}, fmt.Errorf("invalid decimal %q", s)
	}
	if r.Sign() < 0 || (!allowZero && r.Sign() == 0) {
		return Decimal{}, fmt.Errorf("decimal %q must be positive", s)
	}
	if f, _ := r.Float64(); math.IsNaN(f) || math.IsInf(f, 0) || (!allowZero && f <= 0) {
		return Decimal{}, fmt.Errorf("decimal %q is outside the positive range supported by the DP library", s)
	}
	return Decimal{value: r, text: ratToDecimal(r)}, nil
}

func MustParseDecimal(s string) Decimal {
	d, err := ParseDecimal(s)
	if err != nil {
		panic(err)
	}
	return d
}

func (d Decimal) String() string {
	if d.value == nil {
		return ""
	}
	return d.text
}

func (d Decimal) Float64() float64 {
	f, _ := d.value.Float64()
	return f
}

func (d Decimal) Add(other Decimal) Decimal {
	r := new(big.Rat).Add(d.value, other.value)
	return Decimal{value: r, text: ratToDecimal(r)}
}

func (d Decimal) Sub(other Decimal) Decimal {
	r := new(big.Rat).Sub(d.value, other.value)
	return Decimal{value: r, text: ratToDecimal(r)}
}

func (d Decimal) Cmp(other Decimal) int {
	return d.value.Cmp(other.value)
}

func isCanonicalDecimal(s string) bool {
	i := 0
	if s[0] == '+' || s[0] == '-' {
		i++
	}
	if i == len(s) {
		return false
	}
	hasDigit, hasDot := false, false
	for ; i < len(s); i++ {
		if s[i] == '.' {
			if hasDot {
				return false
			}
			hasDot = true
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		hasDigit = true
	}
	return hasDigit && (!hasDot || len(s) > 1)
}

func ratToDecimal(r *big.Rat) string {
	origNum := r.Num()
	negative := origNum.Sign() < 0
	num := new(big.Int).Abs(origNum)
	den := new(big.Int).Set(r.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	twos, fives := 0, 0
	mod := new(big.Int)
	for mod.Mod(den, two).Sign() == 0 {
		den.Quo(den, two)
		twos++
	}
	for mod.Mod(den, five).Sign() == 0 {
		den.Quo(den, five)
		fives++
	}
	if den.Cmp(big.NewInt(1)) != 0 {
		return ""
	}
	scale := twos
	if fives > scale {
		scale = fives
	}
	factor := new(big.Int)
	if scale > twos {
		factor.Exp(two, big.NewInt(int64(scale-twos)), nil)
	} else if scale > fives {
		factor.Exp(five, big.NewInt(int64(scale-fives)), nil)
	} else {
		factor.SetInt64(1)
	}
	scaled := new(big.Int).Mul(num, factor)
	digits := scaled.String()
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale+1-len(digits)) + digits
	}
	whole, fraction := digits[:len(digits)-scale], digits[len(digits)-scale:]
	fraction = strings.TrimRight(fraction, "0")
	if fraction == "" {
		return prefix(negative) + whole
	}
	return prefix(negative) + whole + "." + fraction
}

func prefix(negative bool) string {
	if negative {
		return "-"
	}
	return ""
}
