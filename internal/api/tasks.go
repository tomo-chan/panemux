package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

// taskGitInfo is the repository, branch and pull request of a task's working
// directory — the same metadata a pane header shows, looked up for the
// task's own directory instead of a pane's — with the issues the pull
// request closes and the references its branch name and title carry.
type taskGitInfo struct {
	Repo    string          `json:"repo,omitempty"`
	RepoURL string          `json:"repo_url,omitempty"`
	Branch  string          `json:"branch,omitempty"`
	PRURL   string          `json:"pr_url,omitempty"`
	Issues  []taskIssueLink `json:"issues,omitempty"`
	// Autolinks are the references task_dashboard.autolinks finds.
	Autolinks []taskAutolink `json:"autolinks,omitempty"`
	PRNumber  int            `json:"pr_number,omitempty"`
}

// taskIssueLink is one GitHub issue the task's pull request closes. Repo is
// the issue's owner/name, which can differ from the pull request's.
type taskIssueLink struct {
	URL    string `json:"url"`
	Repo   string `json:"repo,omitempty"`
	Number int    `json:"number"`
}

// taskAutolink is one reference found in the task's branch name or pull
// request title by a task_dashboard.autolinks entry — such as JIRA-123 — and
// the URL that entry makes of it.
type taskAutolink struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// prTaskFields is what the task dashboard reads of a pull request: the
// title for its references and the issues it closes, in the same `gh` call.
const prTaskFields = "url,number,title,closingIssuesReferences"

// autolinkRefs lists the references in texts, in the order they appear,
// each once. A reference is an autolink's key_prefix, exactly as written,
// followed by its identifier: digits, or with is_alphanumeric letters,
// digits and "-", as many as there are. No ASCII letter or digit may come
// directly before the prefix or, for digits, after the identifier.
// validateTaskDashboard refuses overlapping prefixes, so at most one
// autolink matches at any position.
func autolinkRefs(links []config.AutolinkConfig, texts ...string) []taskAutolink {
	if len(links) == 0 {
		return nil
	}
	var refs []taskAutolink
	seen := map[string]bool{}
	for _, text := range texts {
		// At i == len(text) nothing can follow a prefix, so no reference starts there.
		//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent: i <= len(text) adds a position where no identifier fits
		for i := 0; i < len(text); i++ {
			if i > 0 && isASCIIAlnum(text[i-1]) {
				continue
			}
			for _, link := range links {
				end, ok := autolinkRefEnd(link, text, i)
				if !ok {
					continue
				}
				ref := text[i:end]
				if !seen[ref] {
					seen[ref] = true
					refs = append(refs, taskAutolink{Text: ref, URL: link.URL(text[i+len(link.KeyPrefix) : end])})
				}
				i = end - 1
				break
			}
		}
	}
	return refs
}

// autolinkRefEnd is where link's reference starting at text[start] ends, and
// whether there is one there.
func autolinkRefEnd(link config.AutolinkConfig, text string, start int) (int, bool) {
	if !strings.HasPrefix(text[start:], link.KeyPrefix) {
		return 0, false
	}
	numStart := start + len(link.KeyPrefix)
	end := numStart
	for end < len(text) && isAutolinkIDByte(text[end], link.IsAlphanumeric) {
		end++
	}
	if end == numStart || (!link.IsAlphanumeric && end < len(text) && isASCIIAlnum(text[end])) {
		return 0, false
	}
	return end, true
}

// isAutolinkIDByte is what GitHub's autolinks take as the identifier:
// digits, or with is_alphanumeric A-Z in either case, 0-9 and "-".
func isAutolinkIDByte(b byte, alphanumeric bool) bool {
	if alphanumeric {
		return isASCIIAlnum(b) || b == '-'
	}
	return b >= '0' && b <= '9'
}

func isASCIIAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

// closingIssueLinks is the issues a pull request closes, keeping only those
// with an issue number and an http(s) URL: the browser validates every link
// in the response and would reject all of it for one bad entry.
func closingIssueLinks(pr ghPullRequest) []taskIssueLink {
	var links []taskIssueLink
	for _, issue := range pr.ClosingIssues {
		if issue.Number <= 0 || !isHTTPURL(issue.URL) {
			continue
		}
		link := taskIssueLink{Number: issue.Number, URL: issue.URL}
		if issue.Repository.Owner.Login != "" && issue.Repository.Name != "" {
			link.Repo = issue.Repository.Owner.Login + "/" + issue.Repository.Name
		}
		links = append(links, link)
	}
	return links
}

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// webLinkSchemes are the schemes a link on the dashboard may have.
var webLinkSchemes = map[string]bool{schemeHTTP: true, schemeHTTPS: true}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && webLinkSchemes[u.Scheme] && u.Host != ""
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
	// withPR is whether the lookup ran `gh pr view`. An entry without it
	// does not serve a directory a running task uses.
	withPR bool
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
	if refuseCrossSite(w, r) {
		return
	}
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
		resp.Tasks = append(resp.Tasks, taskResponse{Task: task, Git: taskGitFor(task, git[taskGitKey(task.Host, task.CWD)])})
	}
	writeJSON(w, resp)
}

