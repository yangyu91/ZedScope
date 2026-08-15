// Package ai implements the built-in AI relay/agent core for yami-UA.
//
// Design goals (pure Go standard library, zero external deps so it cross
// compiles to android/arm64 and runs offline):
//
//   - OpenAI-compatible /v1/chat/completions endpoint (stream + non-stream).
//   - Multi-provider routing with health probe + automatic failover, in the
//     spirit of cc-switch's local proxy (format conversion + failover).
//   - Proxy-aware upstream client: every request to an LLM backend can be
//     forced through yami-UA's own MITM capture proxy, so "all AI traffic
//     walks the proxy" and is itself inspectable.
//   - DeepSeek "free ride" bridge: reuse the logged-in DeepSeek *web* session
//     (cookie jar from the WebView) to drive chat.deepseek.com, in the spirit
//     of deepseek-pp. No API key required for that path.
//   - A tool-calling agent loop (in the spirit of Coomi-Android) where the AI
//     can drive the in-app browser and analyze captured traffic — without any
//     Android permission, because the browser executes the AI's instructions
//     inside its own WebView via a JS bridge.
package ai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Protocol identifies how a provider is spoken to.
const (
	ProtoOpenAI      = "openai"       // OpenAI-compatible /v1/chat/completions
	ProtoAnthropic   = "anthropic"    // Anthropic /v1/messages
	ProtoDeepseekWeb = "deepseek-web" // chat.deepseek.com web session (cookie)
)

// Provider describes one upstream LLM endpoint.
type Provider struct {
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`      // e.g. https://api.openai.com/v1
	APIKey        string `json:"api_key"`       // may be empty for deepseek-web
	Model         string `json:"model"`         // default model for this provider
	Protocol      string `json:"protocol"`      // Proto*
	UpstreamProxy string `json:"upstream_proxy"` // "" => use engine default proxy
	Cookies       string `json:"cookies"`       // only for deepseek-web (session)
	Token         string `json:"token"`         // only for deepseek-web: Bearer userToken (localStorage) like deepseek-pp
	Healthy       bool   `json:"healthy"`
}

// Registry holds configured providers and the active selection.
type Registry struct {
	mu      sync.RWMutex
	providers map[string]*Provider
	order   []string // failover preference
	active  string

	// DefaultUpstreamProxy forces every backend request through a proxy
	// (typically yami-UA's own MITM capture listener) unless a provider
	// overrides it. Empty means direct.
	DefaultUpstreamProxy string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{providers: map[string]*Provider{}}
}

// AddProvider registers or replaces a provider and appends it to the
// failover order if new.
func (r *Registry) AddProvider(p *Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[p.Name]; !ok {
		r.order = append(r.order, p.Name)
	}
	p.Healthy = true
	r.providers[p.Name] = p
	if r.active == "" {
		r.active = p.Name
	}
}

// SetActive selects the active provider by name.
func (r *Registry) SetActive(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[name]; !ok {
		return fmt.Errorf("unknown provider %q", name)
	}
	r.active = name
	return nil
}

// Active returns the active provider (or nil).
func (r *Registry) Active() *Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[r.active]
}

// List returns a snapshot of all providers.
func (r *Registry) List() []*Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Provider, 0, len(r.providers))
	for _, n := range r.order {
		out = append(out, r.providers[n])
	}
	return out
}

// candidateOrder returns providers to try: active first, then failover order.
func (r *Registry) candidateOrder() []*Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]bool{}
	var out []*Provider
	if p, ok := r.providers[r.active]; ok && p != nil {
		out = append(out, p)
		seen[r.active] = true
	}
	for _, n := range r.order {
		if seen[n] {
			continue
		}
		if p, ok := r.providers[n]; ok && p != nil {
			out = append(out, p)
		}
	}
	return out
}

// Probe health-checks every provider by hitting its models endpoint (or a
// lightweight completion for deepseek-web). Unreachable providers are marked
// unhealthy so failover skips them.
func (r *Registry) Probe() {
	r.mu.RLock()
	names := append([]string{}, r.order...)
	r.mu.RUnlock()
	for _, n := range names {
		r.mu.RLock()
		p := r.providers[n]
		r.mu.RUnlock()
		if p == nil {
			continue
		}
		p.Healthy = r.probeOne(p)
	}
}

func (r *Registry) probeOne(p *Provider) bool {
	c := httpClientFor(r, p)
	switch p.Protocol {
	case ProtoDeepseekWeb:
		// A logged-in session returns 200 on the conversation list.
		req, err := http.NewRequest(http.MethodGet, "https://chat.deepseek.com/api/v0/conversations", nil)
		if err != nil {
			return false
		}
		applyDsWebClientHeaders(req, p)
		resp, err := c.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	default:
		u := p.BaseURL
		if u == "" {
			return false
		}
		req, err := http.NewRequest(http.MethodGet, u+"/models", nil)
		if err != nil {
			return false
		}
		if p.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
		resp, err := c.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}
}

// httpClientFor returns a proxy-aware client for a provider.
func httpClientFor(r *Registry, p *Provider) *http.Client {
	up := p.UpstreamProxy
	if up == "" && r != nil {
		up = r.DefaultUpstreamProxy
	}
	if up == "" {
		return &http.Client{Timeout: 60 * time.Second}
	}
	u, err := url.Parse(up)
	if err != nil {
		return &http.Client{Timeout: 60 * time.Second}
	}
	return &http.Client{
		Timeout: 90 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
	}
}

// chatCompletionURL returns the upstream completion endpoint for a provider.
func chatCompletionURL(p *Provider) string {
	switch p.Protocol {
	case ProtoAnthropic:
		return p.BaseURL + "/messages"
	case ProtoDeepseekWeb:
		return "https://chat.deepseek.com/api/v0/chat/completion"
	default: // openai
		return p.BaseURL + "/chat/completions"
	}
}

// ensure JSON helpers shared across the package.
func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
