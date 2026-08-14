package ai

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ActionSchema describes one agent action for UI display / documentation.
// It is derived from the same single source of truth as the tool schemas and
// the dispatcher, so the three never drift apart.
type ActionSchema struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  string `json:"parameters"` // JSON-schema string
	Requires    string `json:"requires"`   // "" | "browser" | "capture"
}

// actionHandler executes one tool call given the wired capabilities and the
// already-parsed arguments. It MUST NOT panic and MUST NOT return an error
// that crashes the agent loop — failures are returned as descriptive strings.
type actionHandler func(browser BrowserDriver, caps CaptureSource, args map[string]interface{}) string

// actionDef is the single source of truth for one agent action. It feeds the
// OpenAI function schema (tools), the UI action list (AvailableActions) and
// the runtime dispatcher (actionHandlerMap).
type actionDef struct {
	name        string
	description string
	params      string // JSON-schema string
	requires    string // "" | "browser" | "capture"
	handler     actionHandler
}

// actionRegistry lists every action the agent can perform. Order here is the
// order they appear in the tool list and the UI.
var actionRegistry = []actionDef{
	{
		name:        "analyze_capture",
		description: "Inspect a captured HTTP transaction (request/response headers+body) to find tokens, auth headers, params or bugs. Pass the capture id.",
		params:      `{"type":"object","properties":{"id":{"type":"string","description":"capture id"}},"required":["id"]}`,
		requires:    "capture",
		handler:     hAnalyzeCapture,
	},
	{
		name:        "copy_token",
		description: "Return all discovered auth tokens/cookies found in traffic so the user can copy them. No id needed.",
		params:      `{"type":"object","properties":{}}`,
		requires:    "capture",
		handler:     hCopyToken,
	},
	{
		name:        "browser_navigate",
		description: "Navigate the in-app browser to a URL and return a page snapshot.",
		params:      `{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`,
		requires:    "browser",
		handler:     hNavigate,
	},
	{
		name:        "browser_click",
		description: "Click an element in the current page by CSS selector.",
		params:      `{"type":"object","properties":{"selector":{"type":"string"}},"required":["selector"]}`,
		requires:    "browser",
		handler:     hClick,
	},
	{
		name:        "browser_type",
		description: "Type text into an input field by CSS selector.",
		params:      `{"type":"object","properties":{"selector":{"type":"string"},"text":{"type":"string"}},"required":["selector","text"]}`,
		requires:    "browser",
		handler:     hType,
	},
	{
		name:        "browser_extract",
		description: "Extract the visible text / DOM summary of the current page.",
		params:      `{"type":"object","properties":{}}`,
		requires:    "browser",
		handler:     hExtract,
	},
}

// actionHandlerMap is the runtime lookup used by the dispatcher.
var actionHandlerMap = buildHandlerMap()

func buildHandlerMap() map[string]actionHandler {
	m := make(map[string]actionHandler, len(actionRegistry))
	for _, d := range actionRegistry {
		m[d.name] = d.handler
	}
	return m
}

// availableActionNames returns every registered action name (regardless of
// capability availability). Useful for "did you mean" hints on unknown actions.
func availableActionNames() []string {
	names := make([]string, 0, len(actionRegistry))
	for _, d := range actionRegistry {
		names = append(names, d.name)
	}
	return names
}

// capabilityOk reports whether an action requiring `req` can run given the
// wired capabilities.
func capabilityOk(req string, hasBrowser, hasCapture bool) bool {
	switch req {
	case "browser":
		return hasBrowser
	case "capture":
		return hasCapture
	default:
		return true
	}
}

// dispatchToolCall executes a single tool call and returns a result string.
// It is panic-safe: any unexpected panic in a handler is converted into an
// error result so the agent loop can keep going instead of crashing the
// process. Unknown or illegal actions return a safe, descriptive error string
// — they never panic and never abort the run.
func (a *Agent) dispatchToolCall(tc ToolCall) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("dispatcher recovered from panic in %q: %v", tc.Function.Name, r)
		}
	}()

	h, ok := actionHandlerMap[tc.Function.Name]
	if !ok {
		return "unknown tool: " + tc.Function.Name + " — valid actions: " + strings.Join(availableActionNames(), ", ")
	}
	args, err := parseArgs(tc.Function.Arguments)
	if err != nil {
		return fmt.Sprintf("invalid JSON arguments for %q: %s", tc.Function.Name, err.Error())
	}
	return h(a.browser, a.caps, args)
}

