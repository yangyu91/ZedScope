package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// harHeader mirrors a single HAR name/value header.
type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// harLog is the root HAR object.
type harLog struct {
	Version string      `json:"version"`
	Creator harCreator  `json:"creator"`
	Entries []harEntry  `json:"entries"`
}

// harDocument wraps the log per the HAR 1.2 spec ({"log": {...}}).
type harDocument struct {
	Log harLog `json:"log"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harEntry struct {
	StartedDateTime string     `json:"startedDateTime"`
	Time            float64    `json:"time"`
	Request         harRequest `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}   `json:"cache"`
	Timings         harTimings `json:"timings"`
}

type harRequest struct {
	Method      string      `json:"method"`
	URL         string      `json:"url"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     []harHeader `json:"headers"`
	QueryString []struct{}  `json:"queryString"`
	Cookies     []struct{}  `json:"cookies"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int         `json:"bodySize"`
	PostData    *harPost    `json:"postData,omitempty"`
}

type harPost struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

type harResponse struct {
	Status      int         `json:"status"`
	StatusText  string      `json:"statusText"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     []harHeader `json:"headers"`
	Cookies     []struct{}  `json:"cookies"`
	Content     harContent  `json:"content"`
	RedirectURL string      `json:"redirectURL"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int         `json:"bodySize"`
}

type harContent struct {
	Size         int    `json:"size"`
	Compression  int    `json:"compression"`
	MimeType     string `json:"mimeType"`
	Text         string `json:"text"`
}

type harTimings struct {
	Send    int `json:"send"`
	Wait    int `json:"wait"`
	Receive int `json:"receive"`
}

// ExportHAR serializes the captured records of a Store into a standard HAR 1.2
// JSON document. It is safe to call concurrently with capture.
func ExportHAR(s *Store) ([]byte, error) {
	recs := s.List()
	entries := make([]harEntry, 0, len(recs))
	for _, r := range recs {
		entries = append(entries, recordToEntry(r))
	}
	log := harLog{
		Version: "1.2",
		Creator: harCreator{Name: "yami-UA", Version: "1.0"},
		Entries: entries,
	}
	return json.MarshalIndent(harDocument{Log: log}, "", "  ")
}

// recordToEntry converts a single captured Record into a HAR entry.
func recordToEntry(r *Record) harEntry {
	reqHeaders := parseHeaderDump(r.ReqHeaders)
	respHeaders := parseHeaderDump(r.RespHeaders)

	req := harRequest{
		Method:      r.Method,
		URL:         r.URL,
		HTTPVersion: "HTTP/1.1",
		Headers:     reqHeaders,
		QueryString: []struct{}{},
		Cookies:     []struct{}{},
		HeadersSize: -1,
		BodySize:    len(r.ReqBody),
	}
	if r.ReqBody != "" {
		req.PostData = &harPost{MimeType: "application/octet-stream", Text: r.ReqBody}
	}

	mime := contentTypeOf(r.RespHeaders)
	resp := harResponse{
		Status:      r.StatusCode,
		StatusText:  http.StatusText(r.StatusCode),
		HTTPVersion: "HTTP/1.1",
		Headers:     respHeaders,
		Cookies:     []struct{}{},
		Content: harContent{
			Size:     len(r.RespBody),
			MimeType: mime,
			Text:     r.RespBody,
		},
		RedirectURL: "",
		HeadersSize: -1,
		BodySize:    len(r.RespBody),
	}

	started := r.Time
	if started.IsZero() {
		started = time.Now()
	}
	return harEntry{
		StartedDateTime: started.UTC().Format(time.RFC3339),
		Time:            0,
		Request:         req,
		Response:        resp,
		Cache:           struct{}{},
		Timings:         harTimings{Send: -1, Wait: -1, Receive: -1},
	}
}

// parseHeaderDump turns a raw "Key: Value\r\n" header dump into HAR headers.
func parseHeaderDump(dump string) []harHeader {
	var out []harHeader
	for _, line := range strings.Split(dump, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i := strings.IndexByte(line, ':'); i >= 0 {
			out = append(out, harHeader{
				Name:  strings.TrimSpace(line[:i]),
				Value: strings.TrimSpace(line[i+1:]),
			})
		}
	}
	if out == nil {
		out = []harHeader{}
	}
	return out
}
