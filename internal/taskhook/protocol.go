// Package taskhook defines host-local input notices, not approval requests or
// evidence that Codex accepted an answer. Process and rollout verification is
// deliberately outside this package's state transitions.
package taskhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

const (
	ProtocolVersion = 1
	MaxInputBytes   = 64 << 10
	MaxRecordBytes  = 16 << 10
	MaxPending      = 16
	// Revisions remain exactly representable by future JSON/browser consumers.
	MaxRevision         uint64 = 1<<53 - 1
	Pending                    = "pending"
	Provisional                = "provisional"
	ProvidedDisposition        = "provided"
	Resolved                   = "resolved"
	ToolBash                   = "bash"
)

var (
	ErrInput       = errors.New("invalid hook input")
	ErrUnsupported = errors.New("unsupported hook protocol or scope")
	ErrCorrupt     = errors.New("invalid hook record")
	ErrConflict    = errors.New("hook record changed")
	ErrLimit       = errors.New("hook record limit reached")
	uuid           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	opaque         = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)
	generationID   = regexp.MustCompile(`^g1-[0-9a-f]{32}$`)
	episodeID      = regexp.MustCompile(`^ep1-[0-9a-f]{32}$`)
	noticeID       = regexp.MustCompile(`^h1-[0-9a-f]{32}$`)
)

type Owner struct {
	Generation string `json:"generation"`
	Birth      string `json:"birth"`
	UID        int    `json:"uid"`
	PID        int    `json:"pid"`
}

// Anchor names a verified file identity and the end of its last complete line
// at notice start. It contains no path or transcript content.
type Anchor struct {
	FileID string `json:"file_id"`
	Offset int64  `json:"offset"`
}

type Episode struct {
	Anchor      *Anchor `json:"anchor,omitempty"`
	ID          string  `json:"id"`
	TurnID      string  `json:"turn_id"`
	ToolKind    string  `json:"tool_kind"`
	Disposition string  `json:"disposition"`
}

type Receipt struct {
	EpisodeIDs []string `json:"episode_ids"`
	Revision   uint64   `json:"revision"`
}

type Record struct {
	Receipt   *Receipt  `json:"provided_receipt,omitempty"`
	SessionID string    `json:"session_id"`
	NoticeID  string    `json:"notice_id"`
	Episodes  []Episode `json:"episodes"`
	Owner     Owner     `json:"owner"`
	Version   int       `json:"version"`
	Revision  uint64    `json:"revision"`
	// ClosedRevision fences slow ingress scans across an acknowledgement or
	// terminal transition without invalidating other concurrent reservations.
	ClosedRevision uint64 `json:"closed_revision,omitempty"`
}

// Binding must come from the host's process/session/turn verifier. Accepting a
// caller's environment or hook payload as that proof is not sufficient.
type Binding struct {
	Anchor    *Anchor
	SessionID string
	TurnID    string
	Owner     Owner
}

type Permission struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	ToolKind  string `json:"tool_kind"`
}

type Provided struct {
	SessionID  string   `json:"session_id"`
	EpisodeIDs []string `json:"episode_ids"`
	Owner      Owner    `json:"owner"`
	Revision   uint64   `json:"revision"`
}

// Terminal is evidence supplied after a complete, bounded scan of a verified
// rollout. Offset is the end of the terminal line, strictly after the anchor.
// Partial lines, rotation, truncation, scan gaps and unknown events are not
// evidence and must never be passed here by a reader.
type Terminal struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	FileID    string `json:"file_id"`
	Owner     Owner  `json:"owner"`
	Revision  uint64 `json:"revision"`
	Offset    int64  `json:"offset"`
}

