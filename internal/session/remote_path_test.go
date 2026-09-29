package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ValidateRemotePath is the guard internal/tasks puts in front of its launch
// script; it must accept and refuse exactly what the pane's own guard does.
func TestValidateRemotePath(t *testing.T) {
	for path, ok := range map[string]bool{
		"/workspace/user/project":    true,
		"/workspace/user/my project": true,
		"/":                          true,
		"workspace/user/project":     false,
		"":                           false,
		"/tmp/$(id)":                 false,
		"/tmp/it's":                  false,
		"/tmp/a;b":                   false,
		"/tmp/a\nb":                  false,
	} {
		err := ValidateRemotePath("working directory", path)
		if ok {
			assert.NoError(t, err, path)
		} else {
			assert.ErrorContains(t, err, "must be an absolute path with no shell metacharacters", path)
		}
	}
}
