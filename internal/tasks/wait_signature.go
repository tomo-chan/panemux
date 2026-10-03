package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// waitSignatureVersion prefixes every wait signature. Changing what a
// signature is made from changes the version, so a browser that remembers
// signatures from before takes every wait as new rather than matching a
// value made another way.
const waitSignatureVersion = "w1"

// waitSignature identifies one wait of one session (issue #278): the same
// wait gives the same value on every collection, and a wait that began
// later, or for another reason, gives another. startMillis is when the wait
// began on the host's own clock, as the agent recorded it — never
// panemux's clock, the collection time or a file's mtime, which would make
// one wait look like many. Without a session or a start there is no
// signature: one is never made up. The value is opaque to clients.
func waitSignature(host, agent, sessionID string, startMillis int64, kind string) string {
	if sessionID == "" || startMillis <= 0 {
		return ""
	}
	// Each part is length-prefixed, so no value can borrow bytes from the
	// part after it.
	var b strings.Builder
	for _, part := range []string{waitSignatureVersion, host, agent, sessionID, strconv.FormatInt(startMillis, 10), kind} {
		b.WriteString(strconv.Itoa(len(part)))
		b.WriteByte(':')
		b.WriteString(part)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return waitSignatureVersion + "-" + hex.EncodeToString(sum[:])
}
