package ai

import (
	"strings"
	"testing"
)

func TestDecomposeLoginTask(t *testing.T) {
	plan := DecomposeTask("请打开 https://x.test/login 并登录，输入账号密码")
	if len(plan.Steps) < 3 {
		t.Fatalf("login plan too short: %d steps", len(plan.Steps))
	}
	joined := strings.Join([]string{plan.Steps[0].Goal, plan.Steps[1].Goal}, " ")
	if !strings.Contains(joined, "打开") {
		t.Fatalf("expected a navigate step, got: %s", joined)
	}
	// the input/click step should suggest type/click actions
	hasForm := false
	for _, s := range plan.Steps {
		for _, a := range s.Actions {
			if a == "browser_type" || a == "browser_click" {
				hasForm = true
			}
		}
	}
	if !hasForm {
		t.Fatalf("login plan missing form-fill actions: %+v", plan.Steps)
	}
	// every plan ends with a verification/summary step
	if !strings.Contains(plan.Steps[len(plan.Steps)-1].Goal, "校验") {
		t.Fatalf("plan must end with a verification step, got: %+v", plan.Steps)
	}
}

func TestDecomposeTokenTask(t *testing.T) {
	plan := DecomposeTask("分析抓到的请求并提取 token 凭证")
	found := false
	for _, s := range plan.Steps {
		for _, a := range s.Actions {
			if a == "analyze_capture" || a == "copy_token" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("token plan missing capture actions: %+v", plan.Steps)
	}
}

func TestDecomposeTrivialTask(t *testing.T) {
	plan := DecomposeTask("随便看看")
	if len(plan.Steps) != 1 {
		t.Fatalf("trivial task should yield exactly one step, got %d", len(plan.Steps))
	}
	if len(plan.Steps[0].Actions) != len(actionRegistry) {
		t.Fatalf("trivial step should expose all actions, got %d", len(plan.Steps[0].Actions))
	}
}

func TestDecomposeURLTriggersNavigate(t *testing.T) {
	plan := DecomposeTask("navigate to https://example.com and extract")
	if !strings.Contains(plan.Steps[0].Goal, "打开") {
		t.Fatalf("URL task should start with navigate step, got: %+v", plan.Steps[0])
	}
}

func TestDecomposeDeterministic(t *testing.T) {
	a := DecomposeTask("打开页面登录")
	b := DecomposeTask("打开页面登录")
	if len(a.Steps) != len(b.Steps) {
		t.Fatalf("non-deterministic step count: %d vs %d", len(a.Steps), len(b.Steps))
	}
	for i := range a.Steps {
		if a.Steps[i].Goal != b.Steps[i].Goal {
			t.Fatalf("step %d differs: %q vs %q", i, a.Steps[i].Goal, b.Steps[i].Goal)
		}
	}
}

func TestDecomposeIndicesContiguous(t *testing.T) {
	plan := DecomposeTask("打开 https://x.test 登录并提交，再分析 token")
	for i, s := range plan.Steps {
		if s.Index != i+1 {
			t.Fatalf("step index mismatch at %d: %d", i, s.Index)
		}
	}
}

func TestPlanTaskLLMOverride(t *testing.T) {
	called := false
	fn := func(task string) (Plan, error) {
		called = true
		return Plan{Task: task, Steps: []PlanStep{{Goal: "LLM step", Actions: []string{"browser_extract"}}}}, nil
	}
	plan, err := PlanTask("do something", fn)
	if err != nil {
		t.Fatalf("PlanTask llm: %v", err)
	}
	if !called {
		t.Fatal("LLM planner was not invoked")
	}
	if plan.Strategy != "llm" {
		t.Fatalf("strategy should be llm, got %q", plan.Strategy)
	}
	if plan.Steps[0].Index != 1 {
		t.Fatalf("index not normalized: %d", plan.Steps[0].Index)
	}
}

func TestPlanTaskLLMFailureFallsBack(t *testing.T) {
	fn := func(task string) (Plan, error) {
		return Plan{}, errPlanBoom
	}
	_, err := PlanTask("x", fn)
	if err == nil {
		t.Fatal("expected error from failing LLM planner")
	}
}

func TestPlanStringRenders(t *testing.T) {
	plan := DecomposeTask("打开 https://x.test 登录")
	s := plan.String()
	if !strings.Contains(s, "任务：") || !strings.Contains(s, "1.") {
		t.Fatalf("plan.String() malformed:\n%s", s)
	}
}

var errPlanBoom = errorsNew("planner boom")

// errorsNew is a tiny helper to avoid importing errors only for one var.
func errorsNew(s string) error { return &simpleErr{s} }

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }
