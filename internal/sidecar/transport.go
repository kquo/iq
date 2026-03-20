package sidecar

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"iq/internal/config"
)

// inferTimeout is the deadline for a single non-streaming inference call.
// Local models can be slow; 5 minutes covers even large slow-tier models.
const inferTimeout = 5 * time.Minute

// inferClient is used for non-streaming RawCall requests.
// Stream uses http.DefaultClient because its Timeout would cancel mid-stream.
var inferClient = &http.Client{Timeout: inferTimeout}

// ── OpenAI-compatible types ───────────────────────────────────────────────────

// ChatRequest is an OpenAI-compatible inference request.
type ChatRequest struct {
	Messages          []config.Message `json:"messages"`
	Stream            bool             `json:"stream"`
	MaxTokens         int              `json:"max_tokens,omitempty"`
	RepetitionPenalty float64          `json:"repetition_penalty,omitempty"`
	Temperature       float64          `json:"temperature,omitempty"`
	TopP              *float64         `json:"top_p,omitempty"`
	MinP              *float64         `json:"min_p,omitempty"`
	TopK              *int             `json:"top_k,omitempty"`
	Stop              []string         `json:"stop,omitempty"`
	Seed              *int             `json:"seed,omitempty"`
}

type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// ── Think-block stripping ───────────────────────────────────────────────────

// StripThinkBlocks removes <think>...</think> reasoning blocks emitted by
// models like DeepSeek-R1 before returning the response to the user.
func StripThinkBlocks(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			// Unclosed tag — strip from <think> to end of string.
			s = strings.TrimSpace(s[:start])
			break
		}
		s = s[:start] + s[end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

// ── HTTP transport ────────────────────────────────────────────────────────────

// RawCall sends a ChatRequest to a sidecar and returns the response text.
func RawCall(ctx context.Context, port int, req ChatRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("http://localhost:%d/v1/chat/completions", port)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := inferClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("sidecar at :%d unreachable — run 'iq start': %w", port, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("sidecar at :%d returned %d: %s", port, resp.StatusCode, b)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from sidecar")
	}
	content := result.Choices[0].Message.Content
	return StripThinkBlocks(content), nil
}

// Call sends a non-streaming inference request to a sidecar.
func Call(ctx context.Context, port int, messages []config.Message, maxTokens int, ip config.ResolvedParams) (string, error) {
	req := ChatRequest{
		Messages:          messages,
		Stream:            false,
		MaxTokens:         maxTokens,
		RepetitionPenalty: ip.RepetitionPenalty,
		Temperature:       ip.Temperature,
		TopP:              ip.TopP,
		MinP:              ip.MinP,
		TopK:              ip.TopK,
		Stop:              ip.Stop,
		Seed:              ip.Seed,
	}
	return RawCall(ctx, port, req)
}

// Stream sends a streaming inference request and prints tokens as they arrive.
// Think blocks are suppressed during streaming; the clean result is printed at the end.
func Stream(ctx context.Context, port int, messages []config.Message, ip config.ResolvedParams) (string, error) {
	req := ChatRequest{
		Messages:          messages,
		Stream:            true,
		MaxTokens:         ip.MaxTokens,
		RepetitionPenalty: ip.RepetitionPenalty,
		Temperature:       ip.Temperature,
		TopP:              ip.TopP,
		MinP:              ip.MinP,
		TopK:              ip.TopK,
		Stop:              ip.Stop,
		Seed:              ip.Seed,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("http://localhost:%d/v1/chat/completions", port)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("sidecar at :%d unreachable — run 'iq start': %w", port, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, b)
	}

	// Collect all tokens. If the model uses <think> blocks (e.g. DeepSeek-R1),
	// we suppress further streaming once <think> is detected and print the clean
	// result at the end. For non-thinking models, tokens stream normally.
	var full strings.Builder
	var preThink strings.Builder // tokens streamed before <think> was detected
	hasThink := false

	// Use a large scanner buffer — DeepSeek-R1 think blocks can produce
	// SSE lines well in excess of bufio's default 64KB limit.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			token := choice.Delta.Content
			if token == "" {
				continue
			}
			full.WriteString(token)
			if !hasThink && strings.Contains(full.String(), "<think>") {
				hasThink = true
			}
			// Only stream to stdout if we have not encountered a think block.
			if !hasThink {
				fmt.Print(token)
				preThink.WriteString(token)
			}
		}
	}
	result := StripThinkBlocks(full.String())
	if hasThink {
		// Only print content that wasn't already streamed. Pre-think tokens were
		// already sent to stdout; skip them to avoid printing them twice.
		pre := preThink.String()
		suffix := result
		if strings.HasPrefix(result, pre) {
			suffix = strings.TrimLeft(result[len(pre):], "\n")
		}
		fmt.Print(suffix)
	}
	fmt.Println()
	return strings.TrimSpace(result), scanner.Err()
}
