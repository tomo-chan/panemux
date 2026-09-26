package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Reading a task's conversation log for its summary (issue #258).
//
// Claude Code's conversation log (~/.claude/projects/*/<session>.jsonl) is
// not a published format, and the Agent Board's rule is not to make a private
// log format part of a contract. So the log is read as little as possible:
// each line is a JSON object, and only the text of the user's and the
// assistant's messages (`type` "user"/"assistant", `message.content` as a
// string or as "text" blocks) is kept. Tool calls, tool results, thinking and
// attachments — which carry file contents, command output and credentials —
// are never read. A log in which nothing is understood is not summarized: it
// is reported as unreadable, never passed on as raw text. See
// docs/DECISIONLOG.md, "Stage 3: summaries".

// ErrInvalidSummary is a summary request refused before anything ran.
var ErrInvalidSummary = errors.New("invalid task summary request")

// ErrNoTranscript is a log the host no longer has.
var ErrNoTranscript = errors.New("the conversation log was not found on the host")

const (
	// transcriptHeadBytes and transcriptTailBytes are how much of a log the
	// host sends: its start, where the first instruction is, and its end.
	// Tool results make up most of a log's bytes, so the text of the
	// excerpt below is found in far more of it than the excerpt keeps.
	transcriptHeadBytes = 256 << 10
	transcriptTailBytes = 2 << 20
	// excerptFirstBytes bounds the first instruction in the excerpt,
	// excerptMessageBytes each recent message, and excerptRecentBytes all
	// recent messages together.
	excerptFirstBytes   = 4 << 10
	excerptMessageBytes = 2 << 10
	excerptRecentBytes  = 24 << 10

	transcriptHeader     = "::panemux-transcript "
	transcriptVersion    = "v1 "
	transcriptNone       = "none"
	transcriptEnd        = "\n::end"
	excerptFirstHeading  = "The session's first instruction:"
	excerptRecentHeading = "The session's most recent messages, oldest first:"
)

// transcriptScriptTemplate prints one conversation log: a header with the
// log's size, then the whole log when it is at most the head and tail
// together, else its first transcriptHeadBytes and its last
// transcriptTailBytes, then "::end". A missing log prints
// "::panemux-transcript none". It is a fixed script run with `sh -s`, like
// collectScript; the only value in it is a session ID that passed
// validSessionID and is single-quoted.
const transcriptScriptTemplate = `set -u
LC_ALL=C
export LC_ALL
sid='{{SESSION_ID}}'
for p in "$HOME"/.claude/projects/*/"$sid.jsonl"; do
	[ -f "$p" ] || continue
	size=$(wc -c <"$p" | tr -d ' ')
	echo "::panemux-transcript v1 $size"
	if [ "$size" -le {{WHOLE}} ]; then
		head -c "$size" "$p"
	else
		head -c {{HEAD}} "$p"
		tail -c {{TAIL}} "$p"
	fi
	echo
	echo '::end'
	exit 0
done
echo '::panemux-transcript none'
exit 0
`

func buildTranscriptScript(sessionID string) (string, error) {
	if !validSessionID.MatchString(sessionID) {
		return "", fmt.Errorf("%w: session ID %q", ErrInvalidSummary, sessionID)
	}
	return strings.NewReplacer(
		"{{SESSION_ID}}", sessionID,
		"{{WHOLE}}", strconv.Itoa(transcriptHeadBytes+transcriptTailBytes),
		"{{HEAD}}", strconv.Itoa(transcriptHeadBytes),
		"{{TAIL}}", strconv.Itoa(transcriptTailBytes),
	).Replace(transcriptScriptTemplate), nil
}

// transcriptData is what the host sent of one log. Whole means Head is the
// entire log; otherwise Head and Tail are its two ends, each cut mid-line.
type transcriptData struct {
	Head  []byte
	Tail  []byte
	Whole bool
}

