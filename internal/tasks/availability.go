package tasks

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"panemux/internal/agent"
)

// AvailabilityStatus is whether panemux can use one capability of an agent
// on a host.
type AvailabilityStatus string

// Availability statuses.
const (
	Available           AvailabilityStatus = "available"
	Degraded            AvailabilityStatus = "degraded"
	Unavailable         AvailabilityStatus = "unavailable"
	AvailabilityUnknown AvailabilityStatus = "unknown"
)

// AvailabilityReason is why a capability is not simply available.
type AvailabilityReason string

// Availability reasons. ReasonAuthMissing and ReasonSessionInfoMissing are
// defined for the stages that report them (#348); nothing reports them yet.
const (
	// ReasonCLIMissing: the agent's CLI is on neither PATH nor a login
	// shell's PATH, so it cannot be launched or resumed.
	ReasonCLIMissing AvailabilityReason = "cli_missing"
	// ReasonUnknownFormat: some of what the agent wrote is in a format this
	// build does not know; its sessions are listed with their state unknown.
	ReasonUnknownFormat AvailabilityReason = "unknown_format"
	// ReasonNotProbed: nothing has been found out yet.
	ReasonNotProbed AvailabilityReason = "not_probed"
	// ReasonProbeFailed: the probe did not run or did not answer.
	ReasonProbeFailed        AvailabilityReason = "probe_failed"
	ReasonAuthMissing        AvailabilityReason = "auth_missing"
	ReasonSessionInfoMissing AvailabilityReason = "session_info_missing"
)

// Availability is whether one capability of one agent can be used on a host
// (issue #373). It is internal: the API does not carry it yet.
type Availability struct {
	Agent      agent.ID
	Capability agent.Capability
	Status     AvailabilityStatus
	Reason     AvailabilityReason
	// Detail says what was found, bounded and without control characters.
	Detail string
	// ObservedVersion is the agent's version as its newest session
	// recorded it. It is recorded, never checked.
	ObservedVersion string
}

const (
	// probeTTL is how long a host's probe result is used before the next
	// full collection probes again; a new connection probes at once.
	probeTTL = 5 * time.Minute
	// probeTimeout bounds one probe, apart from the collection's own time.
	probeTimeout = 5 * time.Second
	probeHeader  = "::panemux-probe v1"
	probeEnd     = "::end"
	probeFound   = "found"
	probeMissing = "missing"
)

// probeAgents are the agents panemux can launch or resume: the CLIs the
// probe looks for.
var probeAgents = launchableAgents(agent.All())

// probeScript looks for each probeAgents CLI the way the launch script
// does: `command -v`, then a login shell's `command -v`, keeping only an
// absolute path to an executable file. It is composed once from registry
// IDs and takes no other value.
var probeScript = mustComposeProbe(probeAgents)

func launchableAgents(defs []agent.Definition) []agent.ID {
	var ids []agent.ID
	for _, d := range defs {
		if d.Has(agent.Launch) || d.Has(agent.Resume) {
			ids = append(ids, d.ID)
		}
	}
	return ids
}

func mustComposeProbe(ids []agent.ID) string {
	script, err := composeProbe(ids)
	//coverage:exempt runs only if a registry ID stops being a plain word, which the registry's own validation forbids
	if err != nil {
		panic("tasks: " + err.Error())
	}
	return script
}

const probePreamble = `LC_ALL=C
export LC_ALL
echo '` + probeHeader + `'
probe_cli() {
	bin=$(command -v "$1" 2>/dev/null)
	case $bin in
	/*) ;;
	*) bin=$("${SHELL:-/bin/sh}" -lc "command -v $1" </dev/null 2>/dev/null | tail -n 1) ;;
	esac
	case $bin in
	/*) if [ -f "$bin" ] && [ -x "$bin" ]; then echo "cli $1 ` + probeFound + `"; return; fi ;;
	esac
	echo "cli $1 ` + probeMissing + `"
}
`

func composeProbe(ids []agent.ID) (string, error) {
	var script strings.Builder
	script.WriteString(probePreamble)
	for _, id := range ids {
		// The ID is a word in "command -v $1" a login shell reads.
		if !validProcessHint.MatchString(string(id)) {
			return "", fmt.Errorf("agent ID %q cannot be probed", id)
		}
		script.WriteString("probe_cli " + string(id) + "\n")
	}
	script.WriteString("echo '" + probeEnd + "'\n")
	return script.String(), nil
}

