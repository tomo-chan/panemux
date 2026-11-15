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

// Agent names retain their ASCII alphabet. SSH keys also allow Unicode letters,
// marks and numbers plus a literal plus, without introducing address separators.
var identityName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var identityHost = regexp.MustCompile(`^(local|ssh:[\p{L}\p{M}\p{N}_.+-]+)$`)

// ValidBoardID checks the protocol host identifier without normalizing a key.
// It is shared by address parsing and composition-root host eligibility.
func ValidBoardID(raw string) bool { return identityHost.MatchString(raw) }

func ParseIdentity(raw string) (Identity, error) {
	name, host, qualified := strings.Cut(raw, "@")
	if !identityName.MatchString(name) || (qualified && (name == SystemID || !ValidBoardID(host))) {
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
