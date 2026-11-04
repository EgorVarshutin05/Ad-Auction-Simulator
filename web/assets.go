package web

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
)

//go:embed index.html style.css app.js
var Files embed.FS

// Handler builds a self-contained page from trusted, embedded assets.
// Styles and scripts do not require additional network requests.
func Handler() (http.Handler, error) {
	page, err := template.ParseFS(Files, "index.html")
	if err != nil {
		return nil, err
	}
	css, err := Files.ReadFile("style.css")
	if err != nil {
		return nil, err
	}
	js, err := Files.ReadFile("app.js")
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	err = page.Execute(&body, struct {
		Styles template.CSS
		Script template.JS
	}{template.CSS(css), template.JS(js)})
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodHead {
			_, _ = w.Write(body.Bytes())
		}
	}), nil
}
