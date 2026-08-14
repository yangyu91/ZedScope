// Package api exposes a local HTTP API the Android UI talks to.
// It only binds 127.0.0.1 so captured data never leaves the device.
package api

import (
	"encoding/json"
	"net/http"

	"yamiua/proxy"
)

// Server wraps a Proxy with a JSON API.
type Server struct {
	p *proxy.Proxy
}

// NewAPI builds an API server for the given proxy.
func NewAPI(p *proxy.Proxy) *Server { return &Server{p: p} }

// Handler returns the API mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/captures", s.listCaptures)
	mux.HandleFunc("/api/ca", s.ca)
	mux.HandleFunc("/api/tokens", s.tokens)
	mux.HandleFunc("/api/clear", s.clear)
	mux.HandleFunc("/api/mods", s.setMods)
	// 抓包增强（ProxyPin 差距补全）：规则 / HAR 导出 / 搜索
	mux.HandleFunc("/api/rules", s.rules)
	mux.HandleFunc("/api/har", s.harExport)
	mux.HandleFunc("/api/search", s.searchCaptures)
	return mux
}

// Listen starts the API server (blocking).
func (s *Server) Listen(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}

func (s *Server) listCaptures(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.p.Store.List())
}

func (s *Server) ca(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Write([]byte(s.p.CAPEM()))
}

func (s *Server) tokens(w http.ResponseWriter, r *http.Request) {
	var toks []proxy.Token
	for _, rec := range s.p.Store.List() {
		toks = append(toks, rec.Tokens...)
	}
	// de-duplicate by key+value
	seen := map[string]bool{}
	uniq := toks[:0]
	for _, t := range toks {
		k := t.Key + "\x00" + t.Value
		if seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, t)
	}
	writeJSON(w, uniq)
}

func (s *Server) clear(w http.ResponseWriter, r *http.Request) {
	s.p.Store.Clear()
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) setMods(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var rules []proxy.ModifyRule
	if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
		writeJSON(w, map[string]string{"status": "err", "msg": err.Error()})
		return
	}
	s.p.Mods.Set(rules)
	writeJSON(w, map[string]interface{}{"status": "ok", "count": len(rules)})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// rules serves GET (current RulesConfig) and POST (install a RulesConfig JSON).
func (s *Server) rules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.p.GetRules())
	case http.MethodPost:
		var cfg proxy.RulesConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeJSON(w, map[string]string{"status": "err", "msg": err.Error()})
			return
		}
		if err := s.p.SetRules(cfg); err != nil {
			writeJSON(w, map[string]string{"status": "err", "msg": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})
	default:
		http.Error(w, "GET/POST only", 405)
	}
}

// harExport streams all captures as a downloadable HAR document.
func (s *Server) harExport(w http.ResponseWriter, r *http.Request) {
	b, err := proxy.ExportHAR(s.p.Store)
	if err != nil {
		writeJSON(w, map[string]string{"status": "err", "msg": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=yami-capture.har")
	w.Write(b)
}

// searchCaptures searches captures by JSON body {"keyword","status","content_type"}.
func (s *Server) searchCaptures(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Keyword     string `json:"keyword"`
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
	}
	_ = json.NewDecoder(r.Body).Decode(&q)
	writeJSON(w, s.p.Store.Search(q.Keyword, q.Status, q.ContentType))
}
