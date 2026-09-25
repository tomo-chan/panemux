package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"panemux/internal/session"
	"panemux/internal/tasks"
)

// taskGitInfo is the repository, branch and pull request of a task's working
// directory — the same metadata a pane header shows, looked up for the
// task's own directory instead of a pane's.
type taskGitInfo struct {
	Repo     string `json:"repo,omitempty"`
	RepoURL  string `json:"repo_url,omitempty"`
	Branch   string `json:"branch,omitempty"`
	PRURL    string `json:"pr_url,omitempty"`
	PRNumber int    `json:"pr_number,omitempty"`
}

type taskResponse struct {
	Git *taskGitInfo `json:"git,omitempty"`
	tasks.Task
}

type tasksResponse struct {
	Hosts []tasks.HostResult `json:"hosts"`
	Tasks []taskResponse     `json:"tasks"`
}

type taskGitCacheEntry struct {
	expiresAt time.Time
	info      *taskGitInfo
}

// taskGitLookupConcurrency bounds how many git/PR lookups one GET /api/tasks
// runs at once. Each is a local `git`/`gh` process or one exec channel on a
// host's collection connection.
const taskGitLookupConcurrency = 4

// taskGitLookupTimeout bounds one remote git lookup.
const taskGitLookupTimeout = 5 * time.Second

func newTaskService(h *Handler) *tasks.Service {
	return tasks.New(taskServiceOptions(h))
}

// taskServiceOptions is the production collector's configuration: the
// ssh_connections hosts, dialed with the pane dialer.
func taskServiceOptions(h *Handler) tasks.Options {
	return tasks.Options{
		Hosts: h.taskHostNames,
		Dial: func(name string) (tasks.Conn, error) {
			cfg, err := session.ResolveSSHConfig(name, h.cfg.SSHConnections, h.sshConfigPath)
			//coverage:exempt Hosts are ssh_connections keys, and ResolveSSHConfig always finds one of those
			if err != nil {
				return nil, fmt.Errorf("resolve ssh connection: %w", err)
			}
			conn, err := session.DialCommandConn(cfg)
			if err != nil {
				return nil, fmt.Errorf("dial: %w", err)
			}
			//coverage:exempt a connection needs a reachable SSH server; internal/session tests CommandConn against one
			return conn, nil
		},
	}
}

// taskHostNames is the hosts the dashboard collects from besides the
// panemux host: the ssh_connections keys. Hosts that exist only in
// ~/.ssh/config are not collected from (issue #252).
func (h *Handler) taskHostNames() []string {
	names := make([]string, 0, len(h.cfg.SSHConnections))
	for name := range h.cfg.SSHConnections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SetTaskService replaces the task collector NewHandler built, closing the
// one it replaces. internal/server's contract fixture uses it to capture a
// GET /api/tasks response that does not depend on the processes, tmux
// sessions and hosts of the machine running the suite.
func (h *Handler) SetTaskService(svc *tasks.Service) {
	previous := h.tasks
	h.tasks = svc
	previous.Close()
}

// Close releases what the handler holds open across requests: the task
// dashboard's per-host connections.
func (h *Handler) Close() {
	h.tasks.Close()
}

// GetTasks collects the agent sessions on every host and returns them with
// each task's git and pull request metadata. It collects on every call —
// the dashboard polls it only while it is on screen — and reports a host
// that failed in that host's entry rather than failing the request.
func (h *Handler) GetTasks(w http.ResponseWriter, r *http.Request) {
	snapshot := h.tasks.Collect(r.Context())

	collected := make(map[string]bool, len(snapshot.Hosts))
	for _, host := range snapshot.Hosts {
		if host.Status == tasks.HostOK {
			collected[host.Name] = true
		}
	}
	git := h.taskGitInfos(r.Context(), snapshot.Tasks, collected)

	resp := tasksResponse{Hosts: snapshot.Hosts, Tasks: make([]taskResponse, 0, len(snapshot.Tasks))}
	for _, task := range snapshot.Tasks {
		resp.Tasks = append(resp.Tasks, taskResponse{Task: task, Git: git[taskGitKey(task.Host, task.CWD)]})
	}
	writeJSON(w, resp)
}

// PostTaskHostReconnect drops a host's collection connection and any
// remembered connection failure, so the next GET /api/tasks dials it again
// without waiting out the retry delay. The only way it fails is a name that
// is not an ssh_connections key (tasks.ErrUnknownHost).
func (h *Handler) PostTaskHostReconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.tasks.Reconnect(chi.URLParam(r, "name")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func taskGitKey(host, cwd string) string {
	return host + "\x00" + cwd
}

// taskGitInfos looks up git metadata once per (host, directory), serving
// repeats from a cache that lives as long as a pane header's does.
func (h *Handler) taskGitInfos(
	ctx context.Context, list []tasks.Task, collected map[string]bool,
) map[string]*taskGitInfo {
	type target struct{ host, cwd string }
	results := map[string]*taskGitInfo{}
	var pending []target

	now := h.nowFn()
	h.taskGitCacheMu.Lock()
	for _, task := range list {
		key := taskGitKey(task.Host, task.CWD)
		if task.CWD == "" || !collected[task.Host] {
			continue
		}
		if _, done := results[key]; done {
			continue
		}
		if entry, ok := h.taskGitCache[key]; ok && now.Before(entry.expiresAt) {
			results[key] = entry.info
			continue
		}
		results[key] = nil
		pending = append(pending, target{task.Host, task.CWD})
	}
	h.taskGitCacheMu.Unlock()

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		sem = make(chan struct{}, taskGitLookupConcurrency)
	)
	for _, t := range pending {
		wg.Add(1)
		sem <- struct{}{}
		go func(t target) {
			defer wg.Done()
			defer func() { <-sem }()
			info := h.taskGitLookup(ctx, t.host, t.cwd)
			mu.Lock()
			results[taskGitKey(t.host, t.cwd)] = info
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	h.taskGitCacheMu.Lock()
	for _, t := range pending {
		key := taskGitKey(t.host, t.cwd)
		h.taskGitCache[key] = taskGitCacheEntry{info: results[key], expiresAt: now.Add(gitInfoCacheTTL)}
	}
	h.taskGitCacheMu.Unlock()
	return results
}

// lookupTaskGit resolves one directory's git metadata, or nil when it is not
// in a repository or cannot be inspected. host is "" for the panemux host.
func (h *Handler) lookupTaskGit(ctx context.Context, host, cwd string) *taskGitInfo {
	var (
		gitCtx session.GitContext
		err    error
	)
	if host == "" {
		if err = gitExistsFn(); err != nil {
			return nil
		}
		gitCtx, err = h.inspectLocalGitContext(cwd)
	} else {
		lookupCtx, cancel := context.WithTimeout(ctx, taskGitLookupTimeout)
		defer cancel()
		gitCtx, err = h.tasks.InspectGitContext(lookupCtx, host, cwd)
	}
	if err != nil {
		return nil
	}

	prURL, prNumber := h.lookupPR(host != "", cwd, gitCtx)
	return &taskGitInfo{
		Repo:     gitCtx.Repo,
		RepoURL:  h.repoPageURLFromOriginURL(gitCtx.OriginURL),
		Branch:   gitCtx.Branch,
		PRURL:    prURL,
		PRNumber: prNumber,
	}
}
