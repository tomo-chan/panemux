package tasks

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"panemux/internal/fileops"
	"panemux/internal/homedir"
)

// What a person records about a task on the dashboard — that it is done, and
// the labels it carries (issue #256). Nothing about a task's process says
// whether its work is finished, so "done" is only ever what a person set.
//
// Records live on the panemux host in one file, whichever host the task runs
// on, and are kept until a person clears them: a task that has left the
// dashboard's list (its conversation log deleted, or older than the listing
// window) keeps its record, which applies again if the session is listed
// again. Only tasks with a session ID can carry a record; a pid is reused
// after the process exits, so a record keyed by one would move to an
// unrelated task.

const (
	recordsFileName = "tasks.json"
	// recordsFileMode keeps the file private to the operator, like every
	// other file panemux writes under ~/.config/panemux.
	recordsFileMode os.FileMode = 0600
	// recordsFileVersion is the only format this build reads or writes. A
	// file with another version is refused rather than overwritten.
	recordsFileVersion = 1

	// MaxLabels is how many labels one task can carry.
	MaxLabels = 20
	// MaxLabelLength is the longest label, in characters.
	MaxLabelLength = 32
)

// recordAgents are the agents the dashboard lists tasks for.
var recordAgents = map[string]bool{AgentClaude: true, AgentCodex: true}

// ErrInvalidRecord wraps every reason Put refuses a record for what it
// contains rather than for failing to store it.
var ErrInvalidRecord = errors.New("invalid task record")

// RecordKey identifies the task a record belongs to. The same session ID on
// two hosts, or under two agents, is two tasks.
type RecordKey struct {
	Host      string
	Agent     string
	SessionID string
}

// Record is what a person recorded about one task.
type Record struct {
	// Host is the ssh_connections key, or "" for the panemux host.
	Host      string   `json:"host"`
	Agent     string   `json:"agent"`
	SessionID string   `json:"session_id"`
	Labels    []string `json:"labels,omitempty"`
	Done      bool     `json:"done,omitempty"`
}

// Key is the task the record belongs to.
func (r Record) Key() RecordKey {
	return RecordKey{Host: r.Host, Agent: r.Agent, SessionID: r.SessionID}
}

func (r Record) isEmpty() bool {
	return !r.Done && len(r.Labels) == 0
}

func (r Record) clone() Record {
	if r.Labels != nil {
		r.Labels = append([]string(nil), r.Labels...)
	}
	return r
}

type recordsFile struct {
	Records []Record `json:"records"`
	Version int      `json:"version"`
}

// RecordStore reads and writes the task record file. It serves from memory
// while the file's modification time and size are what it last read or
// wrote, and reads the file again when they are not, so an edit made by hand
// while panemux runs is neither hidden nor undone by the next save. A file it
// could not read is tried again on the next use, and until it is read every
// write fails, so a file it does not understand is never replaced. A symlink
// at the path is written through, not replaced.
type RecordStore struct {
	records map[RecordKey]Record
	path    string
	// seen is the file as it was when records was read or last written.
	seen recordsFileStamp
	mu   sync.Mutex
}

// recordsFileStamp is what tells an unchanged file from one edited since.
type recordsFileStamp struct {
	modTime time.Time
	size    int64
	exists  bool
}

func statRecordsFile(path string) (recordsFileStamp, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return recordsFileStamp{}, nil
	}
	if err != nil {
		return recordsFileStamp{}, fmt.Errorf("reading task record file: %w", err)
	}
	return recordsFileStamp{modTime: info.ModTime(), size: info.Size(), exists: true}, nil
}

// NewRecordStore returns a store for the file at path, or for
// DefaultRecordsPath when path is "".
func NewRecordStore(path string) *RecordStore {
	return &RecordStore{path: path}
}

// DefaultRecordsPath returns ~/.config/panemux/tasks.json.
func DefaultRecordsPath() (string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config", "panemux", recordsFileName), nil
}

// Records returns every record, keyed by task. The map and its records are
// the caller's to change.
func (s *RecordStore) Records() (map[RecordKey]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	out := make(map[RecordKey]Record, len(s.records))
	for key, rec := range s.records {
		out[key] = rec.clone()
	}
	return out, nil
}

// Put replaces the record of rec's task with rec, after normalizing its
// labels, and writes the file. A record that is neither done nor labeled is
// removed. It returns the record as stored.
func (s *RecordStore) Put(rec Record) (Record, error) {
	if err := validateRecordKey(rec.Key()); err != nil {
		return Record{}, err
	}
	labels, err := NormalizeLabels(rec.Labels)
	if err != nil {
		return Record{}, err
	}
	rec.Labels = labels

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return Record{}, err
	}
	next := maps.Clone(s.records)
	if rec.isEmpty() {
		delete(next, rec.Key())
	} else {
		next[rec.Key()] = rec.clone()
	}
	if err := s.writeLocked(next); err != nil {
		return Record{}, err
	}
	s.records = next
	return rec.clone(), nil
}

func (s *RecordStore) resolvePathLocked() (string, error) {
	if s.path == "" {
		path, err := DefaultRecordsPath()
		if err != nil {
			return "", err
		}
		s.path = path
	}
	return s.path, nil
}

