package taskhook

import (
	"context"
	"encoding/hex"
	"io"
)

// Reservation is a one-use ingress token. The caller must already have verified
// the process owner before Reserve, and must verify the rollout and owner again
// before Finish. The unverified turn never appears in PendingIDs.
type Reservation struct {
	SessionID      string
	TurnID         string
	EpisodeID      string
	NoticeID       string
	Owner          Owner
	ClosedRevision uint64
}

// Reserve commits before the slow eligibility scan. Other ingress reservations
// may finish in either order. Input provided invalidates all older scans;
// terminal evidence invalidates only its turn. A crash leaves an inactive slot.
func (s *Store) Reserve(ctx context.Context, b Binding) (Reservation, Record, error) {
	if !validBinding(b) {
		return Reservation{}, Record{}, ErrInput
	}
	var random [32]byte
	if _, err := io.ReadFull(s.random, random[:]); err != nil {
		return Reservation{}, Record{}, ErrStorage
	}
	token := Reservation{SessionID: b.SessionID, TurnID: b.TurnID, Owner: b.Owner,
		EpisodeID: "ep1-" + hex.EncodeToString(random[:16]), NoticeID: "h1-" + hex.EncodeToString(random[16:])}
	b.Anchor = nil
	r, err := s.update(ctx, b.SessionID, b.Owner, func(old Record, exists bool) (Record, error) {
		var next Record
		var changeErr error
		if exists {
			next, changeErr = AppendEpisode(old, b, token.EpisodeID, token.NoticeID)
		} else {
			next, changeErr = StartRecord(b, token.EpisodeID, token.NoticeID)
		}
		if changeErr != nil {
			return old, changeErr
		}
		next.Episodes[len(next.Episodes)-1].Disposition = Provisional
		if exists {
			next.NoticeID = old.NoticeID
		}
		return next, nil
	})
	if err != nil {
		return Reservation{}, r, err
	}
	token.ClosedRevision = r.ClosedRevision
	return token, r, nil
}

// Finish publishes only a verified same-owner/session/turn binding. Veto is
// positive evidence of an exact-turn terminal (including before the watermark)
// or a clearly later turn; missing context or partial scans are not evidence.
// Unknown eligibility should leave the reservation inactive, never publish it.
func (s *Store) Finish(ctx context.Context, token Reservation, b Binding, veto bool) (Record, error) {
	if !validBinding(b) || b.Anchor == nil {
		return Record{}, ErrInput
	}
	if token.Owner != b.Owner || token.SessionID != b.SessionID || token.TurnID != b.TurnID ||
		!episodeID.MatchString(token.EpisodeID) || !noticeID.MatchString(token.NoticeID) {

		return Record{}, ErrConflict
	}
	return s.update(ctx, b.SessionID, b.Owner, func(r Record, exists bool) (Record, error) {
		if !exists || r.ClosedRevision != token.ClosedRevision {
			return r, ErrConflict
		}
		for i, ep := range r.Episodes {
			if ep.ID != token.EpisodeID {
				continue
			}
			if ep.Disposition != Provisional || ep.TurnID != token.TurnID {
				return r, ErrConflict
			}
			if r.Revision == MaxRevision {
				return r, ErrLimit
			}
			next := cloneRecord(r)
			next.Revision++
			next.Receipt = nil
			next.Episodes[i].Anchor = b.Anchor
			if veto {
				next.Episodes[i].Disposition = Resolved
				for j := range next.Episodes {
					if next.Episodes[j].Disposition == Provisional && next.Episodes[j].TurnID == token.TurnID {
						next.Episodes[j].Disposition = Resolved
					}
				}
			} else {
				next.Episodes[i].Disposition = Pending
				next.NoticeID = token.NoticeID
			}
			return cloneRecord(next), nil
		}
		return r, ErrConflict
	})
}
