// Package yami is the gomobile binding surface.
//
// gomobile only supports a restricted set of types across the language
// boundary, so every exported function takes/returns a string (usually
// JSON). The Android app calls these via the generated AAR.
package yami

import (
	"encoding/json"
	"fmt"

	"yamiua/ai"
	"yamiua/api"
	"yamiua/proxy"
	"yamiua/proxy/proto"
)

var (
	core      *proxy.Proxy
	apiSrv    *api.Server
	aiReg     *ai.Registry
	aiRelay   *ai.Relay
	browserAd *browserAdapter
)

// Start launches the proxy + API in background goroutines.
// listenProxy / listenAPI are "127.0.0.1:port" strings.
func Start(listenProxy, listenAPI string) string {
	if core != nil {
		return "already started"
	}
	core = proxy.New(listenProxy)
	apiSrv = api.NewAPI(core)
	go func() {
		if err := core.Listen(); err != nil {
			// surfaced via logs; app should retry with another port
		}
	}()
	go func() { _ = apiSrv.Listen(listenAPI) }()
	return "ok"
}

// CaPEM returns the root CA certificate (PEM) for the user to install.
//
// NOTE: gomobile lowerFirst-cases the exported name, so `CaPEM` -> Java
// `caPEM`. A fully-uppercase `CAPEM` would become `capem` and break the
// Kotlin call site `Yami.caPEM()` (build error -> no APK). Do NOT rename
// back to CAPEM.
func CaPEM() string {
	if core == nil {
		return ""
	}
	return core.CAPEM()
}

// Captures returns the captured transactions as a JSON array.
func Captures() string {
	if core == nil {
		return "[]"
	}
	b, _ := json.Marshal(core.Store.List())
	return string(b)
}

// Tokens returns all discovered tokens as a JSON array (de-duplicated).
func Tokens() string {
	if core == nil {
		return "[]"
	}
	var toks []proxy.Token
	for _, rec := range core.Store.List() {
		toks = append(toks, rec.Tokens...)
	}
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
	b, _ := json.Marshal(uniq)
	return string(b)
}

// Clear wipes the capture store and spilled body files.
func Clear() string {
	if core != nil {
		core.Clear()
	}
	return "ok"
}

// SetCleanCapture toggles the default capture filter. When enabled (default),
// yami-UA's own machinery (localhost API, AI relay, proxy core) and known
// AI/relay provider hosts are hidden, leaving a clean browsing capture.
func SetCleanCapture(on bool) string {
	if core != nil {
		core.Filter.SetEnabled(on)
	}
	return "ok"
}

// SetBodyDir overrides where large captured bodies are spilled to disk.
func SetBodyDir(dir string) string {
	if core != nil {
		core.SetBodyDir(dir)
	}
	return "ok"
}

// SetMods installs request-rewrite rules from a JSON array of ModifyRule.
func SetMods(jsonStr string) string {
	if core == nil {
		return "not started"
	}
	var rules []proxy.ModifyRule
	if err := json.Unmarshal([]byte(jsonStr), &rules); err != nil {
		return "err:" + err.Error()
	}
	core.Mods.Set(rules)
	return "ok"
}

// ===================== AI relay / agent binding =====================

// AIStart launches the local OpenAI-compatible relay + agent on listenAddr
// (e.g. "127.0.0.1:8910"). It must be called after Start().
func AIStart(listenAddr string) string {
	if aiRelay != nil {
		return "already started"
	}
	aiReg = ai.NewRegistry()
	aiRelay = ai.NewRelay(aiReg)
	browserAd = newBrowserAdapter()
	if core != nil {
		aiRelay.SetCaptureSource(&captureAdapter{store: core.Store})
		// Route AI upstream traffic through yami-UA's own capture proxy so
		// "all AI traffic walks the proxy" and is itself inspectable.
		aiReg.DefaultUpstreamProxy = core.ListenAddr()
	}
	aiRelay.SetBrowser(browserAd)
	go func() { _ = aiRelay.Listen(listenAddr) }()
	return "ok"
}

// AISetProvider registers an upstream provider from JSON (ai.Provider shape).
func AISetProvider(jsonStr string) string {
	if aiReg == nil {
		return "ai not started"
	}
	var p ai.Provider
	if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
		return "err:" + err.Error()
	}
	aiReg.AddProvider(&p)
	// 用密钥 token 时直接开启省 token 模式（deepseek-web 免费桥不开启）。
	if aiRelay != nil {
		aiRelay.ApplyDefaultCompact()
	}
	return "ok"
}

// AISetActive selects the active provider by name.
func AISetActive(name string) string {
	if aiReg == nil {
		return "ai not started"
	}
	if err := aiReg.SetActive(name); err != nil {
		return "err:" + err.Error()
	}
	if aiRelay != nil {
		aiRelay.ApplyDefaultCompact()
	}
	return "ok"
}

// AiSetCompact toggles the 省token 模式 (session context compaction).
func AiSetCompact(on bool) string {
	if aiRelay == nil {
		return "ai not started"
	}
	aiRelay.Sessions().SetCompactEnabled(on)
	return "ok"
}

