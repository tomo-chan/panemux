package api

import (
	"time"

	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/host"
	"panemux/internal/tasks"
)

// Nullable fields distinguish missing observations from an observed state.
// SelfReport is supplemental even when task observations supply the primary state.
type boardIdentityEntry struct {
	StatusSince    *time.Time        `json:"status_since"`
	TaskObservedAt *time.Time        `json:"task_observed_at"`
	LastMessageAt  *time.Time        `json:"last_message_at"`
	SelfReport     *boardStatusEntry `json:"self_report"`
	PaneID         *string           `json:"pane_id"`
	State          *string           `json:"state"`
	WaitingFor     *string           `json:"waiting_for"`
	Host           string            `json:"host"`
	Team           string            `json:"team"`
	Name           string            `json:"name"`
	Source         string            `json:"source"`
}

type boardTaskObservation struct {
	at         *time.Time
	tasks      []tasks.Task
	generation uint64
}

// beginBoardTaskCollection orders full and live collection starts with a logical
// clock. A full collection uses one generation for all of its host results.
func (h *Handler) beginBoardTaskCollection() uint64 {
	h.boardTasksMu.Lock()
	defer h.boardTasksMu.Unlock()
	h.boardTaskGeneration++
	return h.boardTaskGeneration
}

// recordBoardTasks consumes existing collections only; status requests do no I/O.
// Replacing a host observation also removes tasks no longer observed. A failed or
// pending collection removes task priority without discarding self-reports.
// Keep its generation even without tasks so late older results cannot restore it.
func (h *Handler) recordBoardTasks(generation uint64, result tasks.HostResult, list []tasks.Task) {
	h.cfgMu.RLock()
	defer h.cfgMu.RUnlock()
	if result.Name != "" {
		if _, ok := h.cfg.SSHConnections[result.Name]; !ok {
			return
		}
	}
	h.boardTasksMu.Lock()
	defer h.boardTasksMu.Unlock()
	if h.boardTasks == nil {
		h.boardTasks = make(map[string]boardTaskObservation)
	}
	// Starts mint distinct generations under boardTasksMu. GetTasks records once
	// per unique host from the config map; CollectHostLive records its host once.
	// Equal generations replace previous outcomes; only strictly older results are stale.
	if generation < h.boardTasks[result.Name].generation {
		return
	}
	observation := boardTaskObservation{generation: generation}
	if result.Status == tasks.HostOK {
		observation.at = result.CollectedAt
		observation.tasks = append([]tasks.Task(nil), list...)
	}
	h.boardTasks[result.Name] = observation
}

// invalidateBoardConnections runs under cfgMu after a successful save. Keep
// tombstone generations so an in-flight result cannot repopulate an old target.
func (h *Handler) invalidateBoardConnections(previous, next map[string]config.SSHConnection) {
	h.boardTasksMu.Lock()
	defer h.boardTasksMu.Unlock()
	if h.boardTasks == nil {
		h.boardTasks = make(map[string]boardTaskObservation)
	}
	invalidate := func(name string) {
		h.boardTaskGeneration++
		h.boardTasks[name] = boardTaskObservation{generation: h.boardTaskGeneration}
	}
	for name, old := range previous {
		current, exists := next[name]
		if !exists || current != old {
			invalidate(name)
		}
	}
	for name := range next {
		if _, exists := previous[name]; !exists {
			invalidate(name)
		}
	}
}

func statusEntry(s board.Status) boardStatusEntry {
	return boardStatusEntry{
		UpdatedAt: s.UpdatedAt, State: s.State, CWD: s.CWD, Branch: s.Branch, Repo: s.Repo,
		PRURL: s.PRURL, LastTool: s.LastTool, Summary: s.Summary,
	}
}

type boardIdentityBuilder struct {
	entries   map[string]boardIdentityEntry
	panes     map[string]*config.PaneConfig
	paneHosts map[string]string
	team      string
}

func (h *Handler) boardIdentities() map[string]boardIdentityEntry {
	h.cfgMu.RLock()
	defer h.cfgMu.RUnlock()
	h.boardTasksMu.RLock()
	defer h.boardTasksMu.RUnlock()
	b := boardIdentityBuilder{
		entries: make(map[string]boardIdentityEntry), panes: make(map[string]*config.PaneConfig),
		paneHosts: make(map[string]string), team: h.cfg.AgentBoard.Team,
	}
	for _, pane := range h.cfg.AllPanes() {
		target := host.Local
		if pane.Type == config.PaneTypeSSH || pane.Type == config.PaneTypeSSHTmux {
			if _, ok := h.cfg.SSHConnections[pane.Connection]; !ok {
				continue
			}
			target = host.Host(pane.Connection)
		}
		b.panes[pane.ID] = pane
		b.paneHosts[pane.ID] = target.BoardID()
	}
	b.addReports(h.boardCache.StatusSnapshot())
	b.addMessages(h.boardCache.MessagesSince(0))
	b.addDetected(h.boardCache.ParticipationSnapshot())
	b.addTasks(h.boardTasks)
	return b.entries
}

