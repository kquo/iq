package search

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

var param = &Param{
	Query: "golang",
}

func TestBuildRequest(t *testing.T) {
	req, err := buildRequest(param, defaultClientOption)
	if err != nil {
		t.Fatal(err)
	}

	url := req.URL.String()
	if url != `https://html.duckduckgo.com/html?api=%2Fd.js&dc=1&o=json&q=golang&s=0&v=1` {
		t.Fatal(url)
	}
}

func TestParseFixture(t *testing.T) {
	f, err := os.Open("testdata/ddg.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	results, err := parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if results == nil || len(*results) != 3 {
		t.Fatalf("expected 3 results, got %v", results)
	}

	r := (*results)[0]
	if r.Title != "Go Programming Language" {
		t.Errorf("title = %q", r.Title)
	}
	if r.Link != "https://go.dev/" {
		t.Errorf("link = %q", r.Link)
	}
	if !strings.Contains(r.Snippet, "open source") {
		t.Errorf("snippet = %q", r.Snippet)
	}

	r2 := (*results)[2]
	if r2.Link != "https://pkg.go.dev/" {
		t.Errorf("result[2] link = %q", r2.Link)
	}
}

func TestBraveResponseParse(t *testing.T) {
	raw := `{
		"web": {
			"results": [
				{"title": "Go Dev", "url": "https://go.dev/", "description": "The Go website."},
				{"title": "Pkg Go", "url": "https://pkg.go.dev/", "description": "Go packages."}
			]
		}
	}`
	var br braveResponse
	if err := json.Unmarshal([]byte(raw), &br); err != nil {
		t.Fatal(err)
	}
	if len(br.Web.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(br.Web.Results))
	}
	if br.Web.Results[0].Title != "Go Dev" {
		t.Errorf("title = %q", br.Web.Results[0].Title)
	}
	if br.Web.Results[1].URL != "https://pkg.go.dev/" {
		t.Errorf("url = %q", br.Web.Results[1].URL)
	}
}

func TestSearch(t *testing.T) {
	var (
		err     error
		results *[]Result
		backoff = 500 * time.Millisecond
		retries = 3
	)
	for i := range retries {
		results, err = Search(param, 10)
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "202") {
			if i < retries-1 {
				t.Logf("attempt %d/%d: DDG returned 202, retrying in %s...", i+1, retries, backoff)
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
			t.Skipf("skipping: DDG unavailable from this environment (202 after %d attempts)", retries)
		}
		// Non-202 error — fail immediately
		t.Fatal(err)
	}
	if results == nil {
		t.Fatal("expected results, got nil")
	}
}
