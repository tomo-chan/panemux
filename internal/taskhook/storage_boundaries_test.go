package taskhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"panemux/internal/fileops"
)

type faultRecords struct {
	readErr, lockErr        error
	onLock, onRead, onWrite func()
	data                    []byte
	writes                  int
}

func (f *faultRecords) Close() error { return nil }
func (f *faultRecords) Read(string, int) ([]byte, error) {
	if f.onRead != nil {
		f.onRead()
	}
	return f.data, f.readErr
}
func (f *faultRecords) Replace(_ string, data []byte) error {
	f.writes++
	f.data = append([]byte(nil), data...)
	if f.onWrite != nil {
		f.onWrite()
	}
	return nil
}
func (f *faultRecords) Lock(context.Context, time.Duration) (func(), error) {
	if f.onLock != nil {
		f.onLock()
	}
	return func() {}, f.lockErr
}

func TestStoreCancellationAtIOBoundariesNeverAcknowledgesUnknownWrite(t *testing.T) {
	for _, stage := range []string{"before-open", "while-locking", "after-lock", "after-read", "after-write"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := start(t)
			data, err := EncodeRecord(r)
			require.NoError(t, err)
			f := &faultRecords{data: data}
			s := NewStore("/workspace/records")
			s.open = func(string) (recordFiles, error) { return f, nil }
			switch stage {
			case "before-open":
				cancel()
			case "while-locking":
				f.onLock = cancel
				f.lockErr = errors.New("interrupted")
			case "after-lock":
				f.onLock = cancel
			case "after-read":
				f.onRead = cancel
			case "after-write":
				f.onWrite = cancel
			}
			got, err := s.MarkProvided(ctx, claim(r))
			require.Error(t, err)
			require.NotEqual(t, uint64(2), got.Revision, "must not acknowledge failed/unknown write")
			if stage == "after-write" {
				require.Equal(t, 1, f.writes)
				durable, decodeErr := DecodeRecord(f.data)
				require.NoError(t, decodeErr)
				require.Empty(t, PendingIDs(durable))
				f.onWrite = nil
				got, err = s.MarkProvided(context.Background(), claim(r))
				require.NoError(t, err)
				require.Equal(t, durable, got)
				require.Equal(t, 1, f.writes, "identical retry must not write again")
			} else {
				require.Zero(t, f.writes)
			}
		})
	}
}

func TestStoreReadAndMutationErrorsPreservePrivateRecord(t *testing.T) {
	r := start(t)
	data, err := EncodeRecord(r)
	require.NoError(t, err)
	for _, readErr := range []error{fileops.ErrPrivateLimit, errors.New("read failed")} {
		f := &faultRecords{data: data, readErr: readErr}
		s := NewStore("/workspace/records")
		s.open = func(string) (recordFiles, error) { return f, nil }
		_, err = s.Read(r.SessionID, r.Owner)
		require.Error(t, err)
		_, err = s.MarkProvided(context.Background(), claim(r))
		require.Error(t, err)
		require.Zero(t, f.writes)
	}
	s := NewStore("/workspace/missing")
	_, err = s.Read(r.SessionID, r.Owner)
	require.ErrorIs(t, err, ErrStorage)
	_, err = s.Append(context.Background(), binding(testTurn))
	require.ErrorIs(t, err, ErrStorage)
	_, err = s.Read("invalid", r.Owner)
	require.ErrorIs(t, err, ErrInput)
	_, err = s.MarkProvided(context.Background(), Provided{SessionID: "invalid"})
	require.ErrorIs(t, err, ErrInput)
}

func TestStoreMissingInputsAndLockFailures(t *testing.T) {
	r := start(t)
	data, err := EncodeRecord(r)
	require.NoError(t, err)
	s, _ := newStore(t)
	_, err = s.MarkProvided(context.Background(), claim(r))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = s.ResolveTurn(context.Background(), Terminal{Owner: r.Owner, SessionID: r.SessionID})
	require.ErrorIs(t, err, os.ErrNotExist)
	s.random = bytes.NewReader(nil)
	_, err = s.Append(context.Background(), binding(testTurn))
	require.ErrorIs(t, err, ErrStorage)
	_, _, err = s.Reserve(context.Background(), binding(testTurn))
	require.ErrorIs(t, err, ErrStorage)
	_, err = s.Append(context.Background(), Binding{})
	require.ErrorIs(t, err, ErrInput)
	_, _, err = s.Reserve(context.Background(), Binding{})
	require.ErrorIs(t, err, ErrInput)
	for _, lockErr := range []error{context.DeadlineExceeded, errors.New("lock failed")} {
		f := &faultRecords{data: data, lockErr: lockErr}
		s.open = func(string) (recordFiles, error) { return f, nil }
		_, err = s.MarkProvided(context.Background(), claim(r))
		require.Error(t, err)
		require.Zero(t, f.writes)
	}
}

func TestStoreRejectsCorruptNextStateAndMisboundReads(t *testing.T) {
	r := start(t)
	data, err := EncodeRecord(r)
	require.NoError(t, err)
	s := NewStore("/workspace/records")
	// A callback cannot commit a malformed next state.
	f := &faultRecords{data: data}
	s.open = func(string) (recordFiles, error) { return f, nil }
	_, err = s.update(context.Background(), r.SessionID, r.Owner, func(next Record, _ bool) (Record, error) {
		next.Version++
		next.Revision++
		return next, nil
	})
	require.Error(t, err)
	require.Zero(t, f.writes)
	// Renaming a file does not turn another session or birth into the requested record.
	different := cloneRecord(r)
	different.SessionID = "00000000-0000-4000-8000-000000000002"
	f.data, err = EncodeRecord(different)
	require.NoError(t, err)
	_, err = s.Read(r.SessionID, r.Owner)
	require.ErrorIs(t, err, ErrCorrupt)
	different.SessionID = r.SessionID
	different.Owner.Birth = "other"
	f.data, err = EncodeRecord(different)
	require.NoError(t, err)
	_, err = s.Read(r.SessionID, r.Owner)
	require.ErrorIs(t, err, ErrConflict)
}

func TestRegistryStorageFailuresNeverReplaceIdentity(t *testing.T) {
	entry := registration()
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	for _, scenario := range []string{"open", "lock", "read", "corrupt", "cancel", "write-cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &faultRecords{readErr: os.ErrNotExist}
			r := NewRegistry("/workspace/registry")
			r.store.open = func(string) (recordFiles, error) { return f, nil }
			switch scenario {
			case "open":
				r.store.open = func(string) (recordFiles, error) { return nil, ErrStorage }
			case "lock":
				f.lockErr = errors.New("lock failed")
			case "read":
				f.readErr = errors.New("read failed")
			case "corrupt":
				f.readErr = nil
				f.data = []byte("{}")
				_, err = r.Read(entry.Owner.Generation)
				require.Error(t, err)
			case "cancel":
				f.onRead = cancel
			case "write-cancel":
				f.onWrite = cancel
			}
			err = r.Register(ctx, entry)
			require.Error(t, err)
			if scenario == "write-cancel" {
				require.Equal(t, data, f.data)
			} else {
				require.Zero(t, f.writes)
			}
		})
	}
	r := NewRegistry("/workspace/missing")
	_, err = r.Read(entry.Owner.Generation)
	require.ErrorIs(t, err, ErrStorage)
	_, err = r.Read("invalid")
	require.ErrorIs(t, err, ErrInput)
	bad := entry
	bad.Owner.PID = 0
	require.ErrorIs(t, r.Register(context.Background(), bad), ErrInput)
}
