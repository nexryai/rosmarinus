package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

type ActorLookup interface {
	FindLocalByID(context.Context, string) (*actors.Actor, error)
	FindLocalByUsername(context.Context, string) (*actors.Actor, error)
}

func NewHandler(cfg config.Config, logger *log.Logger, actorLookup ActorLookup) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/inbox", notImplemented(logger, http.MethodPost))
	mux.HandleFunc("/users/", notImplemented(logger, http.MethodGet, http.MethodPost))
	mux.HandleFunc("/notes/", notImplemented(logger, http.MethodGet))
	mux.HandleFunc("/emojis/", notImplemented(logger, http.MethodGet))
	mux.HandleFunc("/likes/", notImplemented(logger, http.MethodGet))
	mux.HandleFunc("/follows/", notImplemented(logger, http.MethodGet))
	mux.HandleFunc("/.well-known/", wellKnown(cfg, actorLookup))
	mux.HandleFunc("/nodeinfo/", nodeInfo(cfg))
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

func wellKnown(cfg config.Config, actorLookup ActorLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setWellKnownCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/.well-known/host-meta":
			hostMeta(w, cfg)
		case "/.well-known/host-meta.json":
			hostMetaJSON(w, cfg)
		case "/.well-known/nodeinfo":
			writeJSON(w, http.StatusOK, map[string]any{"links": nodeInfoLinks(cfg)})
		case "/.well-known/webfinger":
			webFinger(w, r, cfg, actorLookup)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func nodeInfo(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/nodeinfo/2.0":
			writeJSON(w, http.StatusOK, nodeInfoBody(cfg, "2.0"))
		case "/nodeinfo/2.1":
			writeJSON(w, http.StatusOK, nodeInfoBody(cfg, "2.1"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func hostMeta(w http.ResponseWriter, cfg config.Config) {
	w.Header().Set("Content-Type", "application/xrd+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Link rel="lrdd" type="application/xrd+xml" template="%s/.well-known/webfinger?resource={uri}"/></XRD>`, html.EscapeString(strings.TrimRight(cfg.PublicURL, "/")))
}

func hostMetaJSON(w http.ResponseWriter, cfg config.Config) {
	writeJSON(w, http.StatusOK, map[string]any{
		"links": []map[string]string{{
			"rel":      "lrdd",
			"type":     "application/jrd+json",
			"template": strings.TrimRight(cfg.PublicURL, "/") + "/.well-known/webfinger?resource={uri}",
		}},
	})
}

func webFinger(w http.ResponseWriter, r *http.Request, cfg config.Config, actorLookup ActorLookup) {
	if r.URL.Query().Get("resource") == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource is required"})
		return
	}
	if actorLookup == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "actor lookup is not configured"})
		return
	}
	actor, status, err := lookupWebFingerActor(r.Context(), cfg, actorLookup, r.URL.Query().Get("resource"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	if accepts(r, "application/xrd+xml") {
		webFingerXRD(w, cfg, actor)
		return
	}
	webFingerJRD(w, cfg, actor)
}

func nodeInfoLinks(cfg config.Config) []map[string]string {
	base := strings.TrimRight(cfg.PublicURL, "/")
	return []map[string]string{
		{
			"rel":  "http://nodeinfo.diaspora.software/ns/schema/2.0",
			"href": base + "/nodeinfo/2.0",
		},
		{
			"rel":  "http://nodeinfo.diaspora.software/ns/schema/2.1",
			"href": base + "/nodeinfo/2.1",
		},
	}
}

func nodeInfoBody(cfg config.Config, version string) map[string]any {
	return map[string]any{
		"version": version,
		"software": map[string]any{
			"name":    "rosmarinus",
			"version": "0.0.1",
		},
		"protocols":         []string{"activitypub"},
		"services":          map[string]any{"inbound": []string{}, "outbound": []string{}},
		"openRegistrations": false,
		"usage": map[string]any{
			"users":         map[string]any{"total": 0, "activeHalfyear": 0, "activeMonth": 0},
			"localPosts":    0,
			"localComments": 0,
		},
		"metadata": map[string]any{
			"nodeName":        cfg.Host,
			"nodeDescription": "Rosmarinus ActivityPub server",
		},
	}
}

func setWellKnownCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Headers", "Accept")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Vary")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func lookupWebFingerActor(ctx context.Context, cfg config.Config, lookup ActorLookup, resource string) (*actors.Actor, int, error) {
	base := strings.ToLower(strings.TrimRight(cfg.PublicURL, "/"))
	lowerResource := strings.ToLower(resource)
	if strings.HasPrefix(lowerResource, base+"/users/") {
		id := resource[strings.LastIndex(resource, "/")+1:]
		actor, err := lookup.FindLocalByID(ctx, id)
		if actor == nil && err == nil {
			return nil, http.StatusNotFound, nil
		}
		return actor, http.StatusOK, err
	}
	if strings.HasPrefix(lowerResource, base+"/@") {
		username := resource[strings.LastIndex(resource, "/@")+2:]
		actor, err := lookup.FindLocalByUsername(ctx, username)
		if actor == nil && err == nil {
			return nil, http.StatusNotFound, nil
		}
		return actor, http.StatusOK, err
	}
	acct := resource
	if strings.HasPrefix(strings.ToLower(acct), "acct:") {
		acct = acct[len("acct:"):]
	}
	username, host, ok := strings.Cut(acct, "@")
	if !ok {
		return nil, http.StatusBadRequest, nil
	}
	if host != "" && !strings.EqualFold(host, cfg.Host) {
		return nil, http.StatusUnprocessableEntity, nil
	}
	actor, err := lookup.FindLocalByUsername(ctx, username)
	if actor == nil && err == nil {
		return nil, http.StatusNotFound, nil
	}
	return actor, http.StatusOK, err
}

func webFingerJRD(w http.ResponseWriter, cfg config.Config, actor *actors.Actor) {
	w.Header().Set("Content-Type", "application/jrd+json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(webFingerBody(cfg, actor))
}

func webFingerXRD(w http.ResponseWriter, cfg config.Config, actor *actors.Actor) {
	body := webFingerBody(cfg, actor)
	links := body["links"].([]map[string]string)
	w.Header().Set("Content-Type", "application/xrd+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Subject>%s</Subject>`, html.EscapeString(body["subject"].(string)))
	for _, link := range links {
		_, _ = fmt.Fprintf(w, `<Link rel="%s"`, html.EscapeString(link["rel"]))
		for _, attr := range []string{"type", "href", "template"} {
			if link[attr] != "" {
				_, _ = fmt.Fprintf(w, ` %s="%s"`, attr, html.EscapeString(link[attr]))
			}
		}
		_, _ = w.Write([]byte("/>"))
	}
	_, _ = w.Write([]byte("</XRD>"))
}

func webFingerBody(cfg config.Config, actor *actors.Actor) map[string]any {
	base := strings.TrimRight(cfg.PublicURL, "/")
	actorURI := actor.URI
	if actorURI == "" {
		actorURI = base + "/users/" + url.PathEscape(actor.ID)
	}
	subject := "acct:" + actor.Username + "@" + cfg.Host
	return map[string]any{
		"subject": subject,
		"links": []map[string]string{
			{
				"rel":  "self",
				"type": "application/activity+json",
				"href": actorURI,
			},
			{
				"rel":  "http://webfinger.net/rel/profile-page",
				"type": "text/html",
				"href": base + "/@" + url.PathEscape(actor.Username),
			},
			{
				"rel":      "http://ostatus.org/schema/1.0/subscribe",
				"template": base + "/authorize-follow?acct={uri}",
			},
		},
	}
}

func accepts(r *http.Request, contentType string) bool {
	accept := r.Header.Get("Accept")
	return accept != "" && strings.Contains(strings.ToLower(accept), strings.ToLower(contentType))
}
