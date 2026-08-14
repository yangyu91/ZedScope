package proxy

import "testing"

// sampleStore builds an in-memory store with three distinct transactions so
// the Search filters can be verified in isolation (no network).
func sampleStore() *Store {
	s := NewStore(100)
	s.Add(&Record{
		ID: 1, Method: "GET", URL: "https://api.example.com/login",
		ReqHeaders:  "Authorization: Bearer x\r\n",
		RespHeaders: "Content-Type: application/json\r\n",
		StatusCode:  200, RespBody: `{"token":"abc123"}`,
	})
	s.Add(&Record{
		ID: 2, Method: "POST", URL: "https://img.example.com/pic",
		ReqBody:     "rawuploadeddata",
		RespHeaders: "Content-Type: image/png\r\n",
		StatusCode:  200, RespBody: "binarybytes",
	})
	s.Add(&Record{
		ID: 3, Method: "GET", URL: "https://api.example.com/health",
		RespHeaders: "Content-Type: text/html\r\n",
		StatusCode:  404, RespBody: "not found",
	})
	return s
}

func TestSearchKeyword(t *testing.T) {
	s := sampleStore()
	if got := s.Search("token", 0, ""); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("keyword 'token' should match record 1, got %v", ids(got))
	}
	// keyword found in request body
	if got := s.Search("rawuploadeddata", 0, ""); len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("keyword in req body should match record 2, got %v", ids(got))
	}
	// keyword found in request header
	if got := s.Search("Bearer", 0, ""); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("keyword in header should match record 1, got %v", ids(got))
	}
	// no match
	if got := s.Search("zzz-nope", 0, ""); len(got) != 0 {
		t.Fatalf("expected 0 matches, got %v", ids(got))
	}
}

func TestSearchStatus(t *testing.T) {
	s := sampleStore()
	got := s.Search("", 404, "")
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("status 404 should match record 3, got %v", ids(got))
	}
	// status + keyword together
	got = s.Search("token", 200, "")
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("status 200 + keyword should match record 1, got %v", ids(got))
	}
}

func TestSearchContentType(t *testing.T) {
	s := sampleStore()
	got := s.Search("", 0, "json")
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("content-type json should match record 1, got %v", ids(got))
	}
	got = s.Search("", 0, "image")
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("content-type image should match record 2, got %v", ids(got))
	}
	got = s.Search("", 0, "html")
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("content-type html should match record 3, got %v", ids(got))
	}
}

func ids(recs []*Record) []int64 {
	var out []int64
	for _, r := range recs {
		out = append(out, r.ID)
	}
	return out
}
