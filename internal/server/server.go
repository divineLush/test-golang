package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

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

// Handler owns the running sum/sub state and exposes it over HTTP.
type Handler struct {
	sum      atomic.Int64
	subtract atomic.Int64
	add      native.Op
	sub      native.Op
}

// New builds a Handler that accumulates with add and subtracts with sub.
func New(add, sub native.Op) *Handler {
	return &Handler{add: add, sub: sub}
}

// Mux returns the handler's HTTP routes.
func (h *Handler) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/calc", h.handle)
	return mux
}

// PrintTotals prints the current running totals with the given label.
func (h *Handler) PrintTotals(label string) {
	fmt.Printf("[%s] sum=%d sub=%d\n", label, h.sum.Load(), h.subtract.Load())
}

func atomicOp(target *atomic.Int64, op native.Op, num int64) {
	for {
		old := target.Load()
		new := op(old, num)
		if target.CompareAndSwap(old, new) {
			return
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

	atomicOp(&h.sum, h.add, num)
	atomicOp(&h.subtract, h.sub, num)

	w.Write(bodyOK)
}
