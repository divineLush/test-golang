package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestPerSecondBucketPlacement(t *testing.T) {
	var ps perSecond
	base := time.Unix(100, 0)

	for i := 0; i < 3; i++ {
		ps.add(base)
	}
	for i := 0; i < 2; i++ {
		ps.add(base.Add(time.Second))
	}

	snap := ps.snapshot(base.Add(2 * time.Second))

	if snap[57] != 3 {
		t.Errorf("sec 100: got %d, want 3", snap[57])
	}
	if snap[58] != 2 {
		t.Errorf("sec 101: got %d, want 2", snap[58])
	}
	if snap[59] != 0 {
		t.Errorf("sec 102: got %d, want 0", snap[59])
	}
}

func TestPerSecondRolloverResets(t *testing.T) {
	var ps perSecond
	ps.add(time.Unix(100, 0))
	snap := ps.snapshot(time.Unix(100, 0))
	if snap[59] != 1 {
		t.Fatal("initial add failed")
	}

	ps.add(time.Unix(160, 0))
	snap = ps.snapshot(time.Unix(160, 0))
	if snap[59] != 1 {
		t.Errorf("after rollover count: got %d, want 1", snap[59])
	}
}

func TestPerSecondEmpty(t *testing.T) {
	var ps perSecond
	snap := ps.snapshot(time.Now())
	for _, v := range snap {
		if v != 0 {
			t.Errorf("expected all zeros, got %d", v)
		}
	}
}

func TestPerSecondOldSlotsShowZero(t *testing.T) {
	var ps perSecond
	ps.add(time.Unix(100, 0))
	snap := ps.snapshot(time.Unix(160, 0))
	if snap[0] != 0 {
		t.Errorf("sec 101 (60s ago): got %d, want 0", snap[0])
	}
}

func TestLatRingPercentile(t *testing.T) {
	var r latRing
	now := time.Now().UnixNano()
	for i := 0; i < 100; i++ {
		r.add(int64(1000+i), now)
	}

	p95 := r.percentile(now, 0.95)
	p99 := r.percentile(now, 0.99)

	if p95 != 1094 {
		t.Errorf("p95 = %d, want 1094", p95)
	}
	if p99 != 1098 {
		t.Errorf("p99 = %d, want 1098", p99)
	}
}

func TestLatRingWindowFilter(t *testing.T) {
	var r latRing
	now := time.Now().UnixNano()
	r.add(2000, now-int64(61*time.Second))
	r.add(500, now)

	p := r.percentile(now, 0.5)
	if p != 500 {
		t.Errorf("p50 = %d, want 500 (old sample should be excluded)", p)
	}
}

func TestLatRingEmpty(t *testing.T) {
	var r latRing
	if p := r.percentile(time.Now().UnixNano(), 0.95); p != 0 {
		t.Errorf("empty ring p95 = %d, want 0", p)
	}
}

func TestLatRingSumAndCount(t *testing.T) {
	var r latRing
	r.add(100, 0)
	r.add(150, 0)
	if r.sum.Load() != 250 {
		t.Errorf("sum = %d, want 250", r.sum.Load())
	}
	if r.count.Load() != 2 {
		t.Errorf("count = %d, want 2", r.count.Load())
	}
}

func TestWriteContainsAllMetricNames(t *testing.T) {
	var m Metrics
	m.Record(time.Millisecond, 2*time.Millisecond, time.Now())

	var b strings.Builder
	m.Write(&b)
	out := b.String()

	for _, want := range []string{
		"calculator_requests_total 1",
		"# TYPE calculator_requests_per_second gauge",
		"calculator_c_duration_seconds_count 1",
		"calculator_rust_duration_seconds_count 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}