// AiSetCompactRatio sets compact_ratio (0,1]; lower compacts earlier.
func AiSetCompactRatio(r float64) string {
	if aiRelay == nil {
		return "ai not started"
	}
	aiRelay.Sessions().SetCompactRatio(r)
	return "ok"
}

// AiSessionNew creates a fresh conversation and returns its id.
func AiSessionNew() string {
	if aiRelay == nil {
		return ""
	}
	return aiRelay.Sessions().GetOrCreate("").ID
}

// AiSessionList returns all session ids as a JSON array.
func AiSessionList() string {
	if aiRelay == nil {
		return "[]"
	}
	b, _ := json.Marshal(aiRelay.Sessions().List())
	return string(b)
}

// AiSessionClear drops one session (or all when id == "") and returns its count.
func AiSessionClear(id string) string {
	if aiRelay == nil {
		return "0"
	}
	n := len(aiRelay.Sessions().List())
	aiRelay.Sessions().Clear(id)
	return fmt.Sprintf("%d", n)
}

// AiChatSession runs a chat turn inside a persistent session so multi-turn
// conversations keep context and get compacted (省token 模式).
func AiChatSession(sessionID, prompt string) string {
	if aiRelay == nil {
		return "ai not started"
	}
	out, err := aiRelay.ChatSession(sessionID, prompt)
	if err != nil {
		return "err:" + err.Error()
	}
	return out
}

// AIListProviders returns the provider list as JSON.
func AIListProviders() string {
	if aiReg == nil {
		return "[]"
	}
	b, _ := json.Marshal(aiReg.List())
	return string(b)
}

// AISetUpstreamProxy forces AI backend traffic through a proxy (http/socks5),
// overriding the engine default. Empty string restores the engine default.
func AISetUpstreamProxy(upstream string) string {
	if aiReg == nil {
		return "ai not started"
	}
	aiReg.DefaultUpstreamProxy = upstream
	return "ok"
}

// AIChat asks the assistant a plain question (no tools).
func AIChat(prompt string) string {
	if aiRelay == nil {
		return "ai not started"
	}
	out, err := aiRelay.Ask(ai.SystemPromptDefault, prompt)
	if err != nil {
		return "err:" + err.Error()
	}
	return out
}

// AIAnalyze runs the capture-analyst persona over a captured transaction.
// captureID may be "" to analyze the most recent capture.
func AIAnalyze(captureID, prompt string) string {
	if aiRelay == nil {
		return "ai not started"
	}
	ctx := ""
	if core != nil && captureID != "" {
		for _, r := range core.Store.List() {
			if fmt.Sprintf("%d", r.ID) == captureID {
				ctx = renderRecord(r)
			}
		}
	}
	req := prompt
	if ctx != "" {
		req = "下面是抓到的请求：\n" + ctx + "\n\n用户问题：" + prompt
	}
	out, err := aiRelay.Ask(ai.SystemPromptCaptureAnalyst, req)
	if err != nil {
		return "err:" + err.Error()
	}
	return out
}

// AIAgentRun runs the browser-operator agent loop over a task. The agent may
// drive the in-app browser (via the Android WebView JS bridge) without any
// Android permission.
func AIAgentRun(task string) string {
	if aiRelay == nil {
		return "ai not started"
	}
	agent := ai.NewAgent(aiRelay, browserAd, aiRelay.CaptureSource())
	out, err := agent.Run(task)
	if err != nil {
		return "err:" + err.Error()
	}
	return out
}

// AiAgentRunSession runs the agent loop inside a persistent session so
// multi-turn automation keeps context and gets compacted (省token 模式).
func AiAgentRunSession(sessionID, task string) string {
	if aiRelay == nil {
		return "ai not started"
	}
	agent := ai.NewAgent(aiRelay, browserAd, aiRelay.CaptureSource())
	agent.SetSession(sessionID)
	out, err := agent.Run(task)
	if err != nil {
		return "err:" + err.Error()
	}
	return out
}

// ===================== 抓包增强（ProxyPin 差距补全） =====================

// AiSetRules installs the capture-enhancement rule set (domain filter / block
// / map / AES-decrypt) from a JSON RulesConfig.
func AiSetRules(jsonStr string) string {
	if core == nil {
		return "not started"
	}
	var cfg proxy.RulesConfig
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		return "err:" + err.Error()
	}
	if err := core.SetRules(cfg); err != nil {
		return "err:" + err.Error()
	}
	return "ok"
}

// AiGetRules returns the active capture-enhancement rule set as JSON.
func AiGetRules() string {
	if core == nil {
		return "{}"
	}
	b, _ := json.Marshal(core.GetRules())
	return string(b)
}

// AiExportHAR exports all captures as a standard HAR JSON document.
func AiExportHAR() string {
	if core == nil {
		return ""
	}
	b, err := proxy.ExportHAR(core.Store)
	if err != nil {
		return "err:" + err.Error()
	}
	return string(b)
}

