package taskhook

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAncestorBudgetIncludesExactBoundaryButRejectsMore(t *testing.T) {
	for _, count := range []int{MaxAncestors, MaxAncestors + 1} {
		host := &bindingHost{processes: map[int]ProcessIdentity{}}
		owner := testOwner()
		owner.PID = 100 + count - 1
		for i := 0; i < count; i++ {
			host.processes[100+i] = ProcessIdentity{PID: 100 + i, PPID: 101 + i, UID: owner.UID, Birth: owner.Birth}
		}
		v := Verifier{Host: host}
		chain, err := v.ancestorChain(context.Background(), 100, owner)
		if count == MaxAncestors {
			require.NoError(t, err)
			require.Len(t, chain, count)
		} else {
			require.Error(t, err)
		}
	}
}

func TestVerifierFailedOwnerOrStorageProofCannotCreatePending(t *testing.T) {
	p := Permission{SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash}
	v, h, entry := verifierFixture(t)
	_, err := v.Identify(context.Background(), 100, entry.Owner.Generation, Permission{})
	require.ErrorIs(t, err, ErrInput)
	_, err = v.Identify(context.Background(), 100, "invalid", p)
	require.Error(t, err)
	h.filesError = ErrUnsupported
	_, err = v.Identify(context.Background(), 100, entry.Owner.Generation, p)
	require.Error(t, err)
	h.filesError = nil
	f := h.files[42]
	f.Sessions[0].FileID = "invalid/path"
	h.files[42] = f
	_, err = v.Identify(context.Background(), 100, entry.Owner.Generation, p)
	require.Error(t, err)
	f.Sessions[0].FileID = "file-a"
	h.files[42] = f
	_, err = v.Record(context.Background(), 100, "invalid", p)
	require.Error(t, err)
	v.Store.random = bytes.NewReader(nil)
	_, err = v.Record(context.Background(), 100, entry.Owner.Generation, p)
	require.ErrorIs(t, err, ErrStorage)
}

func TestAncestorChainCannotBindInitEvenIfMetadataIsAvailable(t *testing.T) {
	owner := testOwner()
	owner.PID = 1
	host := &bindingHost{processes: map[int]ProcessIdentity{
		2: {PID: 2, PPID: 1, UID: owner.UID, Birth: "child"}, 1: {PID: 1, UID: owner.UID, Birth: owner.Birth}}}
	_, err := (&Verifier{Host: host}).ancestorChain(context.Background(), 2, owner)
	require.Error(t, err)
}