// PostTaskHostReconnect drops a host's collection connection and any
// remembered connection failure, so the next GET /api/tasks dials it again
// without waiting out the retry delay. The only way it fails is a name that
// is not an ssh_connections key (tasks.ErrUnknownHost).
func (h *Handler) PostTaskHostReconnect(w http.ResponseWriter, r *http.Request) {
	if refuseCrossSite(w, r) {
		return
	}
	if err := h.tasks.Reconnect(chi.URLParam(r, "name")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// taskGitFor is a task's share of its directory's git metadata. The metadata
// is read from the directory as it is now, which for a running task is the
// branch it is working on. For a stopped one it is whatever was checked out
// since, so only the repository — which a directory keeps — is reported.
func taskGitFor(task tasks.Task, info *taskGitInfo) *taskGitInfo {
	if info == nil || task.State != tasks.StateStop {
		return info
	}
	if info.Repo == "" && info.RepoURL == "" {
		return nil
	}
	return &taskGitInfo{Repo: info.Repo, RepoURL: info.RepoURL}
}

// refuseCrossSite answers 403 to a request another site's page made. The
// task routes are unauthenticated like the rest of /api/*, but unlike the
// others GET /api/tasks has a heavy side effect — every host is dialed and
// runs the collection script, and `gh pr view` runs per directory — which an
// <img> on any page the operator has open could otherwise trigger. See
// docs/security/command-execution.md, "Task dashboard collection".
func refuseCrossSite(w http.ResponseWriter, r *http.Request) bool {
	if isCrossSiteRequest(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return true
	}
	return false
}

// isCrossSiteRequest uses what a browser adds to every request it makes on a
// page's behalf: Sec-Fetch-Site (sent on every request by current browsers,
// images included) and Origin (sent on cross-origin requests and on POST).
// A request carrying neither is not from a browser page and is allowed.
func isCrossSiteRequest(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "same-site":
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return true
	}
	return u.Host != r.Host && !isLoopbackAuthority(u.Host)
}

func taskGitKey(host, cwd string) string {
	return host + "\x00" + cwd
}

// taskGitTarget is one (host, directory) whose git metadata a response
// needs, and whether it needs the pull request too.
type taskGitTarget struct {
	host, cwd string
	withPR    bool
}

// taskGitTargets lists each (host, directory) the tasks of hosts that were
// collected use, once, in the order they first appear. A directory needs its
// pull request when any running task uses it.
func taskGitTargets(list []tasks.Task, collected map[string]bool) []taskGitTarget {
	var targets []taskGitTarget
	index := map[string]int{}
	for _, task := range list {
		if task.CWD == "" || !collected[task.Host] {
			continue
		}
		key := taskGitKey(task.Host, task.CWD)
		i, seen := index[key]
		if !seen {
			i = len(targets)
			index[key] = i
			targets = append(targets, taskGitTarget{host: task.Host, cwd: task.CWD})
		}
		targets[i].withPR = targets[i].withPR || task.State != tasks.StateStop
	}
	return targets
}

// taskGitInfos looks up git metadata once per (host, directory), serving
// repeats from a cache that lives as long as a pane header's does. The pull
// request is looked up only for a directory a running task uses: taskGitFor
// never reports one for a stopped task.
func (h *Handler) taskGitInfos(
	ctx context.Context, list []tasks.Task, collected map[string]bool,
) map[string]*taskGitInfo {
	targets := taskGitTargets(list, collected)
	results := make(map[string]*taskGitInfo, len(targets))
	var pending []taskGitTarget
	now := h.nowFn()
	h.taskGitCacheMu.Lock()
	for _, t := range targets {
		key := taskGitKey(t.host, t.cwd)
		if entry, ok := h.taskGitCache[key]; ok && now.Before(entry.expiresAt) && (entry.withPR || !t.withPR) {
			results[key] = entry.info
			continue
		}
		pending = append(pending, t)
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
		go func(t taskGitTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			info := h.taskGitLookup(ctx, t.host, t.cwd, t.withPR)
			mu.Lock()
			results[taskGitKey(t.host, t.cwd)] = info
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	// A request abandoned while its lookups ran stopped `gh` and the remote
	// git runs rather than getting their answer, so nothing it looked up is
	// cached as the directory's metadata.
	if ctx.Err() != nil {
		return results
	}
	h.taskGitCacheMu.Lock()
	for _, t := range pending {
		key := taskGitKey(t.host, t.cwd)
		h.taskGitCache[key] = taskGitCacheEntry{
			info: results[key], withPR: t.withPR, expiresAt: now.Add(gitInfoCacheTTL),
		}
	}
	h.taskGitCacheMu.Unlock()
	return results
}

// lookupTaskGit resolves one directory's git metadata, or nil when it is not
// in a repository or cannot be inspected. host is "" for the panemux host.
// The pull request is looked up only when withPR is set.
func (h *Handler) lookupTaskGit(ctx context.Context, host, cwd string, withPR bool) *taskGitInfo {
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

	info := &taskGitInfo{
		Repo:    gitCtx.Repo,
		RepoURL: h.repoPageURLFromOriginURL(gitCtx.OriginURL),
		Branch:  gitCtx.Branch,
	}
	prTitle := ""
	if withPR {
		pr, err := h.lookupPullRequest(ctx, host != "", cwd, gitCtx, prTaskFields)
		if errors.Is(err, errGHUnknownJSONField) {
			// A gh older than 2.72.0: keep the PR link the pane header shows,
			// without the issues and the title this gh cannot give.
			pr, err = h.lookupPullRequest(ctx, host != "", cwd, gitCtx, prBasicFields)
		}
		if err == nil {
			info.PRURL, info.PRNumber = strings.TrimSpace(pr.URL), pr.Number
			info.Issues = closingIssueLinks(pr)
			prTitle = pr.Title
		}
	}
	info.Autolinks = autolinkRefs(h.cfg.TaskDashboard.Autolinks, gitCtx.Branch, prTitle)
	return info
}