// AiSearchCaptures searches captures by keyword / status code / content type.
// q is JSON: {"keyword":"","status":0,"content_type":""} (0 = any status).
func AiSearchCaptures(jsonStr string) string {
	if core == nil {
		return "[]"
	}
	var q struct {
		Keyword      string `json:"keyword"`
		Status       int    `json:"status"`
		ContentType  string `json:"content_type"`
	}
	_ = json.Unmarshal([]byte(jsonStr), &q)
	recs := core.Store.Search(q.Keyword, q.Status, q.ContentType)
	b, _ := json.Marshal(recs)
	return string(b)
}

// ===================== 协议 / 会话 / Agent 增强 =====================

// AiProviderHealth returns provider health + active flag as JSON.
func AiProviderHealth() string {
	if aiReg == nil {
		return "[]"
	}
	type ph struct {
		Name    string `json:"name"`
		Healthy bool   `json:"healthy"`
		Active  bool   `json:"active"`
	}
	active := aiReg.Active()
	out := make([]ph, 0, len(aiReg.List()))
	for _, p := range aiReg.List() {
		a := active != nil && active.Name == p.Name
		out = append(out, ph{p.Name, p.Healthy, a})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// AiSessionExport returns one session (or "" when ai not started) as JSON.
func AiSessionExport(id string) string {
	if aiRelay == nil {
		return ""
	}
	ses := aiRelay.Sessions().GetOrCreate(id)
	b, err := ses.ExportJSON()
	if err != nil {
		return "err:" + err.Error()
	}
	return string(b)
}

// AiSessionImport loads a session JSON previously produced by AiSessionExport.
func AiSessionImport(id, jsonStr string) string {
	if aiRelay == nil {
		return "ai not started"
	}
	ses := aiRelay.Sessions().GetOrCreate(id)
	if err := ses.ImportJSON([]byte(jsonStr)); err != nil {
		return "err:" + err.Error()
	}
	return "ok"
}

// AiProtoParse parses a vmess/vless/trojan/ss/socks5 share link into an xray
// outbound map (JSON). Errors are prefixed with "err:".
func AiProtoParse(link string) string {
	cfg, err := proto.Parse(link)
	if err != nil {
		return "err:" + err.Error()
	}
	b, _ := json.Marshal(cfg.ToOutboundMap())
	return string(b)
}

// AiAgentActions returns the available browser/agent tool schemas as JSON.
func AiAgentActions() string {
	if aiRelay == nil {
		return "[]"
	}
	b, _ := json.Marshal(ai.AvailableActions(true, true))
	return string(b)
}

// AiSetPlanner toggles the offline task planner used by the agent loop.
func AiSetPlanner(on bool) string {
	if aiRelay == nil {
		return "ai not started"
	}
	ai.SetDefaultPlannerEnabled(on)
	return "ok"
}

// ===================== SSH session type =====================
//
// "ssh" is a first-class session type alongside the AI chat session: open a
// remote shell and run commands, with combined output streamed back. Backed
// by golang.org/x/crypto/ssh.

var sshMgr = ai.NewSshManager()

// AiSshConnect opens an SSH session. host is "host:port"; authType is
// "password" | "key"; secret is the password or PEM private key. Returns the
// session id, or an "err:"-prefixed message.
func AiSshConnect(host, user, authType, secret string) string {
	s, err := sshMgr.Connect(ai.SshAuth{Host: host, User: user, AuthType: authType, Secret: secret})
	if err != nil {
		return "err:" + err.Error()
	}
	return s.ID
}

// AiSshExec runs a command on an SSH session and returns combined output.
func AiSshExec(id, cmd string) string {
	s, ok := sshMgr.Get(id)
	if !ok {
		return "err: no such ssh session: " + id
	}
	out, err := s.Exec(cmd)
	if err != nil {
		return "err:" + err.Error()
	}
	return out
}

// AiSshList returns active SSH session ids as a JSON array.
func AiSshList() string {
	b, _ := json.Marshal(sshMgr.List())
	return string(b)
}

// AiSshClose terminates an SSH session. Returns "1" on success, "0" otherwise.
func AiSshClose(id string) string {
	if sshMgr.Close(id) {
		return "1"
	}
	return "0"
}

// AIBrowserPending returns the next pending browser command as JSON, or "".
// The Android layer should poll this on a background thread and execute the
// command via the WebView, then call AIBrowserComplete.
func AIBrowserPending() string { return pendingBrowserJSON() }

// AIBrowserComplete posts the result of a browser command.
func AIBrowserComplete(id, result string) string {
	if browserAd == nil {
		return "no browser"
	}
	browserAd.Complete(id, result)
	return "ok"
}

func renderRecord(r *proxy.Record) string {
	return "METHOD=" + r.Method + " URL=" + r.URL +
		"\nREQ_HEADERS=" + r.ReqHeaders +
		"\nREQ_BODY=" + r.ReqBody +
		"\nRESP_HEADERS=" + r.RespHeaders +
		"\nRESP_BODY=" + truncate(r.RespBody, 4000)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}
