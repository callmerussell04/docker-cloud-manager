package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type RegistryTokenLogic interface {
	Exchange(ctx context.Context, username, password, audience string, scopes []string) (string, error)
}

// RegistryTokenHandler is mounted on a dedicated TLS listener, never alongside
// Core's control-plane routes. Passwords are accepted only in Basic auth headers.
func RegistryTokenHandler(logic RegistryTokenLogic, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || q.Get("offline_token") == "true" || q.Has("access_token") || q.Has("password") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		user, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="dcm-registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		scopes := []string{}
		for _, value := range q["scope"] {
			scopes = append(scopes, strings.Fields(value)...)
		}
		token, err := logic.Exchange(ctx, user, password, q.Get("service"), scopes)
		if err != nil {
			code := http.StatusServiceUnavailable
			if errors.Is(err, apperrors.ErrForbidden) || errors.Is(err, apperrors.ErrNotFound) {
				code = http.StatusUnauthorized
			}
			writeRegistryTokenResponse(r.Context(), w, logger, code, map[string]string{"error": "registry authorization unavailable or denied"})
			return
		}
		writeRegistryTokenResponse(r.Context(), w, logger, http.StatusOK, map[string]any{"token": token, "access_token": token, "expires_in": int64(service.RegistryTokenTTL / time.Second), "issued_at": time.Now().UTC().Format(time.RFC3339)})
	})
}

// writeRegistryTokenResponse encodes before writing so an encoding failure
// still produces a status. A failed write means the client is gone; it is
// logged once without the body, which may carry a token.
func writeRegistryTokenResponse(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logger.ErrorContext(ctx, "failed to encode registry token response", "operation", "registry token response", "error", err)
		return
	}
	w.WriteHeader(status)
	if _, err := w.Write(append(raw, '\n')); err != nil {
		logger.WarnContext(ctx, "failed to write registry token response", "operation", "registry token response", "status", status, "error", err)
	}
}

// RegistryEdgeHandler exposes only the Registry API and token realm. All access
// decisions on API paths are still enforced by Distribution's native verifier.
func RegistryEdgeHandler(target *url.URL, authority string, transport http.RoundTripper, logic RegistryTokenLogic, logger *slog.Logger) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite:   func(r *httputil.ProxyRequest) { r.SetURL(target); r.Out.Host = authority },
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "registry unavailable", http.StatusServiceUnavailable)
		},
	}
	tokenHandler := RegistryTokenHandler(logic, logger)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		r = r.WithContext(ctx)
		switch {
		case r.URL.Path == "/token":
			tokenHandler.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/v2/"):
			proxy.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
