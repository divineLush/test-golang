package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type stats struct {
	ok     atomic.Int64
	errors atomic.Int64
}

func worker(ctx context.Context, id int, endpoint *url.URL, client *http.Client, interval time.Duration, st *stats) {
	for ctx.Err() == nil {
		u := *endpoint
		num := rand.IntN(201) - 100
		q := u.Query()
		q.Set("num", strconv.Itoa(num))
		u.RawQuery = q.Encode()

		req, err := http.NewRequest(http.MethodPost, u.String(), nil)
		if err != nil {
			st.errors.Add(1)
			fmt.Printf("[worker %d] request failed: %v\n", id, err)
		} else {
			req = req.WithContext(ctx)
			resp, err := client.Do(req)
			if err != nil {
				st.errors.Add(1)
				fmt.Printf("[worker %d] request failed: %v\n", id, err)
			} else {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				st.ok.Add(1)
			}
		}

		if interval > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}
}

func main() {
	argURL := flag.String("url", "http://localhost:8080/calc", "calculator endpoint")
	threads := flag.Int("n", 10, "number of worker goroutines")
	interval := flag.Float64("interval", 0.1, "pause between requests per worker, in seconds (0 = as fast as possible)")
	timeout := flag.Float64("timeout", 5.0, "HTTP request timeout, seconds")
	flag.Parse()

	parsedURL, err := url.Parse(*argURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid url %q: %v\n", *argURL, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: time.Duration(*timeout * float64(time.Second))}
	intervalDur := time.Duration(*interval * float64(time.Second))

	st := &stats{}
	var wg sync.WaitGroup
	for i := 0; i < *threads; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(ctx, id, parsedURL, client, intervalDur, st)
		}(i)
	}

	fmt.Printf("Generator started: %d threads -> %s\n", *threads, *argURL)

	<-ctx.Done()
	fmt.Println("\nSIGINT received, stopping generator...")

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	fmt.Printf("Total requests: ok=%d errors=%d\n", st.ok.Load(), st.errors.Load())
}
