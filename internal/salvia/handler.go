// Package salvia serves the production SPA embedded in the Rosmarinus binary.
package salvia

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var embeddedDist embed.FS

type handler struct {
	assets fs.FS
	index  []byte
}

// NewHandler returns an HTTP handler backed only by files embedded at build time.
func NewHandler() http.Handler {
	assets, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		panic("open embedded Salvia assets: " + err.Error())
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		panic("read embedded Salvia index: " + err.Error())
	}
	return &handler{assets: assets, index: index}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "." || name == "" {
		if !acceptsHTML(r) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h.serveIndex(w, r)
		return
	}
	if data, err := fs.ReadFile(h.assets, name); err == nil {
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		serveContent(w, r, name, data)
		return
	}
	if name == "assets" || strings.HasPrefix(name, "assets/") || path.Ext(name) != "" || !acceptsHTML(r) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.serveIndex(w, r)
}

func (h *handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	serveContent(w, r, "index.html", h.index)
}

func serveContent(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func acceptsHTML(r *http.Request) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	return accept == "" || strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*")
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data: blob: https:; script-src 'self'; style-src 'self' 'unsafe-inline'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}
