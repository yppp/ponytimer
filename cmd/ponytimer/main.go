// Command ponytimer serves the PonyTimer tournament clock.
//
// The binary serves the REST API, the SSE event stream and the embedded web
// interface (management pages and display pages) on top of a SQLite
// database.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"

	"github.com/yppp/ponytimer/internal/auth"
	"github.com/yppp/ponytimer/internal/core"
	"github.com/yppp/ponytimer/internal/httpapi"
	"github.com/yppp/ponytimer/internal/store"
	"github.com/yppp/ponytimer/internal/webui"
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

	authSvc := auth.NewService(st)

	srv := httpapi.New(st, core.RealClock{}, authSvc)
	if err := srv.Start(); err != nil {
		log.Fatalf("ponytimer: cannot start server: %v", err)
	}
	defer srv.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// The web interface. The management pages need a session, the display
	// page is public: the display token is its credential.
	mux.HandleFunc("GET /setup", authSvc.SetupGet)
	mux.HandleFunc("POST /setup", authSvc.SetupPost)
	mux.HandleFunc("GET /login", authSvc.LoginGet)
	mux.HandleFunc("POST /login", authSvc.LoginPost)
	mux.HandleFunc("POST /logout", authSvc.LogoutPost)
	mux.Handle("GET /admin", authSvc.RequireAuth(http.HandlerFunc(webui.RenderAdmin)))
	mux.Handle("GET /admin/editor", authSvc.RequireAuth(http.HandlerFunc(webui.RenderEditor)))
	mux.Handle("GET /admin/accounts", authSvc.RequireAuth(http.HandlerFunc(authSvc.AccountsGet)))
	mux.Handle("POST /admin/accounts", authSvc.RequireAuth(http.HandlerFunc(authSvc.AccountsPost)))
	mux.HandleFunc("GET /display", webui.RenderDisplay)
	mux.Handle("GET /static/", http.StripPrefix("/static/", webui.StaticHandler()))

	// The API routes are more specific than "/", so they take precedence.
	mux.Handle("/api/", srv.Handler())
	mux.Handle("/api", srv.Handler())

	log.Printf("ponytimer: listening on %s (data=%s)", *addr, *data)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("ponytimer: %v", err)
	}
}
