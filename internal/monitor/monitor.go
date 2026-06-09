// Package monitor embeds and serves the cluster monitoring UI.
//
// Mount this handler in server.go:
//
//	mux.HandleFunc("/monitor",  monitor.Handler)
//	mux.HandleFunc("/monitor/", monitor.Handler)
//
// Then open http://localhost:8080/monitor in a browser.
//
// The page auto-connects to all three nodes via the existing
// /internal/status endpoint — no extra backend work required.
package monitor

import (
	_ "embed"
	"net/http"
)

//go:embed monitor.html
var monitorHTML []byte

// Handler serves the monitoring dashboard.
// Registered at both /monitor and /monitor/ in server.go.
func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(monitorHTML)
}
