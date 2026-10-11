package taskhook

import "context"

func ProvisionalIDs(r Record) []string {
	var ids []string
	for _, ep := range r.Episodes {
		if ep.Disposition == Provisional {
			ids = append(ids, ep.ID)
		}
	}
	return ids
}

// Cancel discards only this helper's inactive reservation after a failed proof.
// It does not invalidate other scans or modify any active notice.
func (s *Store) Cancel(ctx context.Context, token Reservation) (Record, error) {
	if !episodeID.MatchString(token.EpisodeID) {
		return Record{}, ErrInput
	}
	return s.update(ctx, token.SessionID, token.Owner, func(r Record, exists bool) (Record, error) {
		if !exists {
			return r, ErrConflict
		}
		for i, ep := range r.Episodes {
			if ep.ID != token.EpisodeID {
				continue
			}
			if ep.Disposition != Provisional {
				return r, nil
			}
			if r.Revision == MaxRevision {
				return r, ErrLimit
			}
			next := cloneRecord(r)
			next.Episodes[i].Disposition = Resolved
			next.Revision++
			next.Receipt = nil
			return next, nil
		}
		return r, ErrConflict
	})
}

// RecoverProvisional is explicit recovery after rereading an exhausted or
// abandoned ingress set. It requires the exact revision and entire provisional
// set, preserves every active episode and notice ID, and invalidates slow
// verifiers. It is not time-based resolution or a declaration of input provided.
func (s *Store) RecoverProvisional(ctx context.Context, p Provided) (Record, error) {
	return s.update(ctx, p.SessionID, p.Owner, func(r Record, exists bool) (Record, error) {
		if !exists || r.Revision != p.Revision || !validSet(p.EpisodeIDs) || !equalSet(ProvisionalIDs(r), p.EpisodeIDs) {
			return r, ErrConflict
		}
		if r.Revision == MaxRevision {
			return r, ErrLimit
		}
		next := cloneRecord(r)
		for i := range next.Episodes {
			if next.Episodes[i].Disposition == Provisional {
				next.Episodes[i].Disposition = Resolved
			}
		}
		next.Revision++
		next.ClosedRevision = next.Revision
		next.Receipt = nil
		return next, nil
	})
}
