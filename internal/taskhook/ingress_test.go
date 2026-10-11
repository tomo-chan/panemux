package taskhook

import (
	"context"
	"errors"
	"testing"
)

func TestIngressCannotPublishAcrossProvided(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	first, err := s.Append(ctx, binding(testTurn))
	if err != nil {
		t.Fatal(err)
	}
	token, reserved, err := s.Reserve(ctx, binding(testTurn))
	if err != nil {
		t.Fatal(err)
	}
	if len(PendingIDs(reserved)) != 1 || reserved.NoticeID != first.NoticeID {
		t.Fatal("unverified ingress changed the active notice")
	}
	if _, err = s.MarkProvided(ctx, claim(first)); !errors.Is(err, ErrConflict) {
		t.Fatal("pre-ingress snapshot cleared a concurrent request")
	}
	provided, err := s.MarkProvided(ctx, claim(reserved))
	if err != nil {
		t.Fatal(err)
	}
	s = NewStore(root)
	if _, err = s.Finish(ctx, token, binding(testTurn), false); !errors.Is(err, ErrConflict) {
		t.Fatal("slow eligibility scan resurrected an acknowledged notice")
	}
	got, err := s.Read(testSession, testOwner())
	if err != nil || got.Revision != provided.Revision || len(PendingIDs(got)) != 0 {
		t.Fatal("stale completion changed durable state")
	}
}

func TestConcurrentIngressPreservesEveryEligibleEpisode(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	a, _, err := s.Reserve(ctx, binding(testTurn))
	if err != nil {
		t.Fatal(err)
	}
	b, reserved, err := s.Reserve(ctx, binding("turn-2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(PendingIDs(reserved)) != 0 {
		t.Fatal("provisional ingress is active")
	}
	if _, err = s.Finish(ctx, b, binding("turn-2"), false); err != nil {
		t.Fatal(err)
	}
	got, err := s.Finish(ctx, a, binding(testTurn), false)
	if err != nil || len(PendingIDs(got)) != 2 {
		t.Fatalf("lost concurrent episode: %v", err)
	}
	if _, err = s.Finish(ctx, a, binding(testTurn), false); !errors.Is(err, ErrConflict) {
		t.Fatal("replayed reservation created another notice")
	}
}

func TestIngressVetoAndBindingConflict(t *testing.T) {
	for _, name := range []string{
		"terminal before watermark", "later turn", "owner changed", "session changed", "turn changed", "missing anchor",
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := newStore(t)
			ctx := context.Background()
			token, _, err := s.Reserve(ctx, binding(testTurn))
			if err != nil {
				t.Fatal(err)
			}
			verified := binding(testTurn)
			veto := false
			switch name {
			case "terminal before watermark", "later turn":
				veto = true
			case "owner changed":
				verified.Owner.Birth = "boot1:999"
			case "session changed":
				verified.SessionID = "00000000-0000-4000-8000-000000000002"
			case "turn changed":
				verified.TurnID = "other-turn"
			case "missing anchor":
				verified.Anchor = nil
			}
			got, finishErr := s.Finish(ctx, token, verified, veto)
			if veto && finishErr != nil {
				t.Fatal(finishErr)
			}
			if !veto && finishErr == nil {
				t.Fatal("unverified tuple accepted")
			}
			if len(PendingIDs(got)) != 0 {
				t.Fatal("veto or mismatch made a notice active")
			}
			if veto {
				if _, err = s.Finish(ctx, token, binding(testTurn), false); !errors.Is(err, ErrConflict) {
					t.Fatal("closed reservation revived")
				}
			}
		})
	}
}
