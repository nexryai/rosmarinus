package httpserver

import (
	"log"
	"net/http"
)

func NewHandler(logger *log.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/inbox", notImplemented(logger, http.MethodPost))
	mux.HandleFunc("/users/", notImplemented(logger, http.MethodGet, http.MethodPost))
	mux.HandleFunc("/notes/", notImplemented(logger, http.MethodGet))
	mux.HandleFunc("/emojis/", notImplemented(logger, http.MethodGet))
	mux.HandleFunc("/likes/", notImplemented(logger, http.MethodGet))
	mux.HandleFunc("/follows/", notImplemented(logger, http.MethodGet))
	mux.HandleFunc("/.well-known/", notImplemented(logger, http.MethodGet, http.MethodOptions))
	mux.HandleFunc("/nodeinfo/", notImplemented(logger, http.MethodGet))
	return mux
}

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func notImplemented(logger *log.Logger, methods ...string) http.HandlerFunc {
	allowed := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		allowed[method] = struct{}{}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[r.Method]; !ok {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if logger != nil {
			logger.Printf("http: route skeleton hit method=%s path=%s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"error":"not implemented"}` + "\n"))
	}
}
