package taskhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProtocolBudgetsAcceptExactLimitAndRejectOverflow(t *testing.T) {
	for _, item := range []struct {
		parse func([]byte) error
		data  []byte
		limit int
	}{
		{data: []byte(permissionJSON()), limit: MaxInputBytes,
			parse: func(b []byte) error { _, err := ParsePermission(b); return err }},
		{data: func() []byte { b, err := EncodeRecord(start(t)); require.NoError(t, err); return b }(), limit: MaxRecordBytes,
			parse: func(b []byte) error { _, err := DecodeRecord(b); return err }},
	} {
		exact := append(append([]byte(nil), item.data...), bytes.Repeat([]byte(" "), item.limit-len(item.data))...)
		require.NoError(t, item.parse(exact))
		require.Error(t, item.parse(append(exact, ' ')))
	}
	for _, input := range []string{`{"a"`, `{"a":`, `{true:1}`, `[`, `]`, `{"a":]}`, `[1,]`, `{"a":1}[]`} {
		var value any
		require.False(t, strictJSON([]byte(input), 100, &value), input)
	}
	for _, depth := range []int{32, 33} {
		data := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
		var value any
		require.Equal(t, depth == 32, strictJSON([]byte(data), 1000, &value))
	}
	// Every required string must reject null; agent_type is an unsupported branch too.
	var source map[string]any
	require.NoError(t, json.Unmarshal([]byte(permissionJSON()), &source))
	for key, value := range source {
		if _, ok := value.(string); !ok {
			continue
		}
		var fields map[string]any
		require.NoError(t, json.Unmarshal([]byte(permissionJSON()), &fields))
		fields[key] = nil
		data, err := json.Marshal(fields)
		require.NoError(t, err)
		_, err = ParsePermission(data)
		require.Error(t, err)
	}
	_, err := ParsePermission([]byte(strings.Replace(permissionJSON(), `"model":"model"`,
		`"model":"model","agent_type":"worker"`, 1)))
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestRecordIntegrityProtectsReceiptAndRevisionBoundaries(t *testing.T) {
	base := start(t)
	for _, mutate := range []func(*Record){
		func(r *Record) { r.ClosedRevision = r.Revision + 1 },
		func(r *Record) { r.Episodes[0].TurnID = "" },
		func(r *Record) {
			r.Episodes[0].Disposition = Provisional
			r.Episodes[0].Anchor = &Anchor{FileID: "file", Offset: 0}
		},
		func(r *Record) { r.Receipt = &Receipt{Revision: 0, EpisodeIDs: PendingIDs(*r)} },
		func(r *Record) { r.Revision = 2; r.Receipt = &Receipt{Revision: 1, EpisodeIDs: PendingIDs(*r)} },
		func(r *Record) {
			r.Revision = 2
			r.Episodes[0].Disposition = Resolved
			r.Receipt = &Receipt{Revision: 1, EpisodeIDs: []string{r.Episodes[0].ID}}
		},
		func(r *Record) { r.Episodes = append(r.Episodes, r.Episodes[0]) },
	} {
		bad := cloneRecord(base)
		mutate(&bad)
		_, err := EncodeRecord(bad)
		require.Error(t, err)
		data, err := json.Marshal(bad)
		require.NoError(t, err)
		_, err = DecodeRecord(data)
		require.Error(t, err)
		_, err = AppendEpisode(bad, binding(testTurn), "ep1-00000000000000000000000000000002",
			"h1-00000000000000000000000000000002")
		require.Error(t, err)
		_, err = MarkProvided(bad, claim(base))
		require.Error(t, err)
		_, err = ResolveTurn(bad, Terminal{})
		require.Error(t, err)
	}
	for _, revision := range []uint64{MaxRevision - 1, MaxRevision, MaxRevision + 1} {
		r := cloneRecord(base)
		r.Revision = revision
		_, err := EncodeRecord(r)
		require.Equal(t, revision > MaxRevision, err != nil)
	}
	// Boundary owner values are accepted; negative UID and zero PID never bind.
	owner := testOwner()
	owner.UID = 0
	owner.PID = 1
	require.True(t, validOwner(owner))
	owner.UID = -1
	require.False(t, validOwner(owner))
	owner.UID = 0
	owner.PID = 0
	require.False(t, validOwner(owner))
}

func TestRecordMutationsFailWithoutChangingSnapshot(t *testing.T) {
	r := start(t)
	badBinding := binding(testTurn)
	badBinding.TurnID = ""
	_, err := StartRecord(badBinding, r.Episodes[0].ID, r.NoticeID)
	require.ErrorIs(t, err, ErrInput)
	wrongOwner := binding(testTurn)
	wrongOwner.Owner.Birth = "other"
	for _, b := range []Binding{badBinding, wrongOwner} {
		got, changeErr := AppendEpisode(r, b, "ep1-00000000000000000000000000000002",
			"h1-00000000000000000000000000000002")
		require.Error(t, changeErr)
		require.Equal(t, r, got)
	}
	got, err := AppendEpisode(r, binding(testTurn), "ep1-00000000000000000000000000000002", r.NoticeID)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, r, got)
	for _, offset := range []int64{-1, 0, 99, 100, 101} {
		terminal := Terminal{Owner: r.Owner, SessionID: r.SessionID, Revision: r.Revision,
			TurnID: testTurn, FileID: "file-1", Offset: offset}
		got, err = ResolveTurn(r, terminal)
		if offset < 0 {
			require.ErrorIs(t, err, ErrInput)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, offset > 100, len(PendingIDs(got)) == 0)
	}
	r.Revision = MaxRevision
	for _, op := range []func() (Record, error){
		func() (Record, error) {
			return AppendEpisode(r, binding(testTurn), "ep1-00000000000000000000000000000002",
				"h1-00000000000000000000000000000002")
		},
		func() (Record, error) { return MarkProvided(r, claim(r)) },
		func() (Record, error) {
			return ResolveTurn(r, Terminal{Owner: r.Owner, SessionID: r.SessionID, Revision: r.Revision,
				TurnID: testTurn, FileID: "file-1", Offset: 101})
		},
	} {
		got, err = op()
		require.ErrorIs(t, err, ErrLimit)
		require.Equal(t, r, got)
	}
}

