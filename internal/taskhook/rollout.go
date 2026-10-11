package taskhook

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

const (
	MaxRolloutBytes     = 4 << 20
	MaxRolloutLineBytes = 64 << 10
)

// RolloutEvidence contains metadata only. The caller must open a private,
// regular no-follow rollout held by the verified native owner, verify its file
// identity before and after this scan, then recheck the owner's birth and UID.
type RolloutEvidence struct {
	Anchor Anchor
	Veto   bool
}

type rolloutMetadata struct {
	Type    string `json:"type"`
	Payload struct {
		ID       string          `json:"id"`
		TurnID   string          `json:"turn_id"`
		Type     string          `json:"type"`
		Reviewer *string         `json:"approvals_reviewer"`
		Source   json.RawMessage `json:"source"`
	} `json:"payload"`
}

// ScanRollout requires a complete bounded snapshot, explicit main-task session,
// and explicit user reviewer for the exact turn. Arbitrary transcript paths,
// latest-context guesses and partial snapshots are not proof. A terminal before
// ingress is a veto too; the anchor never narrows that check to later lines.
func ScanRollout(reader io.Reader, p Permission, fileID string) (RolloutEvidence, error) {
	if !uuid.MatchString(p.SessionID) || !opaque.MatchString(p.TurnID) || !opaque.MatchString(fileID) ||
		p.ToolKind != ToolBash {

		return RolloutEvidence{}, ErrInput
	}
	scanner := bufio.NewScanner(io.LimitReader(reader, MaxRolloutBytes+1))
	scanner.Buffer(make([]byte, 4096), MaxRolloutLineBytes)
	scanner.Split(completeLine)
	var e RolloutEvidence
	e.Anchor.FileID = fileID
	sessionSeen, turnSeen := false, false
	lastTurn := ""
	for scanner.Scan() {
		line := scanner.Bytes()
		e.Anchor.Offset += int64(len(line) + 1)
		if e.Anchor.Offset > MaxRolloutBytes {
			return RolloutEvidence{}, ErrUnsupported
		}
		var item rolloutMetadata
		if !decodeMetadata(line, &item) {
			return RolloutEvidence{}, ErrUnsupported
		}
		switch item.Type {
		case "session_meta":
			if sessionSeen || item.Payload.ID != p.SessionID || string(item.Payload.Source) != `"cli"` {
				return RolloutEvidence{}, ErrUnsupported
			}
			sessionSeen = true
		case "turn_context":
			if item.Payload.TurnID == "" {
				return RolloutEvidence{}, ErrUnsupported
			}
			lastTurn = item.Payload.TurnID
			if lastTurn == p.TurnID {
				if item.Payload.Reviewer == nil || *item.Payload.Reviewer != "user" {
					return RolloutEvidence{}, ErrUnsupported
				}
				turnSeen = true
			}
		case "event_msg":
			if item.Payload.TurnID == p.TurnID && (item.Payload.Type == "task_complete" || item.Payload.Type == "turn_aborted") {
				e.Veto = true
			}
		}
	}
	if scanner.Err() != nil || !sessionSeen || !turnSeen {
		return RolloutEvidence{}, ErrUnsupported
	}
	if lastTurn != p.TurnID {
		e.Veto = true
	}
	return e, nil
}

func completeLine(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return 0, nil, ErrUnsupported
	}
	return 0, nil, nil
}

func decodeMetadata(line []byte, dst *rolloutMetadata) bool {
	d := json.NewDecoder(bytes.NewReader(line))
	d.UseNumber()
	if !jsonValue(d, 0) {
		return false
	}
	if _, err := d.Token(); err != io.EOF {
		return false
	}
	return json.Unmarshal(line, dst) == nil
}
