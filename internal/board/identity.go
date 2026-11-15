package board

import (
	"fmt"
	"panemux/internal/host"
	"regexp"
	"strings"
)

// Identity addresses a host-local agmsg name. An empty Host means an
// unqualified name; qualified addresses use BoardID, never an SSH hostname.
type Identity struct {
	Name string
	Host string
}

// Agent names retain their ASCII alphabet; host keys use host.ValidateConnectionKey.
var identityName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ValidBoardID checks the protocol host identifier without normalizing a key.
// Connection keys use the same validator as configuration loading and API writes.
func ValidBoardID(raw string) bool {
	if raw == host.Local.BoardID() {
		return true
	}
	key, qualified := strings.CutPrefix(raw, "ssh:")
	return qualified && host.ValidateConnectionKey(key) == nil
}

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
