package taskhook

import (
	"context"
	"errors"
	"testing"
)

func TestRecoveryReleasesOnlyInactiveCapacityAfterCrash(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	active, err := s.Append(ctx, binding(testTurn))
	if err != nil {
		t.Fatal(err)
	}
	var tokens []Reservation
	var r Record
	for range MaxPending - 1 {
		var token Reservation
		token, r, err = s.Reserve(ctx, binding(testTurn))
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	if _, _, err = s.Reserve(ctx, binding(testTurn)); !errors.Is(err, ErrLimit) {
		t.Fatal("capacity unbounded")
	}
	s = NewStore(root)
	p := Provided{SessionID: r.SessionID, Owner: r.Owner, Revision: r.Revision, EpisodeIDs: ProvisionalIDs(r)}
	wrong := p
	wrong.EpisodeIDs = wrong.EpisodeIDs[:1]
	if _, err = s.RecoverProvisional(ctx, wrong); !errors.Is(err, ErrConflict) {
		t.Fatal("partial recovery accepted")
	}
	wrong = p
	wrong.EpisodeIDs = PendingIDs(r)
	if _, err = s.RecoverProvisional(ctx, wrong); !errors.Is(err, ErrConflict) {
		t.Fatal("recovery cleared active notice")
	}
	got, err := s.RecoverProvisional(ctx, p)
	if err != nil || len(PendingIDs(got)) != 1 || PendingIDs(got)[0] != PendingIDs(active)[0] ||
		got.NoticeID != active.NoticeID {

		t.Fatalf("recovery changed active notice: %v", err)
	}
	for _, token := range tokens {
		if _, err = s.Finish(ctx, token, binding(testTurn), false); !errors.Is(err, ErrConflict) {
			t.Fatal("recovered token revived")
		}
	}
	if _, _, err = s.Reserve(ctx, binding(testTurn)); err != nil {
		t.Fatalf("capacity not recovered: %v", err)
	}
	if _, err = s.RecoverProvisional(ctx, p); !errors.Is(err, ErrConflict) {
		t.Fatal("old recovery cleared new ingress")
	}
}