func TestProvisionalRecoveryAndFinishAtRevisionLimit(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	token, _, err := s.Reserve(ctx, binding(testTurn))
	require.NoError(t, err)
	// A veto fences unfinished reservations for the same turn.
	_, _, err = s.Reserve(ctx, binding(testTurn))
	require.NoError(t, err)
	closed, err := s.Finish(ctx, token, binding(testTurn), true)
	require.NoError(t, err)
	require.Empty(t, ProvisionalIDs(closed))
	require.Empty(t, PendingIDs(closed))
	token, r, err := s.Reserve(ctx, binding(testTurn))
	require.NoError(t, err)
	r.Revision = MaxRevision
	_, err = s.update(ctx, r.SessionID, r.Owner, func(Record, bool) (Record, error) { return r, nil })
	require.NoError(t, err)
	for _, op := range []func() (Record, error){
		func() (Record, error) { return s.Cancel(ctx, token) },
		func() (Record, error) {
			return s.RecoverProvisional(ctx, Provided{Owner: r.Owner, SessionID: r.SessionID,
				Revision: r.Revision, EpisodeIDs: ProvisionalIDs(r)})
		},
		func() (Record, error) { return s.Finish(ctx, token, binding(testTurn), false) },
	} {
		_, err = op()
		require.ErrorIs(t, err, ErrLimit)
	}
	read, err := s.Read(r.SessionID, r.Owner)
	require.NoError(t, err)
	require.Equal(t, r, read)
	absent := token
	absent.EpisodeID = "ep1-00000000000000000000000000000000"
	_, err = s.Cancel(ctx, absent)
	require.ErrorIs(t, err, ErrConflict)
	_, err = s.Finish(ctx, absent, binding(testTurn), false)
	require.ErrorIs(t, err, ErrConflict)
	token.EpisodeID = "invalid"
	_, err = s.Cancel(ctx, token)
	require.ErrorIs(t, err, ErrInput)
}