// parseProbeOutput reads which CLIs the probe found. Text before the header
// (a profile's greeting) is skipped; anything else unexpected, or an agent
// not answered for, fails the probe.
func parseProbeOutput(out []byte, ids []agent.ID) (map[agent.ID]bool, error) {
	asked := map[agent.ID]bool{}
	for _, id := range ids {
		asked[id] = true
	}
	found := map[agent.ID]bool{}
	started, ended := false, false
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		switch {
		case !started:
			started = line == probeHeader
		case line == probeEnd:
			ended = true
		default:
			fields := strings.Fields(line)
			answered := len(fields) == 3 && (fields[2] == probeFound || fields[2] == probeMissing)
			if !answered || fields[0] != "cli" || !asked[agent.ID(fields[1])] {
				return nil, fmt.Errorf("agent probe printed %q", boundedText(line, maxFormatExample))
			}
			id := agent.ID(fields[1])
			if _, twice := found[id]; twice {
				return nil, fmt.Errorf("agent probe answered for %s twice", id)
			}
			found[id] = fields[2] == probeFound
		}
		if ended {
			break
		}
	}
	if !started || !ended {
		return nil, errors.New("agent probe output is incomplete")
	}
	for _, id := range ids {
		if _, ok := found[id]; !ok {
			return nil, fmt.Errorf("agent probe did not answer for %s", id)
		}
	}
	return found, nil
}

// probeEntry is a host's last probe: when it ran, over which connection
// (nil for the panemux host), and what it found or why it failed.
type probeEntry struct {
	at    time.Time
	conn  Conn
	found map[agent.ID]bool
	err   error
}

