// Package webui serves the embedded web interface of PonyTimer.
//
// Everything the browser needs is compiled into the binary: there is no
// external asset, no CDN and no build step, so the interface works on a
// venue machine with no internet connection.
package webui

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html static/*
var content embed.FS

// templates are parsed once at startup; a broken template is a build time
// mistake, not a runtime condition.
var templates = template.Must(template.ParseFS(content, "templates/*.html"))

// staticFS is the embedded static directory.
var staticFS = mustSub(content, "static")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// PageData is the data of a rendered page.
type PageData struct {
	// Error is the error message shown on the page, if any.
	Error string
	// Accounts is the list of accounts shown on the accounts page.
	Accounts []Account
}

// Account is one account of the accounts page.
type Account struct {
	ID       int64
	Username string
	Created  string
}

// render writes a template as an HTML page.
func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// RenderLogin writes the login page.
func RenderLogin(w http.ResponseWriter, errMsg string) {
	render(w, "login.html", PageData{Error: errMsg})
}

// RenderSetup writes the setup page.
func RenderSetup(w http.ResponseWriter, errMsg string) {
	render(w, "setup.html", PageData{Error: errMsg})
}

// RenderAccounts writes the account management page.
func RenderAccounts(w http.ResponseWriter, accounts []Account, errMsg string) {
	render(w, "accounts.html", PageData{Error: errMsg, Accounts: accounts})
}

// RenderAdmin writes the management page.
func RenderAdmin(w http.ResponseWriter, _ *http.Request) {
	render(w, "admin.html", PageData{})
}

// RenderDisplay writes the display page.
func RenderDisplay(w http.ResponseWriter, _ *http.Request) {
	render(w, "display.html", PageData{})
}

// RenderEditor writes the structure editor page.
//
// The page is a shell: the editor reads its target (a tournament, a
// template or nothing at all) from the query string and loads it from the
// API on its own.
func RenderEditor(w http.ResponseWriter, _ *http.Request) {
	render(w, "editor.html", PageData{})
}

// StaticHandler serves the embedded static files (style sheets and
// scripts).
func StaticHandler() http.Handler {
	return http.FileServer(http.FS(staticFS))
}
