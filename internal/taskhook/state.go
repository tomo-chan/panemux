package taskhook

func StartRecord(b Binding, id, notice string) (Record, error) {
	if !validBinding(b) || !episodeID.MatchString(id) || !noticeID.MatchString(notice) {
		return Record{}, ErrInput
	}
	r := Record{
		Version: ProtocolVersion, SessionID: b.SessionID, Owner: b.Owner, Revision: 1, NoticeID: notice,
		Episodes: []Episode{{ID: id, TurnID: b.TurnID, ToolKind: ToolBash, Disposition: Pending, Anchor: b.Anchor}},
	}
	return cloneRecord(r), nil
}

func AppendEpisode(r Record, b Binding, id, notice string) (Record, error) {
	if err := validateRecord(r); err != nil {
		return r, err
	}
	if !validBinding(b) || !episodeID.MatchString(id) || !noticeID.MatchString(notice) {
		return r, ErrInput
	}
	if r.SessionID != b.SessionID || r.Owner != b.Owner {
		return r, ErrConflict
	}
	if r.Revision == MaxRevision || liveEpisodes(r) == MaxPending {
		return r, ErrLimit
	}
	for _, ep := range r.Episodes {
		if ep.ID == id {
			return r, ErrConflict
		}
	}
	if r.NoticeID == notice {
		return r, ErrConflict
	}
	next := cloneRecord(r)
	next.Episodes = nil
	for _, ep := range r.Episodes {
		if ep.Disposition == Pending || ep.Disposition == Provisional {
			next.Episodes = append(next.Episodes, ep)
		}
	}
	next.Episodes = append(next.Episodes, Episode{
		ID: id, TurnID: b.TurnID, ToolKind: ToolBash, Disposition: Pending, Anchor: b.Anchor,
	})
	next.Revision++
	next.NoticeID = notice
	next.Receipt = nil
	return cloneRecord(next), nil
}

func PendingIDs(r Record) []string {
	ids := make([]string, 0, len(r.Episodes))
	for _, ep := range r.Episodes {
		if ep.Disposition == Pending {
			ids = append(ids, ep.ID)
		}
	}
	return ids
}

func MarkProvided(r Record, p Provided) (Record, error) {
	if err := validateRecord(r); err != nil {
		return r, err
	}
	if r.SessionID != p.SessionID || r.Owner != p.Owner || !validSet(p.EpisodeIDs) {
		return r, ErrConflict
	}
	if receipt := r.Receipt; receipt != nil && receipt.Revision == p.Revision &&
		equalSet(receipt.EpisodeIDs, p.EpisodeIDs) {

		return r, nil
	}
	if r.Revision != p.Revision || !equalSet(PendingIDs(r), p.EpisodeIDs) {
		return r, ErrConflict
	}
	if r.Revision == MaxRevision {
		return r, ErrLimit
	}
	next := cloneRecord(r)
	for i := range next.Episodes {
		switch next.Episodes[i].Disposition {
		case Pending:
			next.Episodes[i].Disposition = ProvidedDisposition
		case Provisional:
			next.Episodes[i].Disposition = Resolved
		}
	}
	next.Revision++
	next.ClosedRevision = next.Revision
	next.Receipt = &Receipt{Revision: p.Revision, EpisodeIDs: append([]string(nil), p.EpisodeIDs...)}
	return next, nil
}

func ResolveTurn(r Record, p Terminal) (Record, error) {
	if err := validateRecord(r); err != nil {
		return r, err
	}
	if r.SessionID != p.SessionID || r.Owner != p.Owner || r.Revision != p.Revision {
		return r, ErrConflict
	}
	if !opaque.MatchString(p.TurnID) || !opaque.MatchString(p.FileID) || p.Offset < 0 {
		return r, ErrInput
	}
	next := cloneRecord(r)
	changed := false
	for i, ep := range next.Episodes {
		// A provisional slot has no anchor yet. Verified same-turn evidence
		// must settle it before its slow pre-terminal scan can publish.
		if ep.Disposition == Provisional && ep.TurnID == p.TurnID {
			next.Episodes[i].Disposition = Resolved
			changed = true
		}
		if ep.Disposition == Pending && ep.TurnID == p.TurnID && ep.Anchor != nil &&
			ep.Anchor.FileID == p.FileID && p.Offset > ep.Anchor.Offset {

			next.Episodes[i].Disposition = Resolved
			changed = true
		}
	}
	if !changed {
		return r, nil
	}
	if r.Revision == MaxRevision {
		return r, ErrLimit
	}
	next.Revision++
	next.Receipt = nil
	return next, nil
}

func liveEpisodes(r Record) int {
	n := 0
	for _, ep := range r.Episodes {
		if ep.Disposition == Pending || ep.Disposition == Provisional {
			n++
		}
	}
	return n
}

func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, id := range a {
		set[id] = true
	}
	for _, id := range b {
		if !set[id] {
			return false
		}
	}
	return true
}

func cloneRecord(r Record) Record {
	r.Episodes = append([]Episode(nil), r.Episodes...)
	for i, ep := range r.Episodes {
		if ep.Anchor != nil {
			a := *ep.Anchor
			r.Episodes[i].Anchor = &a
		}
	}
	if r.Receipt != nil {
		receipt := *r.Receipt
		receipt.EpisodeIDs = append([]string(nil), receipt.EpisodeIDs...)
		r.Receipt = &receipt
	}
	return r
}
