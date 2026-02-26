package main

import (
	"strings"
	"testing"
	"time"
)

var param = &SearchParam{
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

func TestSearch(t *testing.T) {
	var (
		err     error
		results *[]SearchResult
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
