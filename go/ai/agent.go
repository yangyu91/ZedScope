package ai

import (
	"encoding/json"
	"fmt"
)

// BrowserDriver is implemented by the Android layer. The AI drives the in-app
// WebView through it — no Android permission is needed because the browser
// executes the instructions inside its own page context (JS bridge).
type BrowserDriver interface {
	Navigate(url string) (string, error) // returns a short page snapshot
	Click(selector string) (string, error)
	Type(selector, text string) (string, error)
	Extract() (string, error) // visible text / DOM summary of current page
}

// Capture is a minimal view of a captured transaction for the agent.
type Capture struct {
	ID          string
	Method      string
	URL         string
	ReqHeaders  string
	RespHeaders string
	ReqBody     string
	RespBody    string
}

// Token is a discovered credential-ish value.
type Token struct {
	Key    string
	Value  string
	Source string
}

// CaptureSource feeds captured traffic to the agent for analysis.
type CaptureSource interface {
	Captures() []Capture
	Tokens() []Token
}

// Agent runs a tool-calling loop: the LLM decides browser/capture actions,
// yami-UA executes them, and results flow back — closing the loop
// capture -> analyze -> act -> capture again.
type Agent struct {
	relay     *Relay
	browser   BrowserDriver
	caps      CaptureSource
	sysPrompt string
	maxIters  int
	sessions  *SessionStore // 省token 会话层（来自 relay）
	sessionID string

	plannerEnabled bool        // inject an offline plan into the first turn
	plannerFn      PlannerFunc // optional LLM-backed planner override
}

// defaultPlannerEnabled is the package-level default for planner injection, so
// the orchestrator (yami.go AiSetPlanner) can toggle it persistently across
// every agent instance created via NewAgent.
var defaultPlannerEnabled = true

// SetDefaultPlannerEnabled toggles offline plan injection for all future agents.
func SetDefaultPlannerEnabled(on bool) { defaultPlannerEnabled = on }

// NewAgent wires the agent. If browser/caps are nil they are treated as
// unavailable and the matching tools are omitted.
func NewAgent(rl *Relay, browser BrowserDriver, caps CaptureSource) *Agent {
	return &Agent{relay: rl, browser: browser, caps: caps, sysPrompt: SystemPromptBrowserOperator, maxIters: 12, sessions: rl.sessions, plannerEnabled: defaultPlannerEnabled}
}

// SetSystemPrompt overrides the default operator prompt.
func (a *Agent) SetSystemPrompt(p string) { a.sysPrompt = p }

// SetSession attaches a persistent session id so multi-turn agent runs keep
// context (and get compacted via 省token 模式).
func (a *Agent) SetSession(id string) { a.sessionID = id }

// SetPlannerEnabled toggles the offline plan injection. On by default.
func (a *Agent) SetPlannerEnabled(on bool) { a.plannerEnabled = on }

// SetPlanner installs an optional LLM-backed planner; when nil the offline
// heuristic planner is used.
func (a *Agent) SetPlanner(fn PlannerFunc) { a.plannerFn = fn }

// tools returns the OpenAI function schema for the agent. It is generated from
// the single action registry so it stays in sync with the dispatcher and the
// UI action list.
func (a *Agent) tools() []Tool {
	out := make([]Tool, 0, len(actionRegistry))
	for _, d := range actionRegistry {
		if !capabilityOk(d.requires, a.browser != nil, a.caps != nil) {
			continue
		}
		out = append(out, funcTool(d.name, d.description, d.params))
	}
	return out
}

// Run executes the task and returns the final assistant answer.
func (a *Agent) Run(task string) (string, error) {
	msgs := []ChatMessage{}
	if a.sessions != nil && a.sessionID != "" {
		ses := a.sessions.GetOrCreate(a.sessionID)
		if len(ses.Messages) > 0 {
			msgs = append(msgs, ses.Messages...) // resume prior turns
		} else {
			msgs = append(msgs, ChatMessage{Role: "system", Content: a.sysPrompt})
		}
	} else {
		msgs = append(msgs, ChatMessage{Role: "system", Content: a.sysPrompt})
	}
	// Plan → execute → verify: optionally inject an offline plan so the model
	// follows an ordered set of subgoals and verifies after each step.
	taskMsg := task
	if a.plannerEnabled {
		if plan, err := a.planTask(task); err == nil && len(plan.Steps) > 0 {
			taskMsg = task + "\n\n[建议执行计划，请尽量按步骤推进，并在每步后用工具结果校验]\n" + plan.String()
		}
	}
	msgs = append(msgs, ChatMessage{Role: "user", Content: taskMsg})

	for i := 0; i < a.maxIters; i++ {
		req := &ChatCompletionRequest{
			Model:    modelOrDefault(a.relay),
			Messages: msgs,
			Tools:    a.tools(),
			Stream:   false,
		}
		resp, err := a.relay.complete(req)
		if err != nil {
			return "", err
		}
		choice := resp.Choices[0].Message
		if len(choice.ToolCalls) == 0 {
			// no more actions: persist full turn + compact (省token)
			if a.sessions != nil && a.sessionID != "" {
				ses := a.sessions.GetOrCreate(a.sessionID)
				ses.Messages = append(append([]ChatMessage{}, msgs...), choice)
				a.sessions.Compact(ses)
			}
			return choice.Content, nil
		}
		msgs = append(msgs, choice)
		for _, tc := range choice.ToolCalls {
			// 结果校验：每一步执行结果都回传给模型，由其决定是否继续/结束。
			result := a.dispatchToolCall(tc)
			msgs = append(msgs, ChatMessage{Role: "tool", Name: tc.Function.Name, Content: result})
		}
		// 长任务：把工作上下文镜像进会话并在超预算时压缩，避免爆上下文。
		if a.sessions != nil && a.sessionID != "" {
			ses := a.sessions.GetOrCreate(a.sessionID)
			ses.Messages = append([]ChatMessage{}, msgs...)
			a.sessions.Compact(ses) // no-op under budget; compacts otherwise
			msgs = append([]ChatMessage{}, ses.Messages...)
		}
	}
	return "", fmt.Errorf("agent exceeded %d iterations", a.maxIters)
}

// RunWithSession runs a task against a persistent session so multi-turn agent
// runs share context and benefit from 省token compaction. Wires the session
// then delegates to Run — keeping Run's signature unchanged.
func (a *Agent) RunWithSession(sessionID, task string) (string, error) {
	a.SetSession(sessionID)
	return a.Run(task)
}

// planTask resolves a plan via the optional LLM planner, falling back to the
// deterministic offline heuristic.
func (a *Agent) planTask(task string) (Plan, error) {
	return PlanTask(task, a.plannerFn)
}

func modelOrDefault(rl *Relay) string {
	if a := rl.reg.Active(); a != nil && a.Model != "" {
		return a.Model
	}
	return "gpt-4o-mini"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}

func funcTool(name, desc, params string) Tool {
	return Tool{
		Type: "function",
		Function: ToolDef{
			Name:        name,
			Description: desc,
			Parameters:  json.RawMessage(params),
		},
	}
}