func (s *RecordStore) loadLocked() error {
	path, err := s.resolvePathLocked()
	if err != nil {
		return err
	}
	stamp, err := statRecordsFile(path)
	if err != nil {
		s.records = nil
		return err
	}
	if s.records != nil && stamp == s.seen {
		return nil
	}
	records, err := readRecordsFile(path)
	if err != nil {
		s.records = nil
		return err
	}
	s.records, s.seen = records, stamp
	return nil
}

func readRecordsFile(path string) (map[RecordKey]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[RecordKey]Record{}, nil
		}
		return nil, fmt.Errorf("reading task record file: %w", err)
	}
	var file recordsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing task record file: %w", err)
	}
	if file.Version != recordsFileVersion {
		return nil, fmt.Errorf("unsupported task record file version %d", file.Version)
	}
	records := make(map[RecordKey]Record, len(file.Records))
	for _, rec := range file.Records {
		if err := validateRecordKey(rec.Key()); err != nil {
			return nil, fmt.Errorf("task record file: %w", err)
		}
		if _, dup := records[rec.Key()]; dup {
			return nil, fmt.Errorf("task record file: duplicate task record for %s session %s", rec.Agent, rec.SessionID)
		}
		labels, err := NormalizeLabels(rec.Labels)
		if err != nil {
			return nil, fmt.Errorf("task record file: %w", err)
		}
		rec.Labels = labels
		records[rec.Key()] = rec
	}
	return records, nil
}

func (s *RecordStore) writeLocked(records map[RecordKey]Record) error {
	file := recordsFile{Version: recordsFileVersion, Records: make([]Record, 0, len(records))}
	for _, rec := range records {
		file.Records = append(file.Records, rec)
	}
	slices.SortFunc(file.Records, func(a, b Record) int {
		return cmp.Or(
			strings.Compare(a.Host, b.Host), strings.Compare(a.Agent, b.Agent), strings.Compare(a.SessionID, b.SessionID),
		)
	})
	data, err := json.Marshal(file)
	//coverage:exempt a struct of strings, bools and string slices always marshals
	if err != nil {
		return fmt.Errorf("encoding task record file: %w", err)
	}
	target := resolveRecordsWriteTarget(s.path)
	if err = fileops.AtomicWrite(target, data, recordsFileMode, "task record file"); err != nil {
		return err
	}
	stamp, err := statRecordsFile(s.path)
	//coverage:exempt the file was written a moment ago; only a race with its removal fails here
	if err != nil {
		return err
	}
	s.seen = stamp
	return nil
}

// resolveRecordsWriteTarget is the file a write to path should replace: the
// target of a symlink at path, even one that does not exist yet, so the link
// survives the rename AtomicWrite does. internal/config's resolveWriteTarget
// does the same for config.yaml.
func resolveRecordsWriteTarget(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	dest, err := os.Readlink(path)
	if err != nil {
		return path
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(path), dest)
	}
	return dest
}

func validateRecordKey(key RecordKey) error {
	if !validSessionID.MatchString(key.SessionID) {
		return fmt.Errorf("%w: invalid session ID %q", ErrInvalidRecord, key.SessionID)
	}
	if !recordAgents[key.Agent] {
		return fmt.Errorf("%w: unknown agent %q", ErrInvalidRecord, key.Agent)
	}
	return nil
}

// NormalizeLabels trims each label and drops repeats, keeping the order the
// labels were given in. It refuses a label that is empty, longer than
// MaxLabelLength characters, not UTF-8, or carrying a control character, an
// invisible format character or a line or paragraph separator, and more than
// MaxLabels distinct labels. No labels is nil.
func NormalizeLabels(labels []string) ([]string, error) {
	var out []string
	seen := make(map[string]bool, len(labels))
	for _, raw := range labels {
		label := strings.TrimSpace(raw)
		if err := validateLabel(label); err != nil {
			return nil, err
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	if len(out) > MaxLabels {
		return nil, fmt.Errorf("%w: more than %d labels", ErrInvalidRecord, MaxLabels)
	}
	return out, nil
}

func validateLabel(label string) error {
	if label == "" {
		return fmt.Errorf("%w: label is empty", ErrInvalidRecord)
	}
	if !utf8.ValidString(label) {
		return fmt.Errorf("%w: label %q is not valid UTF-8", ErrInvalidRecord, label)
	}
	if utf8.RuneCountInString(label) > MaxLabelLength {
		return fmt.Errorf("%w: label %q is longer than %d characters", ErrInvalidRecord, label, MaxLabelLength)
	}
	if strings.IndexFunc(label, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: label %q contains a control character", ErrInvalidRecord, label)
	}
	// Format characters (zero-width spaces, direction overrides) and line or
	// paragraph separators would make a label look empty, look like another
	// label, or reorder the text shown after it.
	if strings.IndexFunc(label, isInvisibleFormatting) >= 0 {
		return fmt.Errorf("%w: label %q contains an invisible format character", ErrInvalidRecord, label)
	}
	return nil
}

func isInvisibleFormatting(r rune) bool {
	return unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
}