// parseTranscriptOutput reads the fetch script's output. The body is read by
// the length the header announces, never by looking for a marker, since the
// log itself can hold any text.
func parseTranscriptOutput(out []byte) (transcriptData, error) {
	start := -1
	for i := 0; i < len(out); {
		if bytes.HasPrefix(out[i:], []byte(transcriptHeader)) {
			start = i + len(transcriptHeader)
			break
		}
		next := bytes.IndexByte(out[i:], '\n')
		if next < 0 {
			break
		}
		i += next + 1
	}
	if start < 0 {
		return transcriptData{}, errors.New("conversation log output has no header")
	}
	lineEnd := bytes.IndexByte(out[start:], '\n')
	if lineEnd < 0 {
		return transcriptData{}, errors.New("conversation log output ended in its header")
	}
	header := string(out[start : start+lineEnd])
	if header == transcriptNone {
		return transcriptData{}, ErrNoTranscript
	}
	sizeText, ok := strings.CutPrefix(header, transcriptVersion)
	size, err := strconv.Atoi(sizeText)
	if !ok || err != nil || size < 0 {
		return transcriptData{}, fmt.Errorf("conversation log output has an unknown header %q", header)
	}

	body := out[start+lineEnd+1:]
	data := transcriptData{Whole: size <= transcriptHeadBytes+transcriptTailBytes}
	length := size
	if !data.Whole {
		length = transcriptHeadBytes + transcriptTailBytes
	}
	if len(body) < length || !bytes.HasPrefix(body[length:], []byte(transcriptEnd)) {
		return transcriptData{}, errors.New("conversation log output is incomplete")
	}
	if data.Whole {
		data.Head = body[:length]
	} else {
		data.Head = body[:transcriptHeadBytes]
		data.Tail = body[transcriptHeadBytes:length]
	}
	return data, nil
}

// logMessage is one message of the conversation: who said it and its text.
type logMessage struct {
	Role string
	Text string
}

// logLine is the part of a conversation log line that is read.
type logLine struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	IsSidechain bool `json:"isSidechain"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// parseLogMessages reads the messages in chunk. dropFirst and dropLast skip
// a first or last line that a byte limit cut in the middle.
func parseLogMessages(chunk []byte, dropFirst, dropLast bool) []logMessage {
	rows := bytes.Split(chunk, []byte("\n"))
	if dropFirst && len(rows) > 0 {
		rows = rows[1:]
	}
	if dropLast && len(rows) > 0 {
		rows = rows[:len(rows)-1]
	}
	var messages []logMessage
	for _, row := range rows {
		var line logLine
		if json.Unmarshal(row, &line) != nil || line.IsSidechain {
			continue
		}
		if line.Type != "user" && line.Type != "assistant" {
			continue
		}
		if text := strings.TrimSpace(messageText(line.Message.Content)); text != "" {
			messages = append(messages, logMessage{Role: line.Type, Text: text})
		}
	}
	return messages
}

// messageText is a message's content when it is a string, or its "text"
// blocks joined when it is a list of blocks. Any other block — tool use,
// tool result, thinking, an image — is left out.
func messageText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []contentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// buildExcerpt is what claude is given to summarize: the session's first
// instruction, which says what the task is, and its most recent messages,
// which say where it stands. ok is false when the log holds no message this
// reading understands.
func buildExcerpt(data transcriptData) (string, bool) {
	var early, recent []logMessage
	if data.Whole {
		early = parseLogMessages(data.Head, false, false)
		recent = early
	} else {
		early = parseLogMessages(data.Head, false, true)
		recent = parseLogMessages(data.Tail, true, false)
	}

	firstIndex := -1
	for i, message := range early {
		if message.Role == "user" {
			firstIndex = i
			break
		}
	}

	keptFrom := len(recent)
	used := 0
	for keptFrom > 0 {
		text := formatMessage(recent[keptFrom-1], excerptMessageBytes)
		if used+len(text) > excerptRecentBytes {
			break
		}
		used += len(text)
		keptFrom--
	}
	if firstIndex < 0 && keptFrom == len(recent) {
		return "", false
	}

	var b strings.Builder
	// In a whole log the first instruction is also a recent message when it
	// falls inside the kept range; it is not repeated then.
	if firstIndex >= 0 && (!data.Whole || firstIndex < keptFrom) {
		b.WriteString(excerptFirstHeading + "\n")
		b.WriteString(formatMessage(early[firstIndex], excerptFirstBytes))
		b.WriteString("\n")
	}
	if keptFrom < len(recent) {
		b.WriteString(excerptRecentHeading + "\n")
		for _, message := range recent[keptFrom:] {
			b.WriteString(formatMessage(message, excerptMessageBytes))
		}
	}
	return b.String(), true
}

func formatMessage(message logMessage, limit int) string {
	return "[" + message.Role + "] " + truncateUTF8(message.Text, limit) + "\n\n"
}

// truncateUTF8 cuts s to at most limit bytes, on a character boundary, and
// marks the cut with an ellipsis.
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
