package decimal

import "testing"

func TestParseCanonicalKey(t *testing.T) {
	a, err := Parse("0.5")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse("0.50")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse("5e-1")
	if err != nil {
		t.Fatal(err)
	}
	if a.Key() != b.Key() || a.Key() != c.Key() {
		t.Fatalf("equal epsilons must share key: %q %q %q", a.Key(), b.Key(), c.Key())
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	for _, s := range []string{"", "0", "-1", "abc", "1/0", "NaN", "Inf", "1.2.3"} {
		if _, err := Parse(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}

func TestExactAccounting(t *testing.T) {
	var vals []Value
	for i := 0; i < 3; i++ {
		v, err := Parse("0.1")
		if err != nil {
			t.Fatal(err)
		}
		vals = append(vals, v)
	}
	spent := Sum(vals)
	limit, _ := Parse("0.3")
	if spent.Cmp(limit) != 0 {
		t.Fatalf("0.1+0.1+0.1 must equal 0.3 exactly, got %s", spent.String())
	}
}
