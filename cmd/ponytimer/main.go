// Command ponytimer serves the PonyTimer tournament clock.
//
// Step A: the binary only exposes /healthz plus a placeholder page; the real
// wiring (database, REST, SSE) arrives in later steps.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	data := flag.String("data", "ponytimer.db", "path to the SQLite database file")
	flag.Parse()

	// The database is not used yet (step A); the flag value is accepted and
	// stored so later steps can pick it up unchanged.
	dataPath := *data
	log.Printf("ponytimer: listening on %s (data=%s)", *addr, dataPath)

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

	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("ponytimer: %v", err)
	}
}
