package server

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"calculator/internal/native"
)

var (
	errMissingNum = errors.New("missing num")
	errBadNum     = errors.New("bad num")

	bodyOK             = []byte("ok")
	bodyNotImplemented = []byte("not implemented")
	bodyMissingNum     = []byte("missing 'num' query parameter")
	bodyBadNum         = []byte("'num' must be an integer")
)

const (
	metricSeconds = 60
	latRingSize   = 1 << 16
)

func parseNum(rawQuery string) (int64, error) {
	idx := strings.Index(rawQuery, "num=")
	if idx < 0 {
		return 0, errMissingNum
	}
	start := idx + 4
	end := start
	for end < len(rawQuery) && rawQuery[end] != '&' {
		end++
	}
	if start == end {
		return 0, errBadNum
	}
	return strconv.ParseInt(rawQuery[start:end], 10, 64)
}

// perSecond buckets request counts by unix second, keeping the last 60.
type perSecond struct {
	counts [metricSeconds]atomic.Int64
	last   [metricSeconds]atomic.Int64
}

func (p *perSecond) add(now time.Time) {
	s := now.Unix()
	idx := int(s % metricSeconds)
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
func (p *perSecond) snapshot(now time.Time) [metricSeconds]int64 {
	s := now.Unix()
	var out [metricSeconds]int64
	for i := 0; i < metricSeconds; i++ {
		sec := s - int64(metricSeconds-1-i)
		idx := int(sec % metricSeconds)
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

type metrics struct {
	ps    perSecond
	total atomic.Int64
	c     latRing
	rust  latRing
}

func (m *metrics) record(dC, dRust time.Duration, when time.Time) {
	m.ps.add(when)
	m.c.add(int64(dC), when.UnixNano())
	m.rust.add(int64(dRust), when.UnixNano())
	m.total.Add(1)
}

func formatSeconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'g', -1, 64)
}

func (m *metrics) writeSummary(b *strings.Builder, name string, ring *latRing) {
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

func (m *metrics) write(b *strings.Builder) {
	now := time.Now()
	b.WriteString("# HELP calculator_requests_total Successful /calc requests handled by this process.\n")
	b.WriteString("# TYPE calculator_requests_total counter\n")
	fmt.Fprintf(b, "calculator_requests_total %d\n", m.total.Load())

	b.WriteString("# HELP calculator_requests_per_second Successful /calc requests in a given unix second.\n")
	b.WriteString("# TYPE calculator_requests_per_second gauge\n")
	for i, v := range m.ps.snapshot(now) {
		sec := now.Unix() - int64(metricSeconds-1-i)
		fmt.Fprintf(b, "calculator_requests_per_second{sec=\"%d\"} %d\n", sec, v)
	}

	m.writeSummary(b, "calculator_c_duration_seconds", &m.c)
	m.writeSummary(b, "calculator_rust_duration_seconds", &m.rust)
}

// Handler owns the running sum/sub state and exposes it over HTTP.
type Handler struct {
	sum      atomic.Int64
	subtract atomic.Int64
	add      native.Op
	sub      native.Op
	m        metrics
}

// New builds a Handler that accumulates with add and subtracts with sub.
func New(add, sub native.Op) *Handler {
	h := &Handler{add: add, sub: sub}
	return h
}

// Mux returns the handler's HTTP routes.
func (h *Handler) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/calc", h.handle)
	mux.HandleFunc("/metrics", h.metrics)
	return mux
}

// PrintTotals prints the current running totals with the given label.
func (h *Handler) PrintTotals(label string) {
	fmt.Printf("[%s] sum=%d sub=%d\n", label, h.sum.Load(), h.subtract.Load())
}

// atomicOp applies op to target and returns how long it took.
func atomicOp(target *atomic.Int64, op native.Op, num int64) time.Duration {
	start := time.Now()
	for {
		old := target.Load()
		new := op(old, num)
		if target.CompareAndSwap(old, new) {
			return time.Since(start)
		}
	}
}

func (h *Handler) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write(bodyNotImplemented)
		return
	}

	num, err := parseNum(r.URL.RawQuery)
	if err != nil {
		if err == errMissingNum {
			w.WriteHeader(http.StatusBadRequest)
			w.Write(bodyMissingNum)
		} else {
			w.WriteHeader(http.StatusBadRequest)
			w.Write(bodyBadNum)
		}
		return
	}

	dC := atomicOp(&h.sum, h.add, num)
	dRust := atomicOp(&h.subtract, h.sub, num)
	h.m.record(dC, dRust, time.Now())

	w.Write(bodyOK)
}

func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write(bodyNotImplemented)
		return
	}
	var b strings.Builder
	h.m.write(&b)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(b.String()))
}
