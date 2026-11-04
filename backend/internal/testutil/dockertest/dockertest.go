// Package dockertest serves the Docker Engine HTTP API in-process so Docker
// adapters can be tested without a daemon.
package dockertest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// apiVersion is the Engine API version the fake reports; clients negotiate down to it.
const apiVersion = "1.47"

// Response is a scripted daemon answer. A zero Status marks the route as
// unexpected, so table tests can leave a route unset for cases that must not
// reach it.
type Response struct {
	Status int
	Body   string
}

// OK answers 200 with body.
func OK(body string) Response { return Response{Status: http.StatusOK, Body: body} }

// Created answers 201 with body, as create endpoints do.
func Created(body string) Response { return Response{Status: http.StatusCreated, Body: body} }

// NoContent answers 204, as start and remove endpoints do.
func NoContent() Response { return Response{Status: http.StatusNoContent} }

// Error answers status with the daemon's JSON error shape.
func Error(status int, message string) Response {
	body, _ := json.Marshal(map[string]string{"message": message}) // a string map always marshals
	return Response{Status: status, Body: string(body)}
}

// Request is one recorded API call, with the version prefix removed from Path.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte
}

// Daemon is a fake Docker daemon. Routes are keyed by unversioned path, such
// as "/containers/create"; "METHOD /path" keys take precedence over path keys.
type Daemon struct {
	t      testing.TB
	routes map[string]Response

	mu       sync.Mutex
	requests []Request
}

// Serve starts a fake daemon and points the Docker client environment at it,
// so adapters built with client.FromEnv talk to the fake. It uses t.Setenv, so
// the calling test cannot run in parallel.
func Serve(t testing.TB, routes map[string]Response) *Daemon {
	t.Helper()
	d := &Daemon{t: t, routes: routes}
	server := httptest.NewServer(http.HandlerFunc(d.serveHTTP))
	t.Cleanup(server.Close)
	t.Setenv("DOCKER_HOST", "tcp://"+server.Listener.Addr().String())
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	return d
}

func (d *Daemon) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Api-Version", apiVersion)
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/_ping" {
		_, _ = io.WriteString(w, "OK") // test fake; the client reports write failures
		return
	}
	path := unversioned(r.URL.Path)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		d.t.Errorf("read Docker API request %s %s: %v", r.Method, path, err)
	}
	d.mu.Lock()
	d.requests = append(d.requests, Request{Method: r.Method, Path: path, Query: r.URL.Query(), Body: body})
	d.mu.Unlock()

	response, found := d.routes[r.Method+" "+path]
	if !found {
		response, found = d.routes[path]
	}
	if !found || response.Status == 0 {
		d.t.Errorf("unexpected Docker API call %s %s", r.Method, path)
		w.WriteHeader(http.StatusTeapot)
		return
	}
	w.WriteHeader(response.Status)
	_, _ = io.WriteString(w, response.Body) // test fake; the client reports write failures
}

// Requests returns the recorded calls to path, in arrival order.
func (d *Daemon) Requests(path string) []Request {
	d.mu.Lock()
	defer d.mu.Unlock()
	var matched []Request
	for _, r := range d.requests {
		if r.Path == path {
			matched = append(matched, r)
		}
	}
	return matched
}

// unversioned strips a leading /v1.NN segment from path.
func unversioned(path string) string {
	if rest, ok := strings.CutPrefix(path, "/v"); ok {
		if _, tail, found := strings.Cut(rest, "/"); found {
			return "/" + tail
		}
	}
	return path
}
