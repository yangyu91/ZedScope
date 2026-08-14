package proxy

import "strings"

// Search returns the captured records matching all of the given criteria.
// Every parameter is optional / additive:
//   - keyword: case-insensitive substring match against URL, Method, request
//     headers, request body, response headers and response body. Empty = any.
//   - status: exact HTTP status code filter. 0 = any.
//   - contentType: case-insensitive substring match against the response
//     Content-Type header (e.g. "json", "text/html", "image/"). Empty = any.
//
// The result preserves the store's natural oldest -> newest ordering.
func (s *Store) Search(keyword string, status int, contentType string) []*Record {
	s.mu.Lock()
	items := make([]*Record, len(s.items))
	copy(items, s.items)
	s.mu.Unlock()

	kw := strings.ToLower(keyword)
	ct := strings.ToLower(contentType)

	out := make([]*Record, 0, len(items))
	for _, r := range items {
		if status != 0 && r.StatusCode != status {
			continue
		}
		if ct != "" && !strings.Contains(strings.ToLower(contentTypeOf(r.RespHeaders)), ct) {
			continue
		}
		if kw != "" {
			hay := strings.ToLower(r.URL + " " + r.Method + " " +
				r.ReqHeaders + " " + r.ReqBody + " " + r.RespHeaders + " " + r.RespBody)
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// contentTypeOf extracts the value of the (case-insensitive) Content-Type
// response header from a raw header dump ("Key: Value\r\n" lines).
func contentTypeOf(headerDump string) string {
	for _, line := range strings.Split(headerDump, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "content-type:") {
			v := strings.TrimSpace(line[len("content-type:"):])
			// strip any "; charset=..." suffix for matching convenience
			if i := strings.IndexByte(v, ';'); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			return v
		}
	}
	return ""
}
