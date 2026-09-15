package main

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

typedef int64_t (*binary_fn)(int64_t, int64_t);

static int64_t call_binary(binary_fn f, int64_t a, int64_t b) {
    return f(a, b);
}
*/
import "C"

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	stateMu  sync.Mutex
	sumValue int64
	subValue int64
)

type binaryOp func(int64, int64) int64

func loadLibrary(path string) (unsafe.Pointer, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	handle := C.dlopen(cpath, C.RTLD_NOW)
	if handle == nil {
		return nil, fmt.Errorf("%s", C.GoString(C.dlerror()))
	}
	return handle, nil
}

func dlsym(handle unsafe.Pointer, name string) (binaryOp, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	sym := C.dlsym(handle, cname)
	if sym == nil {
		return nil, fmt.Errorf("symbol %q not found", name)
	}

	fn := C.binary_fn(unsafe.Pointer(sym))
	return func(a, b int64) int64 {
		return int64(C.call_binary(fn, C.int64_t(a), C.int64_t(b)))
	}, nil
}

func printTotals(label string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	fmt.Printf("[%s] sum=%d sub=%d\n", label, sumValue, subValue)
}

func periodicPrinter(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			printTotals("periodic")
		}
	}
}

func main() {
	host := flag.String("host", "0.0.0.0", "bind host")
	port := flag.Int("port", 8080, "bind port")
	cLibPath := flag.String("c-lib", "libcalculator.so", "path to the compiled C shared library")
	rustLibPath := flag.String("rust-lib", "libcalculator_rust.so", "path to the compiled Rust shared library")
	interval := flag.Duration("interval", 5*time.Second, "time between periodic sum/sub reports")
	flag.Parse()

	cHandle, err := loadLibrary(*cLibPath)
	if err != nil {
		log.Fatalf("Failed to load native libraries: %v\nDid you run build.sh first?", err)
	}
	rustHandle, err := loadLibrary(*rustLibPath)
	if err != nil {
		log.Fatalf("Failed to load native libraries: %v\nDid you run build.sh first?", err)
	}

	cAdd, err := dlsym(cHandle, "add")
	if err != nil {
		log.Fatalf("Failed to load native libraries: %v\nDid you run build.sh first?", err)
	}
	rustSub, err := dlsym(rustHandle, "sub")
	if err != nil {
		log.Fatalf("Failed to load native libraries: %v\nDid you run build.sh first?", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/calc", func(w http.ResponseWriter, r *http.Request) {
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

		stateMu.Lock()
		sumValue = cAdd(sumValue, num)
		subValue = rustSub(subValue, num)
		stateMu.Unlock()

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", *host, *port),
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go periodicPrinter(ctx, *interval)

	fmt.Printf("Calculator server listening on %s:%d\n", *host, *port)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		fmt.Printf("\nSIGINT received, shutting down...\n")
		printTotals("final")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}
