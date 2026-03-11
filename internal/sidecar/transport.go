package sidecar

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"iq/internal/config"
)

// ── OpenAI-compatible types ───────────────────────────────────────────────────

// ChatRequest is an OpenAI-compatible inference request.
type ChatRequest struct {
	Messages          []config.Message `json:"messages"`
	Stream            bool             `json:"stream"`
	MaxTokens         int              `json:"max_tokens,omitempty"`
	RepetitionPenalty float64          `json:"repetition_penalty,omitempty"`
	Temperature       float64          `json:"temperature,omitempty"`
	RoutingGrammar    *RouteGrammar    `json:"routing_grammar,omitempty"`
}

// RouteGrammar constrains the first tokens of inference to a tool name.
type RouteGrammar struct {
	ToolNames []string `json:"tool_names"`
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
func RawCall(port int, req ChatRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("http://localhost:%d/v1/chat/completions", port)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("sidecar at :%d unreachable — run 'iq start': %w", port, err)
	}
	defer resp.Body.Close()

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
func Call(port int, messages []config.Message, maxTokens int, ip config.ResolvedParams) (string, error) {
	req := ChatRequest{
		Messages:          messages,
		Stream:            false,
		MaxTokens:         maxTokens,
		RepetitionPenalty: ip.RepetitionPenalty,
		Temperature:       ip.Temperature,
	}
	return RawCall(port, req)
}

// CallWithGrammar sends a non-streaming inference request with a routing grammar.
func CallWithGrammar(port int, messages []config.Message, maxTokens int, grammar *RouteGrammar, ip config.ResolvedParams) (string, error) {
	req := ChatRequest{
		Messages:          messages,
		Stream:            false,
		MaxTokens:         maxTokens,
		RepetitionPenalty: ip.RepetitionPenalty,
		Temperature:       ip.Temperature,
		RoutingGrammar:    grammar,
	}
	return RawCall(port, req)
}

// Stream sends a streaming inference request and prints tokens as they arrive.
// Think blocks are suppressed during streaming; the clean result is printed at the end.
func Stream(port int, messages []config.Message, ip config.ResolvedParams) (string, error) {
	req := ChatRequest{
		Messages:          messages,
		Stream:            true,
		MaxTokens:         ip.MaxTokens,
		RepetitionPenalty: ip.RepetitionPenalty,
		Temperature:       ip.Temperature,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("http://localhost:%d/v1/chat/completions", port)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("sidecar at :%d unreachable — run 'iq start': %w", port, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, b)
	}

	// Collect all tokens. If the model uses <think> blocks (e.g. DeepSeek-R1),
	// we suppress streaming output entirely and print the clean result at the end.
	// For non-thinking models, tokens stream normally as they arrive.
	var full strings.Builder
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
			if strings.Contains(full.String(), "<think>") {
				hasThink = true
			}
			// Only stream to stdout if we have not encountered a think block.
			if !hasThink {
				fmt.Print(token)
			}
		}
	}
	result := StripThinkBlocks(full.String())
	if hasThink {
		// Print the clean result after stripping think blocks.
		fmt.Print(result)
	}
	fmt.Println()
	return strings.TrimSpace(result), scanner.Err()
}
