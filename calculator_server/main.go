package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"calculator/internal/native"
	"calculator/internal/server"
)

func mustNative(err error) {
	if err != nil {
		log.Fatalf("Failed to load native libraries: %v\nDid you run build.sh first?", err)
	}
}

func main() {
	host := flag.String("host", "0.0.0.0", "bind host")
	port := flag.Int("port", 8080, "bind port")
	cLibPath := flag.String("c-lib", "libcalculator.so", "path to the compiled C shared library")
	rustLibPath := flag.String("rust-lib", "libcalculator_rust.so", "path to the compiled Rust shared library")
	interval := flag.Duration("interval", 5*time.Second, "time between periodic sum/sub reports")
	flag.Parse()

	cLib, err := native.Load(*cLibPath)
	mustNative(err)
	rustLib, err := native.Load(*rustLibPath)
	mustNative(err)

	cAdd, err := cLib.Symbol("add")
	mustNative(err)
	rustSub, err := rustLib.Symbol("sub")
	mustNative(err)

	h := server.New(cAdd, rustSub)
	httpServer := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", *host, *port),
		Handler: h.Mux(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		ticker := time.NewTicker(*interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.PrintTotals("periodic")
			}
		}
	}()

	fmt.Printf("Calculator server listening on %s:%d\n", *host, *port)

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		fmt.Printf("\nSIGINT received, shutting down...\n")
		h.PrintTotals("final")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}
