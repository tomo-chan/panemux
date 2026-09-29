package board

import "errors"

// errNoHomeDir stands in for whatever os.UserHomeDir reports when it cannot
// resolve a home directory. The real message is platform-specific — and on
// Unix it is only reachable at all by emptying $HOME — so tests inject this
// through homedir.SetFailingForTest and assert against it instead.
var errNoHomeDir = errors.New("no home directory")
