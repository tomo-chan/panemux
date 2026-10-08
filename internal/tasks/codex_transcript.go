package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	script = strings.Replace(script, `"$HOME"/.claude/projects/*/"$sid.jsonl"`,
		`"$HOME"/.codex/sessions/*/*/*/"rollout-"*-"$sid.jsonl"`, 1)
	return strings.ReplaceAll(script, "sort -rn", "sort -k1,1nr -k2,2nr -k3r"), nil
}

func buildCodexTranscriptScriptForLog(sessionID string, log LogVersion) (string, error) {
	script, err := buildCodexTranscriptScript(sessionID)
	if err != nil {
		return "", err
	}
	if log.File == "" {
		return script, nil
	}
	id, ok := rolloutSessionID(log.File)
	if !ok || id != sessionID || log.ModTime < 0 || log.Size < 0 {
		return "", ErrInvalidSummary
	}
	script = strings.Replace(script, `"rollout-"*-"$sid.jsonl"`, "'"+log.File+"'", 1)
	version := strconv.FormatInt(log.ModTime, 10) + " " + strconv.FormatInt(log.Size, 10)
	check := `[ "$(mtime "$best" 2>/dev/null)" = '` + version + `' ] || { echo '::panemux-transcript changed'; exit 0; }
`
	script = strings.Replace(script, `size=$(wc -c`, check+`size=$(wc -c`, 1)
	script = strings.Replace(script, "echo '::end'", check+"echo '::end'", 1)
	return script, nil
}

func parseCodexTranscriptOutput(out []byte, sessionID string) (transcriptData, error) {
	data, err := parseTranscriptOutput(out)
	if err != nil {
		return transcriptData{}, errors.New("codex conversation log could not be read")
	}
	first, _, _ := bytes.Cut(data.Head, []byte("\n"))
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	if json.Unmarshal(first, &meta) != nil || meta.Type != "session_meta" || meta.Payload.ID != sessionID {
		return transcriptData{}, errors.New("codex conversation log has an invalid session header")
	}
	return data, nil
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
