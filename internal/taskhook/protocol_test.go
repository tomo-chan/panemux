package taskhook

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const testSession = "00000000-0000-4000-8000-000000000001"
const testTurn = "turn-1"
const testGeneration = "g1-00000000000000000000000000000001"

func testOwner() Owner {
	return Owner{Generation: testGeneration, UID: 1000, PID: 42, Birth: "boot1:100"}
}
func permissionJSON() string {
	return `{"hook_event_name":"PermissionRequest","session_id":"` + testSession +
		`","turn_id":"turn-1","tool_name":"Bash","permission_mode":"default","model":"model",` +
		`"cwd":"/workspace/user/project","transcript_path":null,` +
		`"tool_input":{"command":"PRIVATE-COMMAND","args":["PRIVATE-ARG"]}}`
}

func TestPermissionInputContract(t *testing.T) {
	input := permissionJSON()
	p, err := ParsePermission([]byte(input))
	if err != nil || p != (Permission{SessionID: testSession, TurnID: testTurn, ToolKind: "bash"}) {
		t.Fatalf("valid hook: %v %v", p, err)
	}
	data, _ := json.Marshal(p)
	if strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), "workspace") {
		t.Fatal("payload leaked into extracted protocol")
	}
	for name, bad := range map[string]string{
		"malformed": "{", "null": "null", "array": "[]", "trailing": input + ` {}`,
		"unknown":            strings.Replace(input, `"model":"model"`, `"request_id":"fake"`, 1),
		"duplicate":          strings.Replace(input, `"turn_id":"turn-1"`, `"turn_id":"turn-0","turn_id":"turn-1"`, 1),
		"wrong event":        strings.Replace(input, "PermissionRequest", "PostToolUse", 1),
		"bad session":        strings.Replace(input, testSession, "../../escape", 1),
		"wrong turn type":    strings.Replace(input, `"turn_id":"turn-1"`, `"turn_id":7`, 1),
		"missing field":      strings.Replace(input, `"transcript_path":null,`, "", 1),
		"wrong ignored type": strings.Replace(input, `"model":"model"`, `"model":7`, 1),
		"subagent":           strings.Replace(input, `"model":"model"`, `"model":"model","agent_id":"agent-1"`, 1),
		"unsupported tool":   strings.Replace(input, `"Bash"`, `"MCP"`, 1),
		"bypass":             strings.Replace(input, `"default"`, `"bypassPermissions"`, 1),
		"oversize":           strings.Replace(input, "PRIVATE-COMMAND", strings.Repeat("x", MaxInputBytes), 1),
		"depth": strings.Replace(input, `{"command":"PRIVATE-COMMAND","args":["PRIVATE-ARG"]}`,
			strings.Repeat("[", 40)+"0"+strings.Repeat("]", 40), 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePermission([]byte(bad))
			if err == nil {
				t.Fatal("accepted invalid or unsupported input")
			}
			if strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("raw diagnostic leaked")
			}
		})
	}
	for _, mode := range []string{"acceptEdits", "plan"} {
		if _, err := ParsePermission([]byte(strings.Replace(input, `"default"`, `"`+mode+`"`, 1))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecordCompatibilityAndIntegrity(t *testing.T) {
	r, err := StartRecord(Binding{SessionID: testSession, Owner: testOwner(), TurnID: testTurn},
		"ep1-00000000000000000000000000000001", "h1-00000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	good, err := EncodeRecord(r)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRecord(good)
	if err != nil || decoded.Revision != 1 || decoded.NoticeID != r.NoticeID {
		t.Fatalf("roundtrip: %v", err)
	}
	for name, mutate := range map[string]func(*Record){
		"version": func(r *Record) { r.Version++ }, "zero revision": func(r *Record) { r.Revision = 0 },
		"unsafe revision": func(r *Record) { r.Revision = MaxRevision + 1 },
		"generation":      func(r *Record) { r.Owner.Generation = "fake" },
		"pid":             func(r *Record) { r.Owner.PID = 0 },
		"uid":             func(r *Record) { r.Owner.UID = -1 },
		"birth":           func(r *Record) { r.Owner.Birth = "" },
		"session":         func(r *Record) { r.SessionID = "../../escape" }, "notice": func(r *Record) { r.NoticeID = "" },
		"episode":               func(r *Record) { r.Episodes[0].ID = "request-from-codex" },
		"disposition":           func(r *Record) { r.Episodes[0].Disposition = "approved" },
		"tool":                  func(r *Record) { r.Episodes[0].ToolKind = "command" },
		"turn":                  func(r *Record) { r.Episodes[0].TurnID = "" },
		"duplicate episode":     func(r *Record) { r.Episodes = append(r.Episodes, r.Episodes[0]) },
		"bad anchor":            func(r *Record) { r.Episodes[0].Anchor = &Anchor{FileID: "file-1", Offset: -1} },
		"missing file identity": func(r *Record) { r.Episodes[0].Anchor = &Anchor{Offset: 5} },
	} {
		t.Run(name, func(t *testing.T) {
			r2, _ := DecodeRecord(good)
			mutate(&r2)
			data, _ := json.Marshal(r2)
			if _, decodeErr := DecodeRecord(data); decodeErr == nil {
				t.Fatal("accepted corrupted record")
			}
		})
	}
	for _, bad := range [][]byte{
		nil,
		[]byte("null"),
		append(good, []byte(` {}`)...),
		[]byte(strings.Replace(string(good),
			`"version":1`, `"version":1,"version":1`, 1)),
		[]byte(strings.Replace(string(good),
			`"version":1`, `"version":1,"command":"PRIVATE"`, 1)),
		[]byte(strings.Repeat(" ", MaxRecordBytes+1)),
	} {
		if _, decodeErr := DecodeRecord(bad); decodeErr == nil {
			t.Fatal("accepted invalid JSON shape or size")
		}
	}
	r.Revision = MaxRevision
	_, err = AppendEpisode(r, Binding{SessionID: testSession, Owner: testOwner(), TurnID: testTurn},
		"ep1-00000000000000000000000000000002", "h1-00000000000000000000000000000002")
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("revision overflow: %v", err)
	}
}