// probeHost probes a host after its full collection, unless the probe of
// its current connection is younger than probeTTL. A failure is kept as
// long as a result, so a slow login shell is not waited for every
// collection; one cut short by the caller giving up is not kept.
func (s *Service) probeHost(ctx context.Context, name string) {
	conn := s.currentConn(name)
	now := s.opts.Now()
	s.mu.Lock()
	entry, ok := s.probes[name]
	s.mu.Unlock()
	if ok && entry.conn == conn && now.Sub(entry.at) < probeTTL {
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	entry = probeEntry{at: now, conn: conn}
	out, err := s.runHostScript(probeCtx, name, probeScript, "agent probe")
	if err == nil {
		entry.found, err = parseProbeOutput(out, probeAgents)
	}
	entry.err = err
	if err != nil && ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	if s.probes == nil {
		s.probes = map[string]probeEntry{}
	}
	s.probes[name] = entry
	s.mu.Unlock()
}

// currentConn is a remote host's connection, nil for the panemux host or a
// host with none.
func (s *Service) currentConn(name string) Conn {
	if name == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if h := s.hosts[name]; h != nil {
		return h.conn
	}
	return nil
}

// Availability is what is known about using each agent on a host ("" is
// the panemux host): collect from its last full collection, launch and
// resume from the probe of its current connection. The agents are in
// registry order, each with collect, launch and resume as it declares them.
func (s *Service) Availability(host string) []Availability {
	conn := s.currentConn(host)
	s.mu.Lock()
	entry, probed := s.probes[host]
	collect := s.collectAvailability[host]
	s.mu.Unlock()
	probed = probed && entry.conn == conn

	var list []Availability
	for _, d := range agent.All() {
		if d.Has(agent.Collect) {
			a, ok := collect[d.ID]
			if !ok {
				a = notProbed(d.ID, agent.Collect)
			}
			list = append(list, a)
		}
		for _, c := range []agent.Capability{agent.Launch, agent.Resume} {
			if d.Has(c) {
				list = append(list, cliAvailability(d.ID, c, entry, probed))
			}
		}
	}
	return list
}

func notProbed(id agent.ID, c agent.Capability) Availability {
	return Availability{Agent: id, Capability: c, Status: AvailabilityUnknown, Reason: ReasonNotProbed}
}

func cliAvailability(id agent.ID, c agent.Capability, entry probeEntry, probed bool) Availability {
	a := Availability{Agent: id, Capability: c}
	switch {
	case !probed:
		return notProbed(id, c)
	case entry.err != nil:
		a.Status, a.Reason = AvailabilityUnknown, ReasonProbeFailed
		a.Detail = boundedText(entry.err.Error(), maxAvailabilityDetail)
	case !entry.found[id]:
		a.Status, a.Reason = Unavailable, ReasonCLIMissing
		a.Detail = string(id) + " is not on PATH or in a login shell's PATH"
	default:
		a.Status = Available
	}
	return a
}

// recordCollectAvailability keeps a host's collect availability from its
// full collection and logs each agent's that changed: degraded, with what
// was found, or available again.
func (s *Service) recordCollectAvailability(host string, formats map[agent.ID]formatReport) {
	current := map[agent.ID]Availability{}
	for _, r := range collectPlan.full.adapters {
		current[r.id] = collectAvailabilityOf(r.id, formats[r.id])
	}
	s.mu.Lock()
	if s.collectAvailability == nil {
		s.collectAvailability = map[string]map[agent.ID]Availability{}
	}
	previous, known := s.collectAvailability[host]
	s.collectAvailability[host] = current
	s.mu.Unlock()

	where := hostName(host)
	for _, r := range collectPlan.full.adapters {
		now, before := current[r.id], previous[r.id]
		switch {
		case now.Status == Degraded && (before.Status != Degraded || before.Detail != now.Detail):
			s.opts.Logf("task collection on %s: %s: collect degraded (%s): %s", where, r.id, now.Reason, now.Detail)
		case now.Status != Degraded && known && before.Status == Degraded:
			s.opts.Logf("task collection on %s: %s: collect available again", where, r.id)
		}
	}
}

func collectAvailabilityOf(id agent.ID, r formatReport) Availability {
	a := Availability{Agent: id, Capability: agent.Collect, Status: Available, ObservedVersion: r.newestVersion()}
	if len(r.counts) > 0 {
		a.Status, a.Reason, a.Detail = Degraded, ReasonUnknownFormat, r.detail()
	}
	return a
}

// Bounds on what an unknown format's Detail carries.
const (
	maxFormatExamples     = 3
	maxFormatExample      = 120
	maxObservedVersions   = 3
	maxObservedVersion    = 32
	maxAvailabilityDetail = 600
)

// The kinds of unknown format formatReport counts, besides a Claude Code
// state file's UnreadableReason.
const (
	// formatUnknownStatus is a status outside the known set.
	formatUnknownStatus = "unknown_status"
	// formatUnknownEvent is a codex turn event outside the known set.
	formatUnknownEvent = "unknown_event"
	// formatMalformedRow is a codex row that cannot be read.
	formatMalformedRow = "malformed_row"
)

// formatReport is what one host's collection found of an agent's that this
// build does not know (issue #373): how many of each kind, the first few
// examples, and the versions the agent recorded.
type formatReport struct {
	counts   map[string]int
	examples []string
	versions []observedVersion
}

type observedVersion struct {
	version string
	at      int64
}

// add counts one thing of kind found in an unknown format; example shows it.
func (r *formatReport) add(kind, example string) {
	if r.counts == nil {
		r.counts = map[string]int{}
	}
	r.counts[kind]++
	if len(r.examples) < maxFormatExamples {
		r.examples = append(r.examples, boundedText(example, maxFormatExample))
	}
}

// observeVersion records that a session written at at (any unit, larger is
// newer) recorded version.
func (r *formatReport) observeVersion(version string, at int64) {
	if version == "" {
		return
	}
	r.versions = append(r.versions, observedVersion{version: boundedText(version, maxObservedVersion), at: at})
}

// distinctVersions are the versions seen, newest first, each once.
func (r *formatReport) distinctVersions() []string {
	sorted := append([]observedVersion(nil), r.versions...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].at > sorted[j].at })
	var out []string
	seen := map[string]bool{}
	for _, v := range sorted {
		if !seen[v.version] {
			seen[v.version] = true
			out = append(out, v.version)
		}
	}
	return out
}

func (r *formatReport) newestVersion() string {
	if v := r.distinctVersions(); len(v) > 0 {
		return v[0]
	}
	return ""
}

// detail is "<kind> <n>, ...; e.g. <example> | ...; versions <v>, ...", or
// "" when nothing was found in an unknown format.
func (r *formatReport) detail() string {
	if len(r.counts) == 0 {
		return ""
	}
	kinds := make([]string, 0, len(r.counts))
	for kind := range r.counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	parts := make([]string, len(kinds))
	for i, kind := range kinds {
		parts[i] = fmt.Sprintf("%s %d", kind, r.counts[kind])
	}
	detail := strings.Join(parts, ", ")
	if len(r.examples) > 0 {
		detail += "; e.g. " + strings.Join(r.examples, " | ")
	}
	if versions := r.distinctVersions(); len(versions) > 0 {
		if len(versions) > maxObservedVersions {
			versions = versions[:maxObservedVersions]
		}
		detail += "; versions " + strings.Join(versions, ", ")
	}
	return detail
}

// formatChecker is an adapter part that reports what it read in a format
// this build does not know.
type formatChecker interface {
	formats(b *taskBuilder) formatReport
}
