package board

import "encoding/json"

// statusKind is the fixed, required discriminator for a status-report
// message body. See docs/agent-board.md's "Status self-report and message
// flow" section for why shape-sniffing (e.g. checking for a "state" field)
// is not used: a human-composed chat message to Sentinel that happens to be
// valid JSON with a similarly named field must never be silently swallowed
// into the status cache.
const statusKind = "board_status"

// statusBody mirrors the JSON shape the bootstrap instruction tells an
// agent to send. All fields besides Kind are optional.
type statusBody struct {
	Kind     string `json:"kind"`
	State    string `json:"state"`
	CWD      string `json:"cwd"`
	Branch   string `json:"branch"`
	Repo     string `json:"repo"`
	PRURL    string `json:"pr_url"`
	LastTool string `json:"last_tool"`
	Summary  string `json:"summary"`
}

// ParseStatus attempts to parse body as a board_status update. ok is true
// only when body is valid JSON AND its "kind" field is exactly
// "board_status" — never on shape alone. Any other body (not JSON, or valid
// JSON with a missing/different kind, even one that happens to also carry a
// "state" field) returns ok == false and must be treated as an ordinary
// chat message by the caller.
func ParseStatus(body string) (Status, bool) {
	var sb statusBody
	if err := json.Unmarshal([]byte(body), &sb); err != nil {
		return Status{}, false
	}
	if sb.Kind != statusKind {
		return Status{}, false
	}
	return Status{
		State:    sb.State,
		CWD:      sb.CWD,
		Branch:   sb.Branch,
		Repo:     sb.Repo,
		PRURL:    sb.PRURL,
		LastTool: sb.LastTool,
		Summary:  sb.Summary,
	}, true
}