func (b *boardIdentityBuilder) entry(id board.Identity) boardIdentityEntry {
	if e, ok := b.entries[id.String()]; ok {
		return e
	}
	e := boardIdentityEntry{Name: id.Name, Host: id.Host, Team: b.team, Source: "unreported"}
	if b.paneHosts[id.Name] == id.Host {
		name := id.Name
		e.PaneID = &name
	}
	return e
}

func (b *boardIdentityBuilder) addReports(reports map[string]board.Status) {
	for key, s := range reports {
		id, err := board.ParseIdentity(key)
		if err != nil || id.Name == board.SystemID {
			continue
		}
		// Unqualified keys belong to legacy in-process callers, not remote observations.
		if id.Host == "" {
			id.Host = b.paneHosts[id.Name]
		}
		if id.Host == "" {
			continue
		}
		e := b.entry(id)
		report := statusEntry(s)
		e.SelfReport = &report
		e.Source = "self_report"
		if s.State != "" {
			state := s.State
			e.State = &state
		}
		b.entries[id.String()] = e
	}
}

func (b *boardIdentityBuilder) addMessages(rows []board.CachedRow) {
	for _, cr := range rows {
		row := cr.Row
		// Relay owns admission; guard synthetic/legacy cache entries without reauthorizing sends.
		id, err := board.ParseIdentity(row.From)
		if err != nil || id.Host != "" || id.Name == board.SystemID || row.Team != b.team || !board.ValidBoardID(row.Host) {
			continue
		}
		id.Host = row.Host
		e := b.entry(id)
		if !row.At.IsZero() && (e.LastMessageAt == nil || row.At.After(*e.LastMessageAt)) {
			at := row.At
			e.LastMessageAt = &at
		}
		b.entries[id.String()] = e
	}
}

func (b *boardIdentityBuilder) addDetected(participation map[string]board.PaneParticipation) {
	for name, p := range participation {
		if p.AgentType == "" || p.Host == "" || b.paneHosts[name] != p.Host {
			continue
		}
		id, err := board.ParseIdentity(name)
		if err != nil || id.Host != "" || id.Name == board.SystemID {
			continue
		}
		id.Host = p.Host
		b.entries[id.String()] = b.entry(id)
	}
}

func (b *boardIdentityBuilder) addTasks(observations map[string]boardTaskObservation) {
	for name, pane := range b.panes {
		taskHost := ""
		if pane.Type == config.PaneTypeSSH || pane.Type == config.PaneTypeSSHTmux {
			taskHost = pane.Connection
		}
		observation, ok := observations[taskHost]
		if !ok {
			continue
		}
		var matching []tasks.Task
		for _, task := range observation.tasks {
			if task.Host == taskHost && task.State != tasks.StateStop && boardTaskMatchesPane(task, pane) {
				matching = append(matching, task)
			}
		}
		if len(matching) == 0 {
			continue
		}
		id, err := board.ParseIdentity(name)
		if err != nil || id.Host != "" || id.Name == board.SystemID {
			continue
		}
		id.Host = b.paneHosts[name]
		e := b.entry(id)
		if len(matching) == 1 && b.matchingPaneCount(matching[0]) == 1 {
			applyBoardTask(&e, matching[0], observation.at)
		}
		b.entries[id.String()] = e
	}
}

func (b *boardIdentityBuilder) matchingPaneCount(task tasks.Task) int {
	count := 0
	for name, pane := range b.panes {
		if b.paneHosts[name] == host.Host(task.Host).BoardID() && boardTaskMatchesPane(task, pane) {
			count++
		}
	}
	return count
}

func applyBoardTask(e *boardIdentityEntry, task tasks.Task, at *time.Time) {
	state := string(task.State)
	e.Source = "task"
	e.State = &state
	e.StatusSince = task.StatusSince
	e.TaskObservedAt = at
	if task.WaitingFor != "" {
		waiting := task.WaitingFor
		e.WaitingFor = &waiting
	}
}

// Share findTaskPane's location predicate. Board priority additionally requires
// one task per pane and one pane per task, rather than selecting the first pane.
func boardTaskMatchesPane(task tasks.Task, pane *config.PaneConfig) bool {
	switch task.Location.Kind {
	case tasks.LocationTmux:
		return task.Location.TmuxSession != "" && task.Location.TmuxSession == pane.TmuxSession &&
			(pane.Type == config.PaneTypeTmux || pane.Type == config.PaneTypeSSHTmux)
	case tasks.LocationOutside:
		return task.Location.PaneID == pane.ID && (pane.Type == config.PaneTypeLocal || pane.Type == config.PaneTypeSSH)
	default:
		return false
	}
}
