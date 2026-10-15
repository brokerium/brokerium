// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package webui serves the dashboard build embedded into the server binary.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const placeholder = `<!doctype html><html><head><meta charset="utf-8"><title>Brokerium</title></head>
<body style="font-family:sans-serif;margin:3rem"><h1>Brokerium</h1>
<p>The dashboard is not built into this binary. Run <code>make web-build</code> and rebuild the server,
or use <code>make web-dev</code> during development. The API is available under <code>/api/v1</code>.</p></body></html>`

// Handler serves static assets and falls back to index.html for client-side routes.
func Handler() http.Handler {
	sub, _ := fs.Sub(dist, "dist")
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "." {
			if f, err := sub.Open(p); err == nil {
				_ = f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(sub, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if err != nil {
			_, _ = w.Write([]byte(placeholder))
			return
		}
		_, _ = w.Write(index)
	})
}
