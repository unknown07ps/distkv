// Package server exposes the KV store over HTTP.
//
// Public endpoints (clients use these):
//
//	GET    /key/{key}     — returns value or 404
//	PUT    /key/{key}     — sets value, replicates async
//	DELETE /key/{key}     — deletes key, replicates async
//
// Routing: if the consistent hash ring routes the key to a different node,
// this handler proxies the request to that node. This is transparent to
// the client — any node can serve any key.
//
// Internal endpoints (nodes use these, not clients):
//
//	GET  /internal/ping       — liveness check for gossip
//	POST /internal/replicate  — receive a replicated write
//	GET  /internal/status     — cluster liveness view
//	GET  /monitor             — cluster monitoring dashboard
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/you/madonna/internal/cluster"
	"github.com/you/madonna/internal/monitor"
	"github.com/you/madonna/internal/replication"
	"github.com/you/madonna/internal/store"
)

// Server wires together store, cluster, and replicator into an HTTP server.
type Server struct {
	store       *store.Store
	cluster     *cluster.Cluster
	replicator  *replication.Replicator
	proxyClient *http.Client
}

// New creates a Server.
func New(s *store.Store, c *cluster.Cluster, r *replication.Replicator) *Server {
	return &Server{
		store:      s,
		cluster:    c,
		replicator: r,
		proxyClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Handler returns the root http.Handler for this node.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public API
	mux.HandleFunc("/key/", s.handleKey)

	// Internal — not exposed outside the cluster network
	mux.HandleFunc("/internal/ping", s.handlePing)
	mux.HandleFunc("/internal/replicate", s.handleReplicate)
	mux.HandleFunc("/internal/status", s.cluster.Monitor().StatusHandler())

	// Monitor dashboard — register both with and without trailing slash so
	// the browser doesn't get a 404 on either form of the URL.
	mux.HandleFunc("/monitor", monitor.Handler)
	mux.HandleFunc("/monitor/", monitor.Handler)

	return withCORS(mux)
}

// withCORS adds permissive CORS headers and handles OPTIONS preflight requests.
// The monitor dashboard fetches /internal/status from all three nodes, which
// are on different ports — the browser treats them as cross-origin.
func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, DELETE, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Preflight: respond immediately without hitting the mux.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		h.ServeHTTP(w, r)
	})
}

// handleKey routes GET/PUT/DELETE /key/{key}.
func (s *Server) handleKey(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/key/")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}

	// Find which node owns this key according to the consistent hash ring.
	owner := s.cluster.OwnerOf(key)

	// If we're not the owner, proxy the request to the correct node.
	if !s.cluster.IsSelf(owner) {
		s.proxy(w, r, owner)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGet(w, r, key)
	case http.MethodPut:
		s.handlePut(w, r, key)
	case http.MethodDelete:
		s.handleDelete(w, r, key)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, key string) {
	val, ok := s.store.Get(key)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, val)
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, key string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	value := string(body)

	if err := s.store.Put(key, value); err != nil {
		log.Printf("PUT %s: store error: %v", key, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Async replication: enqueue and return immediately.
	s.replicator.EnqueuePut(key, value)

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, key string) {
	if err := s.store.Delete(key); err != nil {
		log.Printf("DELETE %s: store error: %v", key, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.replicator.EnqueueDelete(key)

	w.WriteHeader(http.StatusNoContent)
}

// proxy forwards the request to the target node and relays its response.
// This implements the "any node can serve any key" property.
func (s *Server) proxy(w http.ResponseWriter, r *http.Request, targetAddr string) {
	url := fmt.Sprintf("http://%s%s", targetAddr, r.URL.RequestURI())

	body, _ := io.ReadAll(r.Body)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, strings.NewReader(string(body)))
	if err != nil {
		http.Error(w, "proxy build request", http.StatusInternalServerError)
		return
	}

	resp, err := s.proxyClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("X-Served-By", targetAddr)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handlePing responds to gossip heartbeat checks.
func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleReplicate applies an inbound replicated operation to local store.
// This endpoint is called by the primary node's replicator.
func (s *Server) handleReplicate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var op replication.Op
	if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}

	var err error
	switch op.Op {
	case "PUT":
		err = s.store.Put(op.Key, op.Value)
	case "DELETE":
		err = s.store.Delete(op.Key)
	default:
		http.Error(w, "unknown op", http.StatusBadRequest)
		return
	}

	if err != nil {
		log.Printf("replicate %s %s: %v", op.Op, op.Key, err)
		http.Error(w, "apply op", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}