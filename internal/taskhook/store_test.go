package taskhook

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"panemux/internal/fileops"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "records")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	return NewStore(root), root
}

func TestStorePersistsStableNoticeAndDisposition(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	first, err := s.Append(ctx, binding(testTurn))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Append(ctx, binding("turn-2"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 2 || len(PendingIDs(second)) != 2 || first.NoticeID == second.NoticeID {
		t.Fatal("append lost an episode")
	}
	restarted := NewStore(root)
	read, err := restarted.Read(testSession, testOwner())
	if err != nil || read.NoticeID != second.NoticeID {
		t.Fatalf("restart changed notice: %v", err)
	}
	p := claim(second)
	provided, err := restarted.MarkProvided(ctx, p)
	if err != nil || len(PendingIDs(provided)) != 0 {
		t.Fatalf("durable claim: %v", err)
	}
	read, err = s.Read(testSession, testOwner())
	if err != nil || read.Revision != provided.Revision || len(PendingIDs(read)) != 0 {
		t.Fatal("provided not durable")
	}
	again, err := s.MarkProvided(ctx, p)
	if err != nil || again.Revision != read.Revision {
		t.Fatal("retry after restart not idempotent")
	}
	terminal := Terminal{
		SessionID: testSession,
		Owner:     second.Owner,
		Revision:  second.Revision,
		TurnID:    testTurn,
		FileID:    "file-1",
		Offset:    200,
	}
	if _, updateErr := s.ResolveTurn(ctx, terminal); !errors.Is(updateErr, ErrConflict) {
		t.Fatal("old scan overwritten provided state")
	}
	third, err := s.Append(ctx, binding("turn-3"))
	if err != nil {
		t.Fatal(err)
	}
	if _, updateErr := s.MarkProvided(ctx, p); !errors.Is(updateErr, ErrConflict) {
		t.Fatal("old retry cleared new notice")
	}
	read, err = s.Read(testSession, testOwner())
	if err != nil || len(PendingIDs(read)) != 1 || read.NoticeID != third.NoticeID {
		t.Fatal("conflict changed record")
	}
}

func TestStoreSeparatesOwnerGenerationsAndSessions(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	first, err := s.Append(ctx, binding(testTurn))
	if err != nil {
		t.Fatal(err)
	}
	newOwner := binding(testTurn)
	newOwner.Owner.Generation = "g1-00000000000000000000000000000002"
	newOwner.Owner.Birth = "boot1:101"
	second, err := s.Append(ctx, newOwner)
	if err != nil {
		t.Fatal(err)
	}
	otherSession := binding(testTurn)
	otherSession.SessionID = "00000000-0000-4000-8000-000000000002"
	third, err := s.Append(ctx, otherSession)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MarkProvided(ctx, claim(first)); err != nil {
		t.Fatal(err)
	}
	for _, record := range []Record{second, third} {
		read, readErr := s.Read(record.SessionID, record.Owner)
		if readErr != nil || len(PendingIDs(read)) != 1 || read.NoticeID != record.NoticeID {
			t.Fatal("claim for another owner/session changed a notice")
		}
	}
}

func TestStoreConcurrentAppendsKeepEveryNotice(t *testing.T) {
	s, _ := newStore(t)
	const count = 8
	var wg sync.WaitGroup
	errorsCh := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Append(context.Background(), binding(testTurn)); errorsCh <- err }()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Read(testSession, testOwner())
	if err != nil || len(PendingIDs(r)) != count || r.Revision != count {
		t.Fatalf("lost concurrent append: %v", err)
	}
	if _, err := EncodeRecord(r); err != nil {
		t.Fatal(err)
	}
}

type failingFiles struct{ recordFiles }

func (f failingFiles) Replace(string, []byte) error { return errors.New("simulated full disk") }

func TestStoreWriteFailureDoesNotAcknowledgeOrClear(t *testing.T) {
	s, root := newStore(t)
	r, err := s.Append(context.Background(), binding(testTurn))
	if err != nil {
		t.Fatal(err)
	}
	s.open = func(root string) (recordFiles, error) {
		d, openErr := fileops.OpenPrivateDir(root)
		if openErr != nil {
			return nil, openErr
		}
		return failingFiles{d}, nil
	}
	if _, updateErr := s.MarkProvided(context.Background(), claim(r)); !errors.Is(updateErr, ErrStorage) {
		t.Fatalf("failed save result: %v", updateErr)
	}
	persisted, err := NewStore(root).Read(testSession, testOwner())
	if err != nil || persisted.Revision != r.Revision || len(PendingIDs(persisted)) != 1 {
		t.Fatal("failed save modified pending")
	}
}

func TestStoreRefusesCorruptionAndUnsafePaths(t *testing.T) {
	for name, data := range map[string][]byte{
		"malformed": []byte("{"), "unknown version": []byte(`{"version":2}`),
		"oversize": bytes.Repeat([]byte("x"), MaxRecordBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			s, root := newStore(t)
			path := filepath.Join(root, recordName(testSession, testOwner()))
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Read(testSession, testOwner()); err == nil {
				t.Fatal("read accepted invalid record")
			}
			if _, err := s.Append(context.Background(), binding(testTurn)); err == nil {
				t.Fatal("append overwrote invalid record")
			}
			got, _ := os.ReadFile(path)
			if !bytes.Equal(got, data) {
				t.Fatal("invalid record overwritten")
			}
		})
	}
	s, _ := newStore(t)
	for _, id := range []string{"../escape", "/absolute", "", testSession + "/extra"} {
		if _, err := s.Read(id, testOwner()); !errors.Is(err, ErrInput) {
			t.Fatal("unsafe session accepted")
		}
	}
	r, err := s.Append(context.Background(), binding(testTurn))
	if err != nil {
		t.Fatal(err)
	}
	b := binding(testTurn)
	b.Owner.Birth = "boot1:101"
	if _, err := s.Append(context.Background(), b); !errors.Is(err, ErrConflict) {
		t.Fatal("different owner changed generation")
	}
	read, _ := s.Read(testSession, testOwner())
	if read.Revision != r.Revision {
		t.Fatal("old owner record changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Append(ctx, binding(testTurn)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled append: %v", err)
	}
	s.random = bytes.NewReader(nil)
	if _, err := s.Append(context.Background(), binding(testTurn)); !errors.Is(err, ErrStorage) {
		t.Fatal("entropy failure must not add a notice")
	}
}

func TestStoreTurnResolutionPersistsOnlyMatchingNotices(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Append(context.Background(), binding(testTurn)); err != nil {
		t.Fatal(err)
	}
	r, err := s.Append(context.Background(), binding("turn-2"))
	if err != nil {
		t.Fatal(err)
	}
	p := Terminal{
		SessionID: testSession,
		Owner:     r.Owner,
		Revision:  r.Revision,
		TurnID:    testTurn,
		FileID:    "file-1",
		Offset:    101,
	}
	next, err := s.ResolveTurn(context.Background(), p)
	if err != nil || len(PendingIDs(next)) != 1 {
		t.Fatalf("resolve: %v", err)
	}
	read, err := s.Read(testSession, testOwner())
	if err != nil || read.Revision != next.Revision || read.NoticeID != r.NoticeID {
		t.Fatal("resolution not persisted with stable notice")
	}
}
