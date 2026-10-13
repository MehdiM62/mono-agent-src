// Command loadgen is the HTTP client of the Mono Agent vs n8n benchmark
// (scripts/bench/vs-n8n/run.sh). It runs inside a helper container that owns
// the network namespace both servers join, so each server keeps its default
// loopback-only bind.
//
//	loadgen wait -url U [-timeout 120s]
//	    POSTs to U every 5 ms until one answers 2xx; prints the wall-clock
//	    time of that answer as Unix nanoseconds.
//	loadgen fire -url U -n 1000 -c 10
//	    POSTs {"n":i} to U n times from c workers; prints a JSON summary.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loadgen wait|fire -url URL ...")
		os.Exit(2)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	url := fs.String("url", "", "webhook URL")
	n := fs.Int("n", 1000, "requests to send (fire)")
	c := fs.Int("c", 10, "concurrent workers (fire)")
	timeout := fs.Duration("timeout", 120*time.Second, "give up after (wait)")
	_ = fs.Parse(os.Args[2:])
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 256}}

	var firstErr atomic.Value // the first failure, reported by fire
	post := func(i int) bool {
		resp, err := client.Post(*url, "application/json", bytes.NewBufferString(fmt.Sprintf(`{"n":%d}`, i)))
		if err != nil {
			firstErr.CompareAndSwap(nil, err.Error())
			return false
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			firstErr.CompareAndSwap(nil, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, body))
			return false
		}
		return true
	}

	switch os.Args[1] {
	case "wait":
		fmt.Fprintln(os.Stderr, "loadgen: polling")
		deadline := time.Now().Add(*timeout)
		for time.Now().Before(deadline) {
			if post(-1) {
				fmt.Println(time.Now().UnixNano())
				fmt.Fprintln(os.Stderr, "loadgen: ready")
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		fmt.Fprintln(os.Stderr, "loadgen: not ready before timeout")
		os.Exit(1)
	case "fire":
		var ok, failed atomic.Int64
		jobs := make(chan int)
		var wg sync.WaitGroup
		start := time.Now()
		for w := 0; w < *c; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					if post(i) {
						ok.Add(1)
					} else {
						failed.Add(1)
					}
				}
			}()
		}
		for i := 0; i < *n; i++ {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"sent": *n, "ok": ok.Load(), "failed": failed.Load(),
			"start_ns": start.UnixNano(), "requests_ms": time.Since(start).Milliseconds(),
			"first_error": firstErr.Load(),
		})
	default:
		fmt.Fprintln(os.Stderr, "loadgen: unknown mode", os.Args[1])
		os.Exit(2)
	}
}
