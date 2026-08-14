package proxy

import (
	"io"
	"net/http"
)

// writeBlocked responds to a blocked request with a 403 and a short body.
// The request is never forwarded upstream.
func writeBlocked(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, "blocked by yami-UA rule")
}
