package notification

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Budget JSON-escaped bytes, leaving room for headers, robot keywords,
// signatures and platform envelopes within an 18 KiB request body.
const messageTextBudget = 12000
const maxPayloadBytes = 18 * 1024

// SplitMessage preserves every character and prefers paragraph/line boundaries.
// Each part is a separate Markdown message; no tables or code fences are needed
// by the portfolio formatter, so a continuation remains readable on both bots.
func SplitMessage(message Message) []Message {
	remaining := message.Text
	parts := []Message{}
	for len(remaining) > 0 {
		size, end, lastBreak := 0, 0, 0
		for index, r := range remaining {
			encoded, _ := json.Marshal(string(r))
			cost := len(encoded) - 2
			if size+cost > messageTextBudget {
				break
			}
			size += cost
			_, width := utf8.DecodeRuneInString(remaining[index:])
			end = index + width
			if r == '\n' {
				lastBreak = end
			}
		}
		if end < len(remaining) && lastBreak > end/2 {
			end = lastBreak
		}
		parts = append(parts, Message{Title: message.Title, Text: remaining[:end]})
		remaining = remaining[end:]
	}
	if len(parts) == 0 {
		return []Message{message}
	}
	if len(parts) > 1 {
		for i := range parts {
			parts[i].Title = fmt.Sprintf("%s（%d/%d）", strings.TrimSpace(clip(message.Title, 70)), i+1, len(parts))
		}
	}
	return parts
}
