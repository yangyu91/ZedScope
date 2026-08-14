package yami

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"yamiua/ai"
	"yamiua/proxy"
)

// browserCmd is a serialized browser action handed to the Android layer.
type browserCmd struct {
	ID       string `json:"id"`
	Action   string `json:"action"` // navigate|click|type|extract
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
}

// browserAdapter implements ai.BrowserDriver by queueing commands for the
// Android WebView (which executes them via a JS bridge) and waiting for the
// result. This avoids cross-language callbacks and needs no Android permission:
// the browser runs the AI's instructions inside its own page context.
type browserAdapter struct {
	mu      sync.Mutex
	cond    *sync.Cond
	seq     int
	pending map[string]chan string
	queue   []browserCmd
}

func newBrowserAdapter() *browserAdapter {
	b := &browserAdapter{pending: map[string]chan string{}}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// Next returns the next pending browser command (blocking). Called by the
// Android layer on a background thread to drain the agent's actions.
func (b *browserAdapter) Next() (browserCmd, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.queue) == 0 {
		b.cond.Wait()
	}
	c := b.queue[0]
	b.queue = b.queue[1:]
	return c, true
}

// Complete posts the result of a command previously returned by Next.
func (b *browserAdapter) Complete(id, result string) {
	b.mu.Lock()
	ch, ok := b.pending[id]
	b.mu.Unlock()
	if ok {
		ch <- result
	}
}

func (b *browserAdapter) exec(action, url, sel, text string) (string, error) {
	b.mu.Lock()
	b.seq++
	id := fmt.Sprintf("b%d", b.seq)
	ch := make(chan string, 1)
	b.pending[id] = ch
	b.queue = append(b.queue, browserCmd{ID: id, Action: action, URL: url, Selector: sel, Text: text})
	b.cond.Signal()
	b.mu.Unlock()
	select {
	case res := <-ch:
		return res, nil
	case <-time.After(60 * time.Second):
		return "", fmt.Errorf("browser action %q timed out", action)
	}
}

func (b *browserAdapter) Navigate(url string) (string, error) { return b.exec("navigate", url, "", "") }
func (b *browserAdapter) Click(sel string) (string, error)    { return b.exec("click", "", sel, "") }
func (b *browserAdapter) Type(sel, text string) (string, error) {
	return b.exec("type", "", sel, text)
}
func (b *browserAdapter) Extract() (string, error) { return b.exec("extract", "", "", "") }

// captureAdapter bridges proxy.Store to ai.CaptureSource.
type captureAdapter struct{ store *proxy.Store }

func (c *captureAdapter) Captures() []ai.Capture {
	var out []ai.Capture
	for _, r := range c.store.List() {
		out = append(out, ai.Capture{
			ID:          fmt.Sprintf("%d", r.ID),
			Method:      r.Method,
			URL:         r.URL,
			ReqHeaders:  r.ReqHeaders,
			RespHeaders: r.RespHeaders,
			ReqBody:     r.ReqBody,
			RespBody:    r.RespBody,
		})
	}
	return out
}

func (c *captureAdapter) Tokens() []ai.Token {
	var out []ai.Token
	for _, r := range c.store.List() {
		for _, t := range r.Tokens {
			out = append(out, ai.Token{Key: t.Key, Value: t.Value, Source: t.Source})
		}
	}
	return out
}

// pendingBrowserJSON drains one browser command for the Android layer.
func pendingBrowserJSON() string {
	if browserAd == nil {
		return ""
	}
	cmd, ok := browserAd.Next()
	if !ok {
		return ""
	}
	b, _ := json.Marshal(cmd)
	return string(b)
}
