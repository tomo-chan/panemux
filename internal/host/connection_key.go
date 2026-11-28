package host

import (
	"fmt"
	"regexp"
)

// ConnectionKeyCharacters is the single key rule used by configuration and BoardID parsing.
const ConnectionKeyCharacters = "Unicode letters, combining marks, numbers and ._-+ (at least one character)"

var connectionKey = regexp.MustCompile(`^[\p{L}\p{M}\p{N}_.+-]+$`)

// ValidateConnectionKey preserves exact spelling; it never normalizes a key.
func ValidateConnectionKey(key string) error {
	if key == "" {
		return fmt.Errorf("ssh connection name is required (key %q); use %s", key, ConnectionKeyCharacters)
	}
	if !connectionKey.MatchString(key) {
		return fmt.Errorf("ssh connection name must contain only %s (key %q)", ConnectionKeyCharacters, key)
	}
	return nil
}