func ParsePermission(data []byte) (Permission, error) {
	var input struct {
		Transcript *string         `json:"transcript_path"`
		AgentID    *string         `json:"agent_id"`
		AgentType  *string         `json:"agent_type"`
		Event      string          `json:"hook_event_name"`
		SessionID  string          `json:"session_id"`
		TurnID     string          `json:"turn_id"`
		Tool       string          `json:"tool_name"`
		Mode       string          `json:"permission_mode"`
		Model      string          `json:"model"`
		CWD        string          `json:"cwd"`
		ToolInput  json.RawMessage `json:"tool_input"`
	}
	if !strictJSON(data, MaxInputBytes, &input) {
		return Permission{}, ErrInput
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(data, &keys) != nil {
		return Permission{}, ErrInput
	}
	for _, key := range []string{
		"hook_event_name", "session_id", "turn_id", "tool_name", "permission_mode",
		"model", "cwd", "transcript_path", "tool_input",
	} {
		if _, ok := keys[key]; !ok {
			return Permission{}, ErrInput
		}
	}
	// A null string is accepted by encoding/json; the fixed hook schema is not.
	for _, key := range []string{
		"hook_event_name", "session_id", "turn_id", "tool_name", "permission_mode", "model", "cwd", "agent_id", "agent_type",
	} {
		if raw, ok := keys[key]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return Permission{}, ErrInput
		}
	}
	if input.Event != "PermissionRequest" || !uuid.MatchString(input.SessionID) || !opaque.MatchString(input.TurnID) {
		return Permission{}, ErrInput
	}
	if _, ok := keys["agent_id"]; ok {
		return Permission{}, ErrUnsupported
	}
	if _, ok := keys["agent_type"]; ok {
		return Permission{}, ErrUnsupported
	}
	if input.Tool != "Bash" {
		return Permission{}, ErrUnsupported
	}
	switch input.Mode {
	case "default", "acceptEdits", "plan":
	default:
		return Permission{}, ErrUnsupported
	}
	return Permission{SessionID: input.SessionID, TurnID: input.TurnID, ToolKind: ToolBash}, nil
}

func DecodeRecord(data []byte) (Record, error) {
	var r Record
	if !strictJSON(data, MaxRecordBytes, &r) {
		return Record{}, ErrCorrupt
	}
	if err := validateRecord(r); err != nil {
		return Record{}, err
	}
	return r, nil
}

func EncodeRecord(r Record) ([]byte, error) {
	if err := validateRecord(r); err != nil {
		return nil, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, ErrCorrupt
	}
	if len(data) > MaxRecordBytes {
		return nil, ErrLimit
	}
	return data, nil
}

func validOwner(o Owner) bool {
	return generationID.MatchString(o.Generation) && o.UID >= 0 && o.PID > 0 && opaque.MatchString(o.Birth)
}
func validAnchor(a *Anchor) bool { return a == nil || (opaque.MatchString(a.FileID) && a.Offset >= 0) }
func validBinding(b Binding) bool {
	return uuid.MatchString(b.SessionID) && validOwner(b.Owner) && opaque.MatchString(b.TurnID) && validAnchor(b.Anchor)
}

func validateRecord(r Record) error {
	if r.Version != ProtocolVersion {
		return ErrUnsupported
	}
	if !uuid.MatchString(r.SessionID) || !validOwner(r.Owner) || r.Revision == 0 || r.Revision > MaxRevision ||
		r.ClosedRevision > r.Revision || !noticeID.MatchString(r.NoticeID) ||
		len(r.Episodes) == 0 || len(r.Episodes) > MaxPending {

		return ErrCorrupt
	}
	seen := make(map[string]string, len(r.Episodes))
	for _, ep := range r.Episodes {
		if !episodeID.MatchString(ep.ID) || !opaque.MatchString(ep.TurnID) ||
			ep.ToolKind != ToolBash || !validAnchor(ep.Anchor) {

			return ErrCorrupt
		}
		if _, ok := seen[ep.ID]; ok {
			return ErrCorrupt
		}
		switch ep.Disposition {
		case Pending, Provisional, ProvidedDisposition, Resolved:
		default:
			return ErrCorrupt
		}
		if ep.Disposition == Provisional && ep.Anchor != nil {
			return ErrCorrupt
		}
		seen[ep.ID] = ep.Disposition
	}
	if p := r.Receipt; p != nil {
		if r.Revision <= 1 || p.Revision != r.Revision-1 || liveEpisodes(r) != 0 || !validSet(p.EpisodeIDs) {
			return ErrCorrupt
		}
		for _, id := range p.EpisodeIDs {
			if seen[id] != ProvidedDisposition {
				return ErrCorrupt
			}
		}
	}
	return nil
}

func validSet(ids []string) bool {
	if len(ids) == 0 || len(ids) > MaxPending {
		return false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !episodeID.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// strictJSON also refuses duplicate keys, trailing values and excessive depth.
// Errors are fixed diagnostics; decoder text can contain private payloads.
func strictJSON(data []byte, limit int, dst any) bool {
	if len(data) == 0 || len(data) > limit {
		return false
	}
	tokens := json.NewDecoder(bytes.NewReader(data))
	tokens.UseNumber()
	if !jsonValue(tokens, 0) {
		return false
	}
	if _, err := tokens.Token(); err != io.EOF {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst) == nil
}

func jsonValue(d *json.Decoder, depth int) bool {
	if depth > 32 {
		return false
	}
	token, err := d.Token()
	if err != nil {
		return false
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return true
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return false
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return false
			}
			seen[name] = true
			if !jsonValue(d, depth+1) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for d.More() {
			if !jsonValue(d, depth+1) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}
