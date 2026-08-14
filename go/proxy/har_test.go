package proxy

import (
	"encoding/json"
	"testing"
	"time"
)

func TestExportHAR(t *testing.T) {
	s := NewStore(10)
	s.Add(&Record{
		ID: 1, Method: "GET", URL: "https://api.example.com/login", Host: "api.example.com",
		ReqHeaders:  "Authorization: Bearer x\r\n",
		RespHeaders: "Content-Type: application/json\r\n",
		StatusCode:  200, RespBody: `{"token":"abc"}`, Time: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	})

	out, err := ExportHAR(s)
	if err != nil {
		t.Fatalf("ExportHAR: %v", err)
	}

	var doc struct {
		Log struct {
			Version string `json:"version"`
			Creator struct {
				Name string `json:"name"`
			} `json:"creator"`
			Entries []struct {
				StartedDateTime string `json:"startedDateTime"`
				Request         struct {
					Method string `json:"method"`
					URL    string `json:"url"`
				} `json:"request"`
				Response struct {
					Status     int    `json:"status"`
					StatusText string `json:"statusText"`
					Content    struct {
						MimeType string `json:"mimeType"`
						Text     string `json:"text"`
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("HAR is not valid JSON: %v\n%s", err, out)
	}

	if doc.Log.Version != "1.2" {
		t.Errorf("expected HAR version 1.2, got %q", doc.Log.Version)
	}
	if doc.Log.Creator.Name != "yami-UA" {
		t.Errorf("unexpected creator: %q", doc.Log.Creator.Name)
	}
	if len(doc.Log.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(doc.Log.Entries))
	}
	e := doc.Log.Entries[0]
	if e.Request.Method != "GET" || e.Request.URL != "https://api.example.com/login" {
		t.Errorf("request mismatch: %+v", e.Request)
	}
	if e.Response.Status != 200 || e.Response.StatusText != "OK" {
		t.Errorf("response mismatch: status=%d text=%q", e.Response.Status, e.Response.StatusText)
	}
	if e.Response.Content.MimeType != "application/json" {
		t.Errorf("mime type mismatch: %q", e.Response.Content.MimeType)
	}
	if e.Response.Content.Text != `{"token":"abc"}` {
		t.Errorf("body mismatch: %q", e.Response.Content.Text)
	}
	if e.StartedDateTime == "" {
		t.Error("startedDateTime should be populated")
	}
}

func TestExportHAREmpty(t *testing.T) {
	s := NewStore(10)
	out, err := ExportHAR(s)
	if err != nil {
		t.Fatalf("ExportHAR empty: %v", err)
	}
	var doc struct {
		Log struct {
			Entries []json.RawMessage `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(doc.Log.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(doc.Log.Entries))
	}
}
