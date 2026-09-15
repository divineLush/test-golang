// Package server provides the calculator HTTP handler backed by native ops.
package server

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"calculator/internal/native"
)

// Handler owns the running sum/sub state and exposes it over HTTP.
type Handler struct {
	mu       sync.Mutex
	sum      int64
	subtract int64
	add      native.Op
	sub      native.Op

	mux *http.ServeMux
}

// New builds a Handler that accumulates with add and subtracts with sub.
func New(add, sub native.Op) *Handler {
	h := &Handler{add: add, sub: sub, mux: http.NewServeMux()}
	h.mux.HandleFunc("/calc", h.calc)
	return h
}

// Mux returns the handler's HTTP routes.
func (h *Handler) Mux() *http.ServeMux {
	return h.mux
}

// PrintTotals prints the current running totals with the given label.
func (h *Handler) PrintTotals(label string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Printf("[%s] sum=%d sub=%d\n", label, h.sum, h.subtract)
}

func (h *Handler) calc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte("not implemented"))
		return
	}

	rawNum := r.URL.Query().Get("num")
	if rawNum == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("missing 'num' query parameter"))
		return
	}

	num, err := strconv.ParseInt(rawNum, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("'num' must be an integer"))
		return
	}

	h.mu.Lock()
	h.sum = h.add(h.sum, num)
	h.subtract = h.sub(h.subtract, num)
	h.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
