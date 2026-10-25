// Command e2estub is the upstream for the C7b end-to-end test.
//
// It answers 201 and counts the requests it saw, which is what lets the harness prove the claim
// that matters: a rejected record never reached the upstream. A route that only returns a body
// cannot show that — it can only show that some body came back.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sync/atomic"
)

func main() {
	var hits atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc("/__hits", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%d", hits.Load())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			hits.Add(1)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "upstream reached\n")
	})

	fmt.Fprintln(os.Stderr, "e2estub: listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		fmt.Fprintf(os.Stderr, "e2estub: %v\n", err)
		os.Exit(1)
	}
}
