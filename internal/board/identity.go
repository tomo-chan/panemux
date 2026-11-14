package board

import (
	"fmt"
	"regexp"
	"strings"
)

// Identity addresses a host-local agmsg name. An empty Host means an
// unqualified name; qualified addresses use BoardID, never an SSH hostname.
type Identity struct {
	Name string
	Host string
}

// Names and SSH connection keys share the existing safe identifier alphabet.
var identityName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var identityHost = regexp.MustCompile(`^(local|ssh:[A-Za-z0-9_.-]+)$`)

func ParseIdentity(raw string) (Identity, error) {
	name, host, qualified := strings.Cut(raw, "@")
	if !identityName.MatchString(name) || (qualified && (name == SystemID || !identityHost.MatchString(host))) {
		return Identity{}, fmt.Errorf("board: invalid identity %q", raw)
	}
	return Identity{Name: name, Host: host}, nil
}

func (id Identity) String() string {
	if id.Host == "" {
		return id.Name
	}
	return id.Name + "@" + id.Host
}
