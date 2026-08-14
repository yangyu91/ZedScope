package proxy

import (
	"io"
	"net/http"
)

// writeMapped serves a locally-constructed response for a matched map rule,
// without contacting the remote origin.
func writeMapped(w http.ResponseWriter, m MapRule) {
	if m.ContentType != "" {
		w.Header().Set("Content-Type", m.ContentType)
	}
	w.WriteHeader(m.Status)
	if m.Body != "" {
		_, _ = io.WriteString(w, m.Body)
	}
}
