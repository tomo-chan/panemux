package taskhook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"panemux/internal/fileops"
)

var ErrStorage = errors.New("hook record storage unavailable")

type recordFiles interface {
	Close() error
	Read(string, int) ([]byte, error)
	Replace(string, []byte) error
	Lock(context.Context, time.Duration) (func(), error)
}

// Store does not create or repair the private root, verify process bindings,
// change Codex configuration, or register hooks. The host must supply a private
// root and verified Binding. A record for a different owner is a conflict;
// reusing its session ID never grants permission to replace that generation.
type Store struct {
	random io.Reader
	open   func(string) (recordFiles, error)
	root   string
}

func NewStore(root string) *Store {
	return &Store{root: root, random: rand.Reader, open: func(path string) (recordFiles, error) {
		d, err := fileops.OpenPrivateDir(path)
		if err != nil {
			return nil, fmt.Errorf("opening hook records: %w", err)
		}
		return d, nil
	}}
}

func (s *Store) Read(sessionID string, owner Owner) (Record, error) {
	if !uuid.MatchString(sessionID) || !validOwner(owner) {
		return Record{}, ErrInput
	}
	d, err := s.open(s.root)
	if err != nil {
		return Record{}, ErrStorage
	}
	defer func() { _ = d.Close() }()
	return readRecord(d, sessionID, owner)
}

func recordName(sessionID string, owner Owner) string {
	return owner.Generation + "-" + sessionID + ".json"
}

func readRecord(d recordFiles, sessionID string, owner Owner) (Record, error) {
	data, err := d.Read(recordName(sessionID, owner), MaxRecordBytes)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, os.ErrNotExist
	}
	if errors.Is(err, fileops.ErrPrivateLimit) {
		return Record{}, ErrLimit
	}
	if err != nil {
		return Record{}, ErrStorage
	}
	r, err := DecodeRecord(data)
	if err != nil {
		return Record{}, err
	}
	if r.SessionID != sessionID {
		return Record{}, ErrCorrupt
	}
	if r.Owner != owner {
		return Record{}, ErrConflict
	}
	return r, nil
}

func (s *Store) Append(ctx context.Context, b Binding) (Record, error) {
	if !validBinding(b) {
		return Record{}, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return Record{}, fmt.Errorf("appending hook record: %w", err)
	}
	var random [32]byte
	if _, err := io.ReadFull(s.random, random[:]); err != nil {
		return Record{}, ErrStorage
	}
	id := "ep1-" + hex.EncodeToString(random[:16])
	notice := "h1-" + hex.EncodeToString(random[16:])
	return s.update(ctx, b.SessionID, b.Owner, func(r Record, exists bool) (Record, error) {
		if !exists {
			return StartRecord(b, id, notice)
		}
		return AppendEpisode(r, b, id, notice)
	})
}

func (s *Store) MarkProvided(ctx context.Context, p Provided) (Record, error) {
	return s.update(ctx, p.SessionID, p.Owner, func(r Record, exists bool) (Record, error) {
		if !exists {
			return r, os.ErrNotExist
		}
		return MarkProvided(r, p)
	})
}

func (s *Store) ResolveTurn(ctx context.Context, p Terminal) (Record, error) {
	return s.update(ctx, p.SessionID, p.Owner, func(r Record, exists bool) (Record, error) {
		if !exists {
			return r, os.ErrNotExist
		}
		return ResolveTurn(r, p)
	})
}

func (s *Store) update(ctx context.Context, sessionID string, owner Owner,
	change func(Record, bool) (Record, error),
) (Record, error) {
	if !uuid.MatchString(sessionID) || !validOwner(owner) {
		return Record{}, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return Record{}, fmt.Errorf("updating hook record: %w", err)
	}
	d, err := s.open(s.root)
	if err != nil {
		return Record{}, ErrStorage
	}
	defer func() { _ = d.Close() }()
	release, err := d.Lock(ctx, 100*time.Millisecond)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Record{}, fmt.Errorf("locking hook record: %w", ctxErr)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return Record{}, fmt.Errorf("locking hook records: %w", err)
		}
		return Record{}, ErrStorage
	}
	defer release()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Record{}, fmt.Errorf("reading hook record: %w", ctxErr)
	}
	r, err := readRecord(d, sessionID, owner)
	exists := !errors.Is(err, os.ErrNotExist)
	if err != nil && exists {
		return Record{}, err
	}
	next, err := change(r, exists)
	if err != nil {
		return r, err
	}
	if exists && next.Revision == r.Revision {
		return next, nil
	}
	data, err := EncodeRecord(next)
	if err != nil {
		return r, err
	}
	if err := ctx.Err(); err != nil {
		return r, fmt.Errorf("saving hook record: %w", err)
	}
	if d.Replace(recordName(sessionID, owner), data) != nil {
		return r, ErrStorage
	}
	// If cancellation races the durable write, the result is unknown to the
	// caller. A reread and an identical claim retry recover it without clearing
	// any newly appended episode.
	if err := ctx.Err(); err != nil {
		return r, fmt.Errorf("confirming hook record: %w", err)
	}
	return next, nil
}
