package ai

import (
	"fmt"
	"strings"
)

// Plan is an ordered decomposition of a task into subgoals the agent should
// pursue step by step, verifying the outcome after each step before deciding
// whether to continue or finish.
type Plan struct {
	Task     string     `json:"task"`
	Steps    []PlanStep `json:"steps"`
	Strategy string     `json:"strategy"` // "heuristic" | "llm"
}

// PlanStep is one ordered subgoal plus the candidate actions that can satisfy
// it. The agent (LLM) still decides which action to call at runtime.
type PlanStep struct {
	Index   int      `json:"index"`
	Goal    string   `json:"goal"`
	Actions []string `json:"actions"`
}

// DecomposeTask is a deterministic, offline planner: it turns a free-form task
// into an ordered plan using simple keyword/URL heuristics. It is kept pure
// (no LLM, no I/O) so it is fully unit-testable and cheap enough to run on
// every agent turn.
func DecomposeTask(task string) Plan {
	task = strings.TrimSpace(task)
	lower := strings.ToLower(task)
	var steps []PlanStep

	if hasURL(task) || containsAny(lower,
		"打开", "访问", "navigate", "go to", "open", "进入", "跳到") {
		steps = append(steps, PlanStep{
			Goal:    "打开并读取目标页面",
			Actions: []string{"browser_navigate", "browser_extract"},
		})
	}
	if containsAny(lower,
		"登录", "login", "登陆", "登入", "填", "输入", "表单", "type", "fill", "submit", "提交") {
		steps = append(steps, PlanStep{
			Goal:    "在页面上完成输入/点击（填表、登录或提交）",
			Actions: []string{"browser_type", "browser_click", "browser_extract"},
		})
	}
	if containsAny(lower,
		"抓包", "token", "凭证", "鉴权", "auth", "capture", "cookie", "分析请求", "request", "请求") {
		steps = append(steps, PlanStep{
			Goal:    "分析抓到的流量并提取/核对凭证",
			Actions: []string{"analyze_capture", "copy_token"},
		})
	}

	if len(steps) == 0 {
		// Trivial task: a single generic step over all available actions.
		return Plan{Task: task, Steps: []PlanStep{{
			Index:   1,
			Goal:    "理解任务并直接执行",
			Actions: availableActionNames(),
		}}, Strategy: "heuristic"}
	}

	// Verification / summary tail: every non-trivial plan ends by checking
	// results before the model decides to continue or finish.
	steps = append(steps, PlanStep{
		Goal:    "校验当前结果并向用户总结，决定是否继续或结束",
		Actions: []string{"browser_extract"},
	})
	for i := range steps {
		steps[i].Index = i + 1
	}
	return Plan{Task: task, Steps: steps, Strategy: "heuristic"}
}

// PlannerFunc is an optional LLM-backed planner. When set on the agent,
// PlanTask prefers it over the offline heuristic.
type PlannerFunc func(task string) (Plan, error)

// PlanTask decomposes a task. If plannerFn is non-nil it is used (e.g. an
// LLM-backed planner); otherwise the offline heuristic runs. The returned plan
// always has 1-based, contiguous step indices.
func PlanTask(task string, plannerFn PlannerFunc) (Plan, error) {
	if plannerFn != nil {
		p, err := plannerFn(task)
		if err != nil {
			return Plan{}, err
		}
		if len(p.Steps) == 0 {
			return Plan{}, fmt.Errorf("planner returned an empty plan")
		}
		for i := range p.Steps {
			p.Steps[i].Index = i + 1
		}
		p.Strategy = "llm"
		return p, nil
	}
	return DecomposeTask(task), nil
}

// String renders the plan for injection into the agent's prompt.
func (p Plan) String() string {
	if len(p.Steps) == 0 {
		return "(empty plan)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "任务：%s\n", p.Task)
	for _, s := range p.Steps {
		fmt.Fprintf(&b, "%d. %s  [建议动作: %s]\n", s.Index, s.Goal, strings.Join(s.Actions, ", "))
	}
	return b.String()
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func hasURL(s string) bool {
	return strings.Contains(s, "http://") || strings.Contains(s, "https://")
}
