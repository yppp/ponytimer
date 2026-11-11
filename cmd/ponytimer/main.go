// Command ponytimer serves the PonyTimer tournament clock.
//
// Step B: the binary serves the REST API and the SSE event stream on top of
// a SQLite database. The management web pages arrive in a later step.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/yppp/ponytimer/internal/core"
	"github.com/yppp/ponytimer/internal/httpapi"
	"github.com/yppp/ponytimer/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	data := flag.String("data", "ponytimer.db", "path to the SQLite database file")
	flag.Parse()

	st, err := store.Open(*data)
	if err != nil {
		log.Fatalf("ponytimer: cannot open database %s: %v", *data, err)
	}
	defer st.Close()

	srv := httpapi.New(st, core.RealClock{})
	if err := srv.Start(); err != nil {
		log.Fatalf("ponytimer: cannot start server: %v", err)
	}
	defer srv.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "PonyTimer — development placeholder")
	})
	// The API routes are more specific than "/", so they take precedence.
	mux.Handle("/api/", srv.Handler())
	mux.Handle("/api", srv.Handler())

	log.Printf("ponytimer: listening on %s (data=%s)", *addr, *data)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("ponytimer: %v", err)
	}
}
