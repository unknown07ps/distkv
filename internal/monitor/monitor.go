// Package monitor embeds and serves the cluster monitoring UI.
//
// Mount this handler in server.go alongside the existing routes:
//
//   mux.HandleFunc("/monitor", monitor.Handler)
//   mux.Handle("/monitor/", http.StripPrefix("/monitor/", monitor.StaticHandler()))
//
// Then open http://localhost:8080/monitor in a browser.
//
// The page auto-connects to all three nodes via the browser using the
// existing /internal/status endpoint — no extra backend work required.
package monitor

import (
	_ "embed"
	"net/http"
)

//go:embed monitor.html
var monitorHTML []byte

// Handler serves the monitoring dashboard.
// Mount at GET /monitor on any node.
func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(monitorHTML)
}
