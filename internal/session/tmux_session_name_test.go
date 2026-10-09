package session

import "testing"

func TestIsValidTmuxSessionName(t *testing.T) {
	cases := map[string]bool{
		"gpu-box-0123abcd": true,
		"a_b.c-D9":         true,
		"":                 false,
		"a b":              false,
		"a'b":              false,
		"a;b":              false,
		"a:b":              false,
		"開発":               false,
	}
	for name, want := range cases {
		if got := IsValidTmuxSessionName(name); got != want {
			t.Errorf("IsValidTmuxSessionName(%q) = %v, want %v", name, got, want)
		}
	}
}
