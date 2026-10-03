package dp

import "testing"

func TestClamp(t *testing.T) {
	c := Config{Lower: -5, Upper: 10, Bins: 3}
	if got := c.Clamp(-100); got != -5 {
		t.Fatalf("clamp low: %d", got)
	}
	if got := c.Clamp(100); got != 10 {
		t.Fatalf("clamp high: %d", got)
	}
	if got := c.Clamp(3); got != 3 {
		t.Fatalf("clamp inside: %d", got)
	}
}

func TestSumSensitivity(t *testing.T) {
	c := Config{Lower: -10, Upper: 4}
	if got := c.SumSensitivity(); got != 10 {
		t.Fatalf("got %d", got)
	}
	c = Config{Lower: 0, Upper: 7}
	if got := c.SumSensitivity(); got != 7 {
		t.Fatalf("got %d", got)
	}
}

func TestBinsCoverUniquely(t *testing.T) {
	c := Config{Lower: 0, Upper: 9, Bins: 4}
	layout := c.BinLayout()
	if len(layout) != 4 {
		t.Fatalf("want 4 bins, got %d", len(layout))
	}
	for i := 1; i < len(layout); i++ {
		if layout[i].Start != layout[i-1].End {
			t.Fatalf("bins not contiguous at %d", i)
		}
	}
	// Every value in range belongs to exactly one bin.
	for v := int64(0); v <= 9; v++ {
		idx := c.BinOf(v)
		b := layout[idx]
		fv := float64(v)
		in := fv >= b.Start && (fv < b.End || (idx == int64(len(layout)-1) && fv <= b.End))
		if !in {
			t.Fatalf("value %d assigned to bin %d [%v,%v) but not inside", v, idx, b.Start, b.End)
		}
	}
}

func TestNoiseRuns(t *testing.T) {
	m := NewMechanism()
	if _, err := m.NoisyCount(10, 1.0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.NoisySum(10, Config{Lower: -5, Upper: 5}, 1.0); err != nil {
		t.Fatal(err)
	}
	out, err := m.NoisyHistogram([]int64{1, 0, 2}, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("want 3 bins, got %d", len(out))
	}
}

func TestNoiseRejectsBadEpsilon(t *testing.T) {
	m := NewMechanism()
	if _, err := m.NoisyCount(10, 0); err == nil {
		t.Fatal("expected error for epsilon=0")
	}
	if _, err := m.NoisyCount(10, -1); err == nil {
		t.Fatal("expected error for negative epsilon")
	}
}
