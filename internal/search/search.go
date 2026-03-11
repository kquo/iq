package search

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// ── CSS selectors for DDG HTML parsing ───────────────────────────────────────

const (
	selectorResult        = ".result"
	selectorResultTitle   = ".result__title a"
	selectorResultURL     = ".result__url"
	selectorResultSnippet = ".result__snippet"
)

// ── Client ──────────────────────────────────────────────────────────────────

const ddgMinInterval = 1 * time.Second

// Client carries search configuration and per-instance rate-limit state.
// It replaces the former package-level braveAPIKey variable and global mutex,
// eliminating the latent data race on concurrent access.
type Client struct {
	BraveAPIKey string
	Option      *ClientOption
	rateMu      sync.Mutex
	lastRequest time.Time
}

// NewClient returns a Client with the given Brave API key and default options.
func NewClient(braveAPIKey string) *Client {
	return &Client{
		BraveAPIKey: braveAPIKey,
		Option:      defaultClientOption,
	}
}

// throttle enforces a minimum interval between DDG HTTP requests.
func (c *Client) throttle() {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	if elapsed := time.Since(c.lastRequest); elapsed < ddgMinInterval {
		time.Sleep(ddgMinInterval - elapsed)
	}
	c.lastRequest = time.Now()
}

// ── Brave Search fallback ────────────────────────────────────────────────────

const braveSearchURL = "https://api.search.brave.com/res/v1/web/search"

type braveResponse struct {
	Web struct {
		Results []braveResult `json:"results"`
	} `json:"web"`
}

type braveResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// searchBrave queries the Brave Search API and returns results.
func (c *Client) searchBrave(query string, maxResults int, timeout time.Duration) (*[]Result, error) {
	if c.BraveAPIKey == "" {
		return nil, errors.New("brave_api_key not configured")
	}

	u, _ := url.Parse(braveSearchURL)
	q := u.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", maxResults))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Subscription-Token", c.BraveAPIKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave search returned status %d", resp.StatusCode)
	}

	var br braveResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return nil, fmt.Errorf("brave search JSON decode failed: %w", err)
	}

	results := make([]Result, 0, len(br.Web.Results))
	for _, r := range br.Web.Results {
		results = append(results, Result{
			Title:   r.Title,
			Link:    r.URL,
			Snippet: r.Description,
		})
	}
	return &results, nil
}

// ── DDG types and helpers ────────────────────────────────────────────────────

type Param struct {
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

func NewParam(query string) (*Param, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, errors.New("search query is empty")
	}

	return &Param{
		Query: q,
	}, nil
}

func (param *Param) buildURL() (*url.URL, error) {
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

func buildRequest(param *Param, opt *ClientOption) (*http.Request, error) {
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

type Result struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

func parse(r io.Reader) (*[]Result, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML document: %w", err)
	}

	var (
		result []Result
		item   Result
	)
	doc.Find(selectorResult).Each(func(i int, s *goquery.Selection) {
		item.Title = s.Find(selectorResultTitle).Text()

		item.Link = extractLink(
			s.Find(selectorResultURL).AttrOr("href", ""),
		)

		item.Snippet = removeHtmlTagsFromText(
			s.Find(selectorResultSnippet).Text(),
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
func (c *Client) doRequestWithRetry(client *http.Client, req *http.Request, maxRetries int) (*http.Response, error) {
	backoff := 500 * time.Millisecond
	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.throttle()
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

// ── Public API ───────────────────────────────────────────────────────────────

// searchDDG performs a DuckDuckGo HTML search (the primary path).
func (c *Client) searchDDG(param *Param, opt *ClientOption, maxResults int) (*[]Result, error) {
	allResults := []Result{}
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

		paramWithOffset := &Param{Query: param.Query}

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

		resp, err := c.doRequestWithRetry(client, req, 3)
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

// SearchWithOption queries DDG, falling back to Brave Search if configured.
func (c *Client) SearchWithOption(param *Param, opt *ClientOption, maxResults int) (*[]Result, error) {
	results, ddgErr := c.searchDDG(param, opt, maxResults)
	if ddgErr == nil {
		return results, nil
	}
	// DDG failed — try Brave fallback if configured.
	if c.BraveAPIKey != "" {
		braveResults, braveErr := c.searchBrave(param.Query, maxResults, opt.Timeout)
		if braveErr == nil {
			return braveResults, nil
		}
		return nil, fmt.Errorf("%w (brave fallback also failed: %v)", ddgErr, braveErr)
	}
	return nil, ddgErr
}

// Search queries DDG with default options, falling back to Brave if configured.
func (c *Client) Search(param *Param, maxResults int) (*[]Result, error) {
	opt := c.Option
	if opt == nil {
		opt = defaultClientOption
	}
	return c.SearchWithOption(param, opt, maxResults)
}

// ── Convenience free functions (backward-compatible, use default client) ─────

var defaultClient = &Client{Option: defaultClientOption}

// Search queries DDG using a default client (no Brave fallback).
func Search(param *Param, maxResults int) (*[]Result, error) {
	return defaultClient.Search(param, maxResults)
}

// SearchWithOption queries DDG with custom options using a default client.
func SearchWithOption(param *Param, opt *ClientOption, maxResults int) (*[]Result, error) {
	return defaultClient.SearchWithOption(param, opt, maxResults)
}
