package board

import "testing"

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantOK  bool
		wantSt  Status
	}{
		{
			name: "valid full payload",
			body: `{"kind":"board_status","state":"working","cwd":"/workspace/user/project",` +
				`"branch":"feature/x","repo":"owner/repo",` +
				`"pr_url":"https://github.com/owner/repo/pull/123",` +
				`"last_tool":"Edit internal/api/handler.go","summary":"fixing failing tests"}`,
			wantOK: true,
			wantSt: Status{
				State:    "working",
				CWD:      "/workspace/user/project",
				Branch:   "feature/x",
				Repo:     "owner/repo",
				PRURL:    "https://github.com/owner/repo/pull/123",
				LastTool: "Edit internal/api/handler.go",
				Summary:  "fixing failing tests",
			},
		},
		{
			name:   "missing optional fields",
			body:   `{"kind":"board_status","state":"idle"}`,
			wantOK: true,
			wantSt: Status{State: "idle"},
		},
		{
			name:   "not valid JSON falls back to plain message",
			body:   "please review this PR when you get a chance",
			wantOK: false,
		},
		{
			name:   "valid JSON but not an object",
			body:   `"just a string"`,
			wantOK: false,
		},
		{
			name:   "valid JSON array",
			body:   `["state","working"]`,
			wantOK: false,
		},
		{
			// Regression test for the shape-sniffing ambiguity this design
			// document used to have: valid JSON, has a "state" field, but
			// kind is missing entirely.
			name:   "valid JSON with state field but missing kind",
			body:   `{"state":"working","cwd":"/workspace/user/project"}`,
			wantOK: false,
		},
		{
			// Regression test: valid JSON, has a "state" field, kind present
			// but wrong value.
			name:   "valid JSON with state field but wrong kind",
			body:   `{"kind":"chat","state":"working"}`,
			wantOK: false,
		},
		{
			name:   "empty body",
			body:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseStatus(tt.body)
			if ok != tt.wantOK {
				t.Fatalf("ParseStatus(%q) ok = %v, want %v", tt.body, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got != tt.wantSt {
				t.Fatalf("ParseStatus(%q) = %+v, want %+v", tt.body, got, tt.wantSt)
			}
		})
	}
}
