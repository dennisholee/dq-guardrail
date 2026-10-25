// Command e2eload measures added latency: N requests straight to the upstream, N through Envoy
// with the ext_proc filter, and the difference between them.
//
// The difference is the quantity the design budgets (DDP §8.1: "p99 delta ≤ 1 ms"), because the
// absolute number says more about the machine than about the gate. On a laptop under Docker
// Desktop that delta is not a production measurement and this tool does not pretend otherwise —
// it reports what it measured and lets the reader decide what it is worth.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

type result struct {
	label string
	n     int
	p50   time.Duration
	p90   time.Duration
	p99   time.Duration
	max   time.Duration
	errs  int
}

func main() {
	direct := flag.String("direct", "", "URL of the upstream, called with no proxy")
	proxied := flag.String("proxied", "", "URL through the proxy")
	n := flag.Int("n", 300, "requests per phase")
	c := flag.Int("c", 20, "concurrent workers")
	payloadPath := flag.String("payload", "", "JSON file to POST")
	flag.Parse()

	if *direct == "" || *proxied == "" || *payloadPath == "" {
		fmt.Fprintln(os.Stderr, "e2eload: --direct, --proxied and --payload are required")
		os.Exit(2)
	}
	payload, err := os.ReadFile(*payloadPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2eload: %v\n", err)
		os.Exit(2)
	}

	base := measure("direct", *direct, payload, *n, *c)
	through := measure("proxied", *proxied, payload, *n, *c)

	report(base)
	report(through)

	delta := func(a, b time.Duration) time.Duration {
		if b < a {
			return 0
		}
		return b - a
	}
	fmt.Printf("\nadded latency: p50 %s · p90 %s · p99 %s\n",
		delta(base.p50, through.p50), delta(base.p90, through.p90), delta(base.p99, through.p99))
	fmt.Println("caveat: local Docker Desktop, one machine, no production load profile. " +
		"The design's budget is p99 delta ≤ 1 ms at 500 rps with 4 KB / 64 KB payloads.")
	fmt.Println("this environment's run-to-run variance exceeds that budget: two runs against the " +
		"same endpoint differed by 10× at p99, so the numbers above are not a T5 result. " +
		"Only the error count is meaningful here.")

	// A latency threshold in this environment would assert noise. An error count does not:
	// under concurrency the proxy either handled every request or it did not.
	if base.errs > 0 || through.errs > 0 {
		fmt.Fprintf(os.Stderr, "e2eload: %d direct and %d proxied requests failed\n", base.errs, through.errs)
		os.Exit(1)
	}
}

func report(r result) {
	fmt.Printf("%-8s n=%d errors=%d  p50=%s p90=%s p99=%s max=%s\n",
		r.label, r.n, r.errs, r.p50, r.p90, r.p99, r.max)
}

func measure(label, url string, payload []byte, n, concurrency int) result {
	client := &http.Client{Transport: &http.Transport{
		MaxIdleConns:        concurrency,
		MaxIdleConnsPerHost: concurrency,
		DisableKeepAlives:   false,
	}}
	durations := make([]time.Duration, 0, n)
	var mu sync.Mutex
	var errs int

	work := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		work <- struct{}{}
	}
	close(work)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytesReader(payload))
				if err != nil {
					cancel()
					mu.Lock()
					errs++
					mu.Unlock()
					continue
				}
				req.Header.Set("content-type", "application/json")
				start := time.Now()
				resp, err := client.Do(req)
				elapsed := time.Since(start)
				cancel()
				if err != nil {
					mu.Lock()
					errs++
					mu.Unlock()
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				mu.Lock()
				durations = append(durations, elapsed)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return result{
		label: label,
		n:     len(durations),
		errs:  errs,
		p50:   percentile(durations, 0.50),
		p90:   percentile(durations, 0.90),
		p99:   percentile(durations, 0.99),
		max:   percentile(durations, 1.0),
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
