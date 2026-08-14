package proxy

import "testing"

func TestExtractTokens(t *testing.T) {
	rec := &Record{
		URL:   "https://api.example.com/login",
		ReqHeaders: "Authorization: Bearer eyJabc.def.ghi\r\nX-Api-Token: secret123\r\n",
		RespHeaders: "Set-Cookie: sessid=abc123; Path=/\r\nSet-Cookie: theme=dark\r\n",
		RespBody:    `{"access_token":"tok_xyz","refresh_token":"rt_999","user":"bob"}`,
	}
	toks := ExtractTokens(rec)

	want := map[string]string{
		"Authorization": "Bearer eyJabc.def.ghi",
		"Header:X-Api-Token": "secret123",
		"Cookie:sessid": "abc123",
		"JWT": "eyJabc.def.ghi",
		"body:access_token": "tok_xyz",
		"body:refresh_token": "rt_999",
	}
	got := map[string]string{}
	for _, tk := range toks {
		key := tk.Key
		if tk.Key == "JWT" {
			key = "JWT"
		} else if tk.Source == "body" {
			key = "body:" + tk.Key
		} else if tk.Source == "header" && tk.Key != "Authorization" {
			key = "Header:" + tk.Key
		}
		got[key] = tk.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("token %q = %q, want %q (got all: %v)", k, got[k], v, got)
		}
	}
}

func TestDetectLogin(t *testing.T) {
	if !detectLogin("POST", "https://x.com/api/auth/login", "", "Set-Cookie: sid=1\r\n", "") {
		t.Error("login path + session cookie should be detected")
	}
	if detectLogin("GET", "https://x.com/static/app.js", "", "Content-Type: application/javascript\r\n", "") {
		t.Error("static asset should not be login")
	}
}
