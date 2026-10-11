package taskhook

import (
	"errors"
	"fmt"
	"testing"
)

func binding(turn string) Binding {
	return Binding{
		SessionID: testSession, Owner: testOwner(), TurnID: turn, Anchor: &Anchor{FileID: "file-1", Offset: 100},
	}
}
func start(t *testing.T) Record {
	t.Helper()
	r, err := StartRecord(binding(testTurn),
		"ep1-00000000000000000000000000000001", "h1-00000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func appendNotice(t *testing.T, r Record, turn string, n int) Record {
	t.Helper()
	r2, err := AppendEpisode(r, binding(turn), fmt.Sprintf("ep1-%032x", n), fmt.Sprintf("h1-%032x", n))
	if err != nil {
		t.Fatal(err)
	}
	return r2
}
func claim(r Record) Provided {
	return Provided{SessionID: r.SessionID, Owner: r.Owner, Revision: r.Revision, EpisodeIDs: PendingIDs(r)}
}

func TestNoticeSetCompareAndUpdate(t *testing.T) {
	first := start(t)
	r := appendNotice(t, first, "turn-2", 2)
	if len(PendingIDs(r)) != 2 || r.Revision != 2 || r.NoticeID == first.NoticeID || len(first.Episodes) != 1 {
		t.Fatal("new notice must add, revise, and leave old snapshot unchanged")
	}
	for name, mutate := range map[string]func(*Provided){
		"old revision":     func(p *Provided) { p.Revision-- },
		"wrong generation": func(p *Provided) { p.Owner.Generation = "g1-00000000000000000000000000000002" },
		"reused pid":       func(p *Provided) { p.Owner.Birth = "boot1:101" },
		"wrong owner":      func(p *Provided) { p.Owner.PID++ },
		"wrong session":    func(p *Provided) { p.SessionID = "00000000-0000-4000-8000-000000000002" },
		"subset":           func(p *Provided) { p.EpisodeIDs = p.EpisodeIDs[:1] },
		"duplicate":        func(p *Provided) { p.EpisodeIDs = []string{p.EpisodeIDs[0], p.EpisodeIDs[0]} },
		"unknown id":       func(p *Provided) { p.EpisodeIDs[0] = "ep1-00000000000000000000000000000003" },
	} {
		t.Run(name, func(t *testing.T) {
			p := claim(r)
			mutate(&p)
			got, err := MarkProvided(r, p)
			if err == nil || len(PendingIDs(got)) != 2 {
				t.Fatalf("partial/stale claim modified notifications: %v", err)
			}
		})
	}
	p := claim(r)
	p.EpisodeIDs[0], p.EpisodeIDs[1] = p.EpisodeIDs[1], p.EpisodeIDs[0]
	provided, err := MarkProvided(r, p)
	if err != nil || len(PendingIDs(provided)) != 0 || provided.Revision != 3 {
		t.Fatalf("claim: %v", err)
	}
	if len(PendingIDs(r)) != 2 {
		t.Fatal("changed caller snapshot")
	}
	again, err := MarkProvided(provided, p)
	if err != nil || again.Revision != provided.Revision {
		t.Fatalf("retry must be idempotent: %v", err)
	}
	newer := appendNotice(t, provided, "turn-3", 3)
	got, err := MarkProvided(newer, p)
	if !errors.Is(err, ErrConflict) || len(PendingIDs(got)) != 1 {
		t.Fatal("retry cleared a new notice")
	}
	got, err = MarkProvided(newer, claim(r))
	if !errors.Is(err, ErrConflict) || len(PendingIDs(got)) != 1 {
		t.Fatal("older tab cleared a new notice")
	}
}

func TestTerminalEvidenceOnlyClosesItsTurn(t *testing.T) {
	r := appendNotice(t, start(t), "turn-2", 2)
	request := Terminal{
		SessionID: r.SessionID,
		Owner:     r.Owner,
		Revision:  r.Revision,
		TurnID:    testTurn,
		FileID:    "file-1",
		Offset:    200,
	}
	for name, mutate := range map[string]func(*Terminal){
		"generation": func(p *Terminal) { p.Owner.Generation = "g1-00000000000000000000000000000002" },
		"revision":   func(p *Terminal) { p.Revision-- }, "birth": func(p *Terminal) { p.Owner.Birth = "boot1:101" },
		"session": func(p *Terminal) { p.SessionID = "00000000-0000-4000-8000-000000000002" },
	} {
		t.Run(name, func(t *testing.T) {
			p := request
			mutate(&p)
			got, err := ResolveTurn(r, p)
			if !errors.Is(err, ErrConflict) || len(PendingIDs(got)) != 2 {
				t.Fatal("stale evidence cleared notifications")
			}
		})
	}
	for name, mutate := range map[string]func(*Terminal){
		"other turn":   func(p *Terminal) { p.TurnID = "turn-3" },
		"rotated file": func(p *Terminal) { p.FileID = "file-2" },
		"before hook":  func(p *Terminal) { p.Offset = 99 },
		"at watermark": func(p *Terminal) { p.Offset = 100 },
	} {
		t.Run(name, func(t *testing.T) {
			p := request
			mutate(&p)
			got, err := ResolveTurn(r, p)
			if err != nil || len(PendingIDs(got)) != 2 || got.Revision != r.Revision {
				t.Fatal("insufficient evidence resolved")
			}
		})
	}
	got, err := ResolveTurn(r, request)
	if err != nil || len(PendingIDs(got)) != 1 || got.Episodes[0].Disposition != Resolved ||
		got.NoticeID != r.NoticeID || got.Revision != r.Revision+1 {

		t.Fatalf("turn resolution: %v", err)
	}
	r.Episodes[0].Anchor = nil
	got, err = ResolveTurn(r, request)
	if err != nil || len(PendingIDs(got)) != 2 {
		t.Fatal("missing anchor inferred completion")
	}
}

func TestPendingLimitAndCompaction(t *testing.T) {
	r := start(t)
	for n := 2; n <= MaxPending; n++ {
		r = appendNotice(t, r, testTurn, n)
	}
	got, err := AppendEpisode(r, binding(testTurn),
		"ep1-000000000000000000000000000000ff", "h1-000000000000000000000000000000ff")
	if !errors.Is(err, ErrLimit) || len(PendingIDs(got)) != MaxPending {
		t.Fatal("overflow silently replaced pending")
	}
	provided, err := MarkProvided(r, claim(r))
	if err != nil {
		t.Fatal(err)
	}
	next := appendNotice(t, provided, "turn-2", MaxPending+1)
	if len(next.Episodes) != 1 || next.Revision != provided.Revision+1 {
		t.Fatal("settled episodes should not consume pending capacity")
	}
	wrong := binding(testTurn)
	wrong.Owner.Birth = "boot1:101"
	_, err = AppendEpisode(next, wrong,
		"ep1-000000000000000000000000000000fe", "h1-000000000000000000000000000000fe")
	if !errors.Is(err, ErrConflict) {
		t.Fatal("new owner silently overwrote prior generation")
	}
}
