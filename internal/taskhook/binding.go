package taskhook

import "context"

const MaxAncestors = 32

type ProcessIdentity struct {
	Birth string
	UID   int
	PID   int
	PPID  int
}

// HeldSession is returned only after the host has checked a regular no-follow
// file, its UID and inode against the native process's actual open descriptor
// set, and its first-line session metadata against the trusted sessions root.
// Path is transient host metadata; it never enters a notice record.
type HeldSession struct {
	SessionID string
	FileID    string
	Path      string
	Root      string
}

type NativeFiles struct {
	Sessions []HeldSession
	Native   bool
}

// BindingHost is the OS boundary. Files must verify the native executable file
// identity, not just a comm name or environment claim. Scan must operate on a
// bounded snapshot of the same no-follow identity, with no unverified gaps.
type BindingHost interface {
	Process(context.Context, int) (ProcessIdentity, error)
	Files(context.Context, int, string, string) (NativeFiles, error)
	Scan(context.Context, HeldSession, Permission) (RolloutEvidence, error)
}

type BindingProof struct {
	File    HeldSession
	Binding Binding
}

type Verifier struct {
	Registry *Registry
	Store    *Store
	Host     BindingHost
}

func (v *Verifier) ancestorChain(ctx context.Context, hookPID int, owner Owner) ([]ProcessIdentity, error) {
	var chain []ProcessIdentity
	seen := map[int]bool{}
	for pid := hookPID; pid > 1 && len(chain) < MaxAncestors; {
		if ctx.Err() != nil || seen[pid] {
			return nil, ErrUnsupported
		}
		seen[pid] = true
		p, err := v.Host.Process(ctx, pid)
		if err != nil || p.PID != pid || p.UID != owner.UID || !opaque.MatchString(p.Birth) {
			return nil, ErrUnsupported
		}
		chain = append(chain, p)
		if p.PID == owner.PID {
			if p.Birth != owner.Birth {
				return nil, ErrUnsupported
			}
			return chain, nil
		}
		pid = p.PPID
	}
	return nil, ErrUnsupported
}

// Identify checks the registry and finite parent chain before slow turn scans.
// All open sessions may coexist; only the explicit hook session must identify
// exactly one native ancestor and file identity. Duplicate descriptors collapse.
func (v *Verifier) Identify(ctx context.Context, hookPID int, generation string, p Permission) (BindingProof, error) {
	if !uuid.MatchString(p.SessionID) || !opaque.MatchString(p.TurnID) || p.ToolKind != ToolBash {
		return BindingProof{}, ErrInput
	}
	entry, err := v.Registry.Read(generation)
	if err != nil {
		return BindingProof{}, err
	}
	if !entry.Scope.Supported() {
		return BindingProof{}, ErrUnsupported
	}
	chain, err := v.ancestorChain(ctx, hookPID, entry.Owner)
	if err != nil {
		return BindingProof{}, err
	}
	var proof BindingProof
	found := false
	for _, proc := range chain[1:] {
		files, filesErr := v.Host.Files(ctx, proc.PID, entry.SessionsRoot, entry.ExecutableID)
		if filesErr != nil {
			return BindingProof{}, ErrUnsupported
		}
		if !files.Native {
			continue
		}
		matches := map[string]HeldSession{}
		for _, f := range files.Sessions {
			if f.SessionID != p.SessionID {
				continue
			}
			if !opaque.MatchString(f.FileID) {
				return BindingProof{}, ErrUnsupported
			}
			matches[f.FileID] = f
		}
		if len(matches) > 1 || (len(matches) == 1 && found) {
			return BindingProof{}, ErrUnsupported
		}
		for _, f := range matches {
			proof = BindingProof{File: f, Binding: Binding{SessionID: p.SessionID, TurnID: p.TurnID,
				Owner: Owner{Generation: generation, PID: proc.PID, UID: proc.UID, Birth: proc.Birth}}}
			found = true
		}
	}
	if !found || ctx.Err() != nil {
		return BindingProof{}, ErrUnsupported
	}
	return proof, nil
}

func (v *Verifier) Record(ctx context.Context, hookPID int, generation string, p Permission) (Record, error) {
	proof, err := v.Identify(ctx, hookPID, generation, p)
	if err != nil {
		return Record{}, err
	}
	token, r, err := v.Store.Reserve(ctx, proof.Binding)
	if err != nil {
		return r, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = v.Store.Cancel(ctx, token)
		}
	}()
	evidence, err := v.Host.Scan(ctx, proof.File, p)
	if err != nil {
		return r, ErrUnsupported
	}
	latest, err := v.Identify(ctx, hookPID, generation, p)
	if err != nil || latest.Binding.Owner != proof.Binding.Owner || latest.File.FileID != proof.File.FileID ||
		evidence.Anchor.FileID != proof.File.FileID {

		return r, ErrConflict
	}
	proof.Binding.Anchor = &evidence.Anchor
	r, err = v.Store.Finish(ctx, token, proof.Binding, evidence.Veto)
	committed = err == nil
	return r, err
}
