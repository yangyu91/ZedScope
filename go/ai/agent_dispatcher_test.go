package ai

import (
	"errors"
	"strings"
	"testing"
)

// newTestAgent builds an agent with the given wiring without needing a Relay.
func newTestAgent(browser BrowserDriver, caps CaptureSource) *Agent {
	return &Agent{browser: browser, caps: caps, sysPrompt: SystemPromptBrowserOperator, maxIters: 8}
}

func toolCall(name, args string) ToolCall {
	return ToolCall{
		ID:       "call_x",
		Type:     "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: name, Arguments: args},
	}
}

// errBrowser simulates a driver that always fails — used to prove the
// dispatcher converts handler errors into strings without panicking.
type errBrowser struct{}

func (errBrowser) Navigate(url string) (string, error) { return "", errors.New("driver down") }
func (errBrowser) Click(sel string) (string, error)    { return "", errors.New("driver down") }
func (errBrowser) Type(sel, text string) (string, error) {
	return "", errors.New("driver down")
}
func (errBrowser) Extract() (string, error) { return "", errors.New("driver down") }

// ---- dispatcher: known action ----

func TestDispatchNavigate(t *testing.T) {
	b := &mockBrowser{}
	a := newTestAgent(b, &mockCaps{})
	out := a.dispatchToolCall(toolCall("browser_navigate", `{"url":"https://x.test/login"}`))
	if !strings.Contains(out, "snapshot of https://x.test/login") {
		t.Fatalf("navigate did not drive browser, got: %s", out)
	}
	if b.last != "https://x.test/login" {
		t.Fatalf("browser.last = %s", b.last)
	}
}

func TestDispatchCopyToken(t *testing.T) {
	a := newTestAgent(&mockBrowser{}, &mockCaps{})
	out := a.dispatchToolCall(toolCall("copy_token", `{}`))
	if !strings.Contains(out, "leaked-token-123") {
		t.Fatalf("copy_token did not list tokens, got: %s", out)
	}
}

func TestDispatchAnalyzeFound(t *testing.T) {
	a := newTestAgent(&mockBrowser{}, &mockCaps{})
	out := a.dispatchToolCall(toolCall("analyze_capture", `{"id":"c1"}`))
	if !strings.Contains(out, "leaked-token-123") {
		t.Fatalf("analyze_capture did not render capture, got: %s", out)
	}
}

// ---- dispatcher: illegal / unknown actions must be safe, never panic ----

func TestDispatchUnknownActionSafe(t *testing.T) {
	a := newTestAgent(&mockBrowser{}, &mockCaps{})
	out := a.dispatchToolCall(toolCall("launch_missiles", `{}`))
	if !strings.HasPrefix(out, "unknown tool:") {
		t.Fatalf("expected 'unknown tool' error, got: %s", out)
	}
	if !strings.Contains(out, "browser_navigate") {
		t.Fatalf("error should hint valid actions, got: %s", out)
	}
}

func TestDispatchMissingRequiredArgSafe(t *testing.T) {
	a := newTestAgent(&mockBrowser{}, &mockCaps{})
	out := a.dispatchToolCall(toolCall("browser_navigate", `{}`))
	if !strings.HasPrefix(out, "illegal action:") {
		t.Fatalf("expected illegal-action error, got: %s", out)
	}
}

func TestDispatchBrowserUnavailableSafe(t *testing.T) {
	a := newTestAgent(nil, &mockCaps{}) // no browser wired
	out := a.dispatchToolCall(toolCall("browser_click", `{"selector":"#x"}`))
	if !strings.Contains(out, "no browser driver") {
		t.Fatalf("expected browser-unavailable error, got: %s", out)
	}
}

func TestDispatchCaptureUnavailableSafe(t *testing.T) {
	a := newTestAgent(&mockBrowser{}, nil) // no capture source wired
	out := a.dispatchToolCall(toolCall("copy_token", `{}`))
	if !strings.Contains(out, "no capture source") {
		t.Fatalf("expected capture-unavailable error, got: %s", out)
	}
}

func TestDispatchMalformedJSONSafe(t *testing.T) {
	a := newTestAgent(&mockBrowser{}, &mockCaps{})
	out := a.dispatchToolCall(toolCall("browser_extract", `{not json`))
	if !strings.Contains(out, "invalid JSON") {
		t.Fatalf("expected invalid-JSON error, got: %s", out)
	}
}

func TestDispatchHandlerErrorIsSafe(t *testing.T) {
	// A handler returning an error string must not panic the loop.
	a := newTestAgent(errBrowser{}, &mockCaps{})
	out := a.dispatchToolCall(toolCall("browser_navigate", `{"url":"x"}`))
	if !strings.Contains(out, "navigate error:") {
		t.Fatalf("expected navigate error string, got: %s", out)
	}
}

// ---- action schema export (UI) ----

func TestAvailableActionsCapabilityFilter(t *testing.T) {
	withBoth := AvailableActions(true, true)
	if len(withBoth) != len(actionRegistry) {
		t.Fatalf("both wired: want %d actions got %d", len(actionRegistry), len(withBoth))
	}
	noBrowser := AvailableActions(false, true)
	for _, s := range noBrowser {
		if s.Requires == "browser" {
			t.Fatalf("browser action leaked into no-browser set: %s", s.Name)
		}
	}
	none := AvailableActions(false, false)
	for _, s := range none {
		if s.Requires != "" {
			t.Fatalf("capability-gated action leaked: %s", s.Name)
		}
	}
	// names match the dispatcher registry exactly
	for _, s := range withBoth {
		if _, ok := actionHandlerMap[s.Name]; !ok {
			t.Fatalf("schema name %q missing from dispatcher", s.Name)
		}
	}
}
