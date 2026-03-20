package main

import (
	"fmt"
	"strings"

	"iq/internal/config"
	"iq/internal/kb"
)

// contextTrim summarises what was dropped during context budget trimming.
type contextTrim struct {
	KBChunksDropped     int
	SessionTurnsDropped int
}

// estimateTokens estimates total token count for a message slice using the
// chars/4 heuristic (reasonable approximation for English text).
func estimateTokens(msgs []config.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content)
	}
	return total / 4
}

// dropOldestSessionTurns removes n user+assistant pairs from the oldest end of
// messages, preserving messages[0] (system prompt) and the final message
// (current user input). Returns the updated slice and actual pairs dropped.
func dropOldestSessionTurns(msgs []config.Message, n int) ([]config.Message, int) {
	dropped := 0
	for range n {
		// Need at least: system + 1 old pair + current user = 4 messages.
		if len(msgs) < 4 {
			break
		}
		// Drop messages[1] and messages[2] (oldest user+assistant pair).
		msgs = append(msgs[:1:1], msgs[3:]...)
		dropped++
	}
	return msgs, dropped
}

// rebuildKBUserMessage replaces the content of the last message in msgs with
// a rebuilt KB context (from kbResults) followed by the user input. When
// kbResults is empty the last message contains only the user input.
func rebuildKBUserMessage(msgs []config.Message, kbResults []kb.Result, input string) []config.Message {
	if len(msgs) == 0 {
		return msgs
	}
	last := len(msgs) - 1
	if len(kbResults) == 0 {
		msgs[last].Content = input
	} else {
		msgs[last].Content = kb.Context(kbResults) + "\n\n" + input
	}
	return msgs
}

// trimToContextBudget trims messages to fit within (contextWindow - maxTokens)
// input tokens. KB chunks are dropped first (from the end); then session turns
// are dropped oldest-first. The system prompt and current user input are never
// removed. kbResults and input are required to rebuild the KB user message when
// chunks are dropped; pass nil kbResults when not in KB mode.
func trimToContextBudget(
	messages []config.Message,
	kbResults []kb.Result,
	input string,
	contextWindow, maxTokens int,
) ([]config.Message, []kb.Result, contextTrim) {
	budget := contextWindow - maxTokens
	if budget <= 0 || estimateTokens(messages) <= budget {
		return messages, kbResults, contextTrim{}
	}

	var trim contextTrim

	// Phase 1: drop KB chunks from the end until within budget.
	for len(kbResults) > 0 && estimateTokens(messages) > budget {
		kbResults = kbResults[:len(kbResults)-1]
		trim.KBChunksDropped++
		messages = rebuildKBUserMessage(messages, kbResults, input)
	}

	// Phase 2: drop oldest session turns until within budget.
	maxDrops := (len(messages) - 2) / 2
	for i := 0; i < maxDrops && estimateTokens(messages) > budget; i++ {
		var dropped int
		messages, dropped = dropOldestSessionTurns(messages, 1)
		trim.SessionTurnsDropped += dropped
		if dropped == 0 {
			break
		}
	}

	return messages, kbResults, trim
}

// trimWarning returns a one-line warning string describing what was trimmed, or
// an empty string when nothing was dropped.
func trimWarning(ctrim contextTrim, contextWindow int) string {
	var parts []string
	if ctrim.KBChunksDropped > 0 {
		parts = append(parts, fmt.Sprintf("%d KB chunk(s)", ctrim.KBChunksDropped))
	}
	if ctrim.SessionTurnsDropped > 0 {
		parts = append(parts, fmt.Sprintf("%d session turn(s)", ctrim.SessionTurnsDropped))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("[context trimmed: %s to fit %d-token window]",
		strings.Join(parts, ", "), contextWindow)
}
