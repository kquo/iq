package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

type SearchParam struct {
	Query string
}

type ClientOption struct {
	Referrer  string
	UserAgent string
	Timeout   time.Duration
}

var defaultClientOption = &ClientOption{
	Referrer:  "https://google.com",
	UserAgent: `Mozilla/5.0 (Windows NT 10.0; WOW64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.5666.197 Safari/537.36`,
	Timeout:   5 * time.Second,
}

func NewClientOption(referrer, userAgent string, timeout time.Duration) *ClientOption {
	if referrer == "" {
		referrer = defaultClientOption.Referrer
	}
	if userAgent == "" {
		userAgent = defaultClientOption.UserAgent
	}

	if timeout == 0 {
		timeout = defaultClientOption.Timeout
	}

	return &ClientOption{
		Referrer:  referrer,
		UserAgent: userAgent,
		Timeout:   timeout,
	}
}

func NewSearchParam(query string) (*SearchParam, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, errors.New("search query is empty")
	}

	return &SearchParam{
		Query: q,
	}, nil
}

func (param *SearchParam) buildURL() (*url.URL, error) {
	u := &url.URL{
		Scheme: "https",
		Host:   "html.duckduckgo.com",
		Path:   "html",
	}
	q := u.Query()
	q.Add("q", param.Query)
	q.Add("s", "0")
	q.Add("dc", "1")
	q.Add("v", "1")
	q.Add("o", "json")
	q.Add("api", "/d.js")
	u.RawQuery = q.Encode()

	return u, nil
}

func buildRequest(param *SearchParam, opt *ClientOption) (*http.Request, error) {
	u, err := param.buildURL()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return req, err
	}

	req.Header.Add("Referrer", opt.Referrer)
	req.Header.Add("User-Agent", opt.UserAgent)
	req.Header.Add("Cookie", "kl=wt-wt")
	req.Header.Add("Content-Type", "x-www-form-urlencoded")

	return req, nil
}

type SearchResult struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

func parse(r io.Reader) (*[]SearchResult, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML document: %w", err)
	}

	var (
		result []SearchResult
		item   SearchResult
	)
	doc.Find(".result").Each(func(i int, s *goquery.Selection) {
		item.Title = s.Find(".result__title a").Text()

		item.Link = extractLink(
			s.Find(".result__url").AttrOr("href", ""),
		)

		item.Snippet = removeHtmlTagsFromText(
			s.Find(".result__snippet").Text(),
		)

		result = append(result, item)
	})

	return &result, nil
}

func removeHtmlTags(node *html.Node, buf *bytes.Buffer) {
	if node.Type == html.TextNode {
		buf.WriteString(node.Data)
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		removeHtmlTags(child, buf)
	}
}

func removeHtmlTagsFromText(text string) string {
	node, err := html.Parse(strings.NewReader(text))
	if err != nil {
		// If it cannot be parsed text as HTML, return the text as is.
		return text
	}

	buf := &bytes.Buffer{}
	removeHtmlTags(node, buf)

	return buf.String()
}

// Extract target URL from href included in search result
// e.g.
//
//	`//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.vim8.org%2Fdownload.php&amp;rut=...`
//	                          ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
//	                     --> `https://www.vim8.org/download.php`
func extractLink(href string) string {
	u, err := url.Parse(fmt.Sprintf("https:%s", strings.TrimSpace(href)))
	if err != nil {
		return ""
	}

	q := u.Query()
	if !q.Has("uddg") {
		return ""
	}

	return q.Get("uddg")
}

// doRequestWithRetry executes an HTTP request, retrying on 202 responses.
// DuckDuckGo occasionally returns 202 (throttling/async) — a brief pause and
// retry is usually sufficient to get a real 200 back.
func doRequestWithRetry(client *http.Client, req *http.Request, opt *ClientOption, maxRetries int) (*http.Response, error) {
	backoff := 500 * time.Millisecond
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Clone the request so it can be retried (body is nil for GET so this is safe)
		clone := req.Clone(req.Context())
		resp, err := client.Do(clone)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		resp.Body.Close()
		if resp.StatusCode == 202 && attempt < maxRetries {
			time.Sleep(backoff)
			backoff *= 2
			continue
		}
		return nil, fmt.Errorf("failed to get a 200 response, status code: %d", resp.StatusCode)
	}
	return nil, fmt.Errorf("max retries exceeded")
}

func SearchWithOption(param *SearchParam, opt *ClientOption, maxResults int) (*[]SearchResult, error) {
	allResults := []SearchResult{}
	pageSize := 10
	pagesNeeded := (maxResults + pageSize - 1) / pageSize
	if maxResults == 0 {
		pagesNeeded = 3 // Default to 3 pages if no limit specified
	}

	client := &http.Client{
		Timeout: opt.Timeout,
	}

	for page := 0; page < pagesNeeded; page++ {
		offset := page * pageSize

		paramWithOffset := &SearchParam{Query: param.Query}

		u, err := paramWithOffset.buildURL()
		if err != nil {
			return nil, err
		}

		q := u.Query()
		q.Set("s", fmt.Sprintf("%d", offset))
		u.RawQuery = q.Encode()

		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}

		req.Header.Add("Referrer", opt.Referrer)
		req.Header.Add("User-Agent", opt.UserAgent)
		req.Header.Add("Cookie", "kl=wt-wt")
		req.Header.Add("Content-Type", "x-www-form-urlencoded")

		resp, err := doRequestWithRetry(client, req, opt, 3)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		pageResults, err := parse(resp.Body)
		if err != nil {
			return nil, err
		}

		if pageResults != nil {
			allResults = append(allResults, *pageResults...)
		}

		// Stop if we got fewer results than a full page (last page)
		if pageResults == nil || len(*pageResults) < pageSize {
			break
		}
	}

	return &allResults, nil
}

func Search(param *SearchParam, maxResults int) (*[]SearchResult, error) {
	return SearchWithOption(param, defaultClientOption, maxResults)
}
