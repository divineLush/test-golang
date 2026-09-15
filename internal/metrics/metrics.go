// Package metrics collects and renders Prometheus-format metrics for the
// calculator server.
package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	perSecondSlots = 60
	latRingSize    = 1 << 16
)

// perSecond buckets request counts by unix second, keeping the last 60.
type perSecond struct {
	counts [perSecondSlots]atomic.Int64
	last   [perSecondSlots]atomic.Int64
}

func (p *perSecond) add(now time.Time) {
	s := now.Unix()
	idx := int(s % perSecondSlots)
	count := &p.counts[idx]
	last := &p.last[idx]
	for {
		l := last.Load()
		if l == s {
			count.Add(1)
			return
		}
		if last.CompareAndSwap(l, s) {
			count.Store(0)
			count.Add(1)
			return
		}
	}
}

// snapshot returns counts for the last 60 seconds, oldest first.
func (p *perSecond) snapshot(now time.Time) [perSecondSlots]int64 {
	s := now.Unix()
	var out [perSecondSlots]int64
	for i := 0; i < perSecondSlots; i++ {
		sec := s - int64(perSecondSlots-1-i)
		idx := int(sec % perSecondSlots)
		if p.last[idx].Load() == sec {
			out[i] = p.counts[idx].Load()
		}
	}
	return out
}

// latRing buffers per-call latency samples plus cumulative sum/count.
type latRing struct {
	next  atomic.Uint64
	ts    [latRingSize]atomic.Int64
	dur   [latRingSize]atomic.Int64
	sum   atomic.Int64
	count atomic.Uint64
}

func (r *latRing) add(durNanos, whenNanos int64) {
	i := (r.next.Add(1) - 1) % latRingSize
	r.dur[i].Store(durNanos)
	r.ts[i].Store(whenNanos)
	r.sum.Add(durNanos)
	r.count.Add(1)
}

// percentile returns the q-th quantile of samples within the last 60 seconds.
func (r *latRing) percentile(nowNanos int64, q float64) int64 {
	cutoff := nowNanos - int64(60*time.Second)
	samples := make([]int64, 0, 1024)
	for i := 0; i < latRingSize; i++ {
		t := r.ts[i].Load()
		d := r.dur[i].Load()
		if d > 0 && t >= cutoff {
			samples = append(samples, d)
		}
	}
	if len(samples) == 0 {
		return 0
	}
	sort.Slice(samples, func(a, b int) bool { return samples[a] < samples[b] })
	return samples[int(float64(len(samples)-1)*q)]
}

// Metrics tracks request-rate and native call-latency statistics.
type Metrics struct {
	ps    perSecond
	total atomic.Int64
	c     latRing
	rust  latRing
}

// Record registers one successful request and its C and Rust call latencies.
func (m *Metrics) Record(dC, dRust time.Duration, when time.Time) {
	m.ps.add(when)
	m.c.add(int64(dC), when.UnixNano())
	m.rust.add(int64(dRust), when.UnixNano())
	m.total.Add(1)
}

func formatSeconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'g', -1, 64)
}

func (m *Metrics) writeSummary(b *strings.Builder, name string, ring *latRing) {
	now := time.Now()
	p95 := ring.percentile(now.UnixNano(), 0.95)
	p99 := ring.percentile(now.UnixNano(), 0.99)
	b.WriteString("# HELP " + name + " Call latency of the native function, seconds.\n")
	b.WriteString("# TYPE " + name + " summary\n")
	fmt.Fprintf(b, "%s{quantile=\"0.95\"} %s\n", name, formatSeconds(time.Duration(p95)))
	fmt.Fprintf(b, "%s{quantile=\"0.99\"} %s\n", name, formatSeconds(time.Duration(p99)))
	fmt.Fprintf(b, "%s_sum %s\n", name, formatSeconds(time.Duration(ring.sum.Load())))
	fmt.Fprintf(b, "%s_count %d\n", name, ring.count.Load())
}

// Write renders the metrics in Prometheus text format.
func (m *Metrics) Write(b *strings.Builder) {
	now := time.Now()
	b.WriteString("# HELP calculator_requests_total Successful /calc requests handled by this process.\n")
	b.WriteString("# TYPE calculator_requests_total counter\n")
	fmt.Fprintf(b, "calculator_requests_total %d\n", m.total.Load())

	b.WriteString("# HELP calculator_requests_per_second Successful /calc requests in a given unix second.\n")
	b.WriteString("# TYPE calculator_requests_per_second gauge\n")
	for i, v := range m.ps.snapshot(now) {
		sec := now.Unix() - int64(perSecondSlots-1-i)
		fmt.Fprintf(b, "calculator_requests_per_second{sec=\"%d\"} %d\n", sec, v)
	}

	m.writeSummary(b, "calculator_c_duration_seconds", &m.c)
	m.writeSummary(b, "calculator_rust_duration_seconds", &m.rust)
}