func TestMaximumRecordShapeStaysWithinPersistenceBudget(t *testing.T) {
	r := start(t)
	r.Owner.Birth = strings.Repeat("x", 128)
	r.Owner.PID = int(^uint(0) >> 1)
	r.Owner.UID = int(^uint(0) >> 1)
	r.Episodes = nil
	for i := 0; i < MaxPending; i++ {
		r.Episodes = append(r.Episodes, Episode{ID: fmt.Sprintf("ep1-%032x", i),
			TurnID: strings.Repeat("x", 128), ToolKind: ToolBash, Disposition: Pending,
			Anchor: &Anchor{FileID: strings.Repeat("x", 128), Offset: 1<<63 - 1}})
	}
	r.Revision = MaxRevision
	data, err := EncodeRecord(r)
	require.NoError(t, err)
	require.Less(t, len(data), MaxRecordBytes)
	decoded, err := DecodeRecord(data)
	require.NoError(t, err)
	require.True(t, reflect.DeepEqual(r, decoded))
	// Include the largest receipt too, while retaining the longest anchors.
	r.Receipt = &Receipt{Revision: r.Revision - 1}
	for i := range r.Episodes {
		r.Episodes[i].Disposition = ProvidedDisposition
		r.Receipt.EpisodeIDs = append(r.Receipt.EpisodeIDs, r.Episodes[i].ID)
	}
	data, err = EncodeRecord(r)
	require.NoError(t, err)
	require.Less(t, len(data), MaxRecordBytes)
	// The receipt's IDs are bounded independently of record decoding.
	ids := make([]string, 0, MaxPending)
	for _, ep := range r.Episodes {
		ids = append(ids, ep.ID)
	}
	require.True(t, validSet(ids))
	require.False(t, validSet(append(ids, "ep1-00000000000000000000000000000010")))
}

func TestTerminalClosesInactiveReservationsAndPreservesNoticeIdentity(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	r, err := s.Append(ctx, binding(testTurn))
	require.NoError(t, err)
	notice := r.NoticeID
	token, r, err := s.Reserve(ctx, binding(testTurn))
	require.NoError(t, err)
	terminal := Terminal{Owner: r.Owner, SessionID: r.SessionID, Revision: r.Revision,
		TurnID: testTurn, FileID: "file-1", Offset: 101}
	closed, err := s.ResolveTurn(ctx, terminal)
	require.NoError(t, err)
	require.Empty(t, ProvisionalIDs(closed))
	require.Empty(t, PendingIDs(closed))
	require.Equal(t, notice, closed.NoticeID)
	_, err = s.Finish(ctx, token, binding(testTurn), false)
	require.Error(t, err)
	r = start(t)
	_, err = AppendEpisode(r, binding(testTurn), r.Episodes[0].ID, "h1-00000000000000000000000000000002")
	require.ErrorIs(t, err, ErrConflict)
	missing, _ := newStore(t)
	_, err = missing.Cancel(ctx, token)
	require.ErrorIs(t, err, ErrConflict)
}

func TestMetadataDecoderRejectsTrailingTypedAndPartialFragments(t *testing.T) {
	var item rolloutMetadata
	for _, data := range []string{`{} {}`, `{"type":7}`, `{"type":"turn_context","payload":{"turn_id":""}}`} {
		if strings.Contains(data, `"turn_id":""`) {
			_, err := ScanRollout(strings.NewReader(metadataLog(data)),
				Permission{SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash}, "file")
			require.Error(t, err)
		} else {
			require.False(t, decodeMetadata([]byte(data), &item))
		}
	}
	d := json.NewDecoder(strings.NewReader("[]"))
	_, err := d.Token()
	require.NoError(t, err)
	require.False(t, jsonValue(d, 0), "a closing delimiter cannot start a value")
}

func TestRolloutCapacityUsesCompletePhysicalLineOffsets(t *testing.T) {
	p := Permission{SessionID: testSession, TurnID: testTurn, ToolKind: ToolBash}
	_, err := ScanRollout(strings.NewReader(""), Permission{}, "file")
	require.ErrorIs(t, err, ErrInput)
	baseline := metadataLog(`{"type":"turn_context","payload":{"turn_id":"` + testTurn + `","approvals_reviewer":"user"}}`)
	for _, size := range []int{MaxRolloutLineBytes - 1, MaxRolloutLineBytes} {
		line := "{" + strings.Repeat(" ", size-2) + "}\n"
		_, err = ScanRollout(strings.NewReader(baseline+line), p, "file")
		if size < MaxRolloutLineBytes {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	var input strings.Builder
	input.WriteString(baseline)
	for MaxRolloutBytes-input.Len() > 60000 {
		input.WriteString("{" + strings.Repeat(" ", 59997) + "}\n")
	}
	remaining := MaxRolloutBytes - input.Len()
	input.WriteString("{" + strings.Repeat(" ", remaining-3) + "}\n")
	require.Equal(t, MaxRolloutBytes, input.Len())
	evidence, err := ScanRollout(strings.NewReader(input.String()), p, "file")
	require.NoError(t, err)
	require.Equal(t, int64(MaxRolloutBytes), evidence.Anchor.Offset)
	_, err = ScanRollout(strings.NewReader(input.String()+"\n"), p, "file")
	require.Error(t, err)
}
