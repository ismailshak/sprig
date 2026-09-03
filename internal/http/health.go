package http

import (
	"encoding/json"
	"net/http"

	"github.com/ismailshak/sprig/internal/build"
)

type healthzResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	GoVersion string `json:"go_version"`
}

// handleHealthz reports whether the process is listening and which binary
// is running.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	info := build.Read()

	w.Header().Set("Content-Type", "application/json")
	// A failed write means the probe hung up, which nothing here can act on.
	_ = json.NewEncoder(w).Encode(healthzResponse{
		Status:    "ok",
		Version:   info.Version,
		Revision:  info.Revision,
		GoVersion: info.GoVersion,
	})
}
