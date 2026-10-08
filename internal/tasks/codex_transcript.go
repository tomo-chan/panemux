package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const codexMessageType = "message"

// buildCodexTranscriptScript reads a standard Codex rollout using the same
// byte framing and mtime/size/path ordering as the stopped-session collector.
// Only a UUID enters the fixed glob; neither log content nor a caller's path
// selects a command. Task collection and reader selection must agree.
func buildCodexTranscriptScript(sessionID string) (string, error) {
	if !validUUID.MatchString(sessionID) {
		return "", fmt.Errorf("%w: invalid codex session ID", ErrInvalidSummary)
	}
	script, err := buildTranscriptScript(sessionID)
	if err != nil { //coverage:exempt a valid UUID also passes validSessionID
		return "", err
	}
	return strings.Replace(script, `"$HOME"/.claude/projects/*/"$sid.jsonl"`,
		`"$HOME"/.codex/sessions/*/*/*/"rollout-"*-"$sid.jsonl"`, 1), nil
}

// Codex rollouts are an unpublished format. Read only canonical response_item
// messages, never event_msg mirrors, reasoning, calls, results or attachments.
// A message ID deduplicates replayed response items; identical text from
// distinct messages (or messages without IDs) is legitimate conversation.
func buildCodexExcerpt(data transcriptData) (string, bool) {
	seen := map[string]bool{}
	early := parseCodexLogMessages(data.Head, false, !data.Whole, seen)
	recent := early
	if !data.Whole {
		recent = parseCodexLogMessages(data.Tail, true, false, seen)
	}
	return formatExcerpt(data.Whole, early, recent)
}

func parseCodexLogMessages(chunk []byte, dropFirst, dropLast bool, seen map[string]bool) []logMessage {
	rows := bytes.Split(chunk, []byte("\n"))
	if dropFirst {
		rows = rows[1:]
	}
	if dropLast {
		rows = rows[:len(rows)-1]
	}
	var messages []logMessage
	for _, row := range rows {
		var line struct {
			Type    string `json:"type"`
			Payload struct {
				Type    string         `json:"type"`
				ID      string         `json:"id"`
				Role    string         `json:"role"`
				Content []contentBlock `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(row, &line) != nil || line.Type != "response_item" || line.Payload.Type != codexMessageType {
			continue
		}
		p := line.Payload
		if p.Role != logRoleUser && p.Role != logRoleAssistant {
			continue
		}
		if p.ID != "" && seen[p.ID] {
			continue
		}
		var parts []string
		for _, block := range p.Content {
			if (p.Role == logRoleUser && block.Type == "input_text") ||
				(p.Role == logRoleAssistant && block.Type == "output_text") {

				text := strings.TrimSpace(block.Text)
				if p.Role == logRoleUser {
					text = codexConversationText(text)
				}
				if text != "" {
					parts = append(parts, text)
				}
			}
		}
		if len(parts) > 0 {
			messages = append(messages, logMessage{Role: p.Role, Text: strings.Join(parts, "\n")})
			if p.ID != "" {
				seen[p.ID] = true
			}
		}
	}
	return messages
}

// Codex injects global/project AGENTS and environment descriptions as user
// messages. They are context, not the conversation's first request. Strip only
// complete, leading wrappers; ordinary mentions/quotes remain conversation.
// This is separate from the summarizer's own inherited Codex configuration.
func codexConversationText(text string) string {
	if strings.HasPrefix(text, "# AGENTS.md instructions for ") {
		_, after, opened := strings.Cut(text, "<INSTRUCTIONS>")
		_, rest, closed := strings.Cut(after, "</INSTRUCTIONS>")
		if opened && closed {
			text = strings.TrimSpace(rest)
		}
	}
	if strings.HasPrefix(text, "<environment_context>") {
		if _, rest, ok := strings.Cut(text, "</environment_context>"); ok {
			text = strings.TrimSpace(rest)
		}
	}
	return text
}