// parseArgs decodes the tool-call argument JSON into a generic map. Empty
// arguments decode to an empty map (most tools tolerate that). A malformed
// payload returns an error so the dispatcher can report it safely.
func parseArgs(raw string) (map[string]interface{}, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]interface{}{}, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	if m == nil {
		return map[string]interface{}{}, nil
	}
	return m, nil
}

// stringArg extracts a string argument, tolerating numeric JSON values.
// ok is false when the key is missing or not a scalar we can coerce.
func stringArg(args map[string]interface{}, key string) (string, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(t), true
	default:
		return "", false
	}
}

// ---- action handlers (panic-safe; return strings, never error out) ----

func hAnalyzeCapture(browser BrowserDriver, caps CaptureSource, args map[string]interface{}) string {
	if caps == nil {
		return "illegal action: analyze_capture but no capture source is wired"
	}
	id, ok := stringArg(args, "id")
	if !ok || id == "" {
		return "illegal action: analyze_capture requires a non-empty 'id'"
	}
	for _, c := range caps.Captures() {
		if c.ID == id {
			return fmt.Sprintf("METHOD=%s URL=%s\nREQ_HEADERS=%v\nREQ_BODY=%s\nRESP_HEADERS=%v\nRESP_BODY=%s",
				c.Method, c.URL, c.ReqHeaders, c.ReqBody, c.RespHeaders, truncate(c.RespBody, 4000))
		}
	}
	return "capture id not found: " + id
}

func hCopyToken(browser BrowserDriver, caps CaptureSource, args map[string]interface{}) string {
	if caps == nil {
		return "illegal action: copy_token but no capture source is wired"
	}
	toks := caps.Tokens()
	if len(toks) == 0 {
		return "no tokens discovered yet"
	}
	var b strings.Builder
	for _, t := range toks {
		fmt.Fprintf(&b, "%s = %s  (%s)\n", t.Key, t.Value, t.Source)
	}
	return b.String()
}

func hNavigate(browser BrowserDriver, caps CaptureSource, args map[string]interface{}) string {
	if browser == nil {
		return "illegal action: browser_navigate but no browser driver is wired"
	}
	url, ok := stringArg(args, "url")
	if !ok || url == "" {
		return "illegal action: browser_navigate requires a non-empty 'url'"
	}
	s, err := browser.Navigate(url)
	if err != nil {
		return "navigate error: " + err.Error()
	}
	return s
}

func hClick(browser BrowserDriver, caps CaptureSource, args map[string]interface{}) string {
	if browser == nil {
		return "illegal action: browser_click but no browser driver is wired"
	}
	sel, ok := stringArg(args, "selector")
	if !ok || sel == "" {
		return "illegal action: browser_click requires a non-empty 'selector'"
	}
	s, err := browser.Click(sel)
	if err != nil {
		return "click error: " + err.Error()
	}
	return s
}

func hType(browser BrowserDriver, caps CaptureSource, args map[string]interface{}) string {
	if browser == nil {
		return "illegal action: browser_type but no browser driver is wired"
	}
	sel, ok := stringArg(args, "selector")
	if !ok || sel == "" {
		return "illegal action: browser_type requires a non-empty 'selector'"
	}
	text, ok := stringArg(args, "text")
	if !ok {
		return "illegal action: browser_type requires a 'text' argument"
	}
	s, err := browser.Type(sel, text)
	if err != nil {
		return "type error: " + err.Error()
	}
	return s
}

func hExtract(browser BrowserDriver, caps CaptureSource, args map[string]interface{}) string {
	if browser == nil {
		return "illegal action: browser_extract but no browser driver is wired"
	}
	s, err := browser.Extract()
	if err != nil {
		return "extract error: " + err.Error()
	}
	return s
}

// AvailableActions returns the action schemas available for the given wired
// capabilities. Intended for UI display of what the agent can do.
func AvailableActions(hasBrowser, hasCapture bool) []ActionSchema {
	out := make([]ActionSchema, 0, len(actionRegistry))
	for _, d := range actionRegistry {
		if !capabilityOk(d.requires, hasBrowser, hasCapture) {
			continue
		}
		out = append(out, ActionSchema{
			Name:        d.name,
			Description: d.description,
			Parameters:  d.params,
			Requires:    d.requires,
		})
	}
	return out
}

// AvailableActions returns the action schemas for the agent's current wiring.
func (a *Agent) AvailableActions() []ActionSchema {
	return AvailableActions(a.browser != nil, a.caps != nil)
}
