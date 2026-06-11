package session

import (
	"fmt"
	"strings"
)

type GitContextErrorCause string

const (
	GitContextCauseNotGitRepo       GitContextErrorCause = "not_git_repo"
	GitContextCauseGitNotInstalled  GitContextErrorCause = "git_not_installed"
	GitContextCauseInvalidCWD       GitContextErrorCause = "invalid_cwd"
	GitContextCauseSSHSessionFailed GitContextErrorCause = "ssh_session_failure"
	GitContextCauseGitMetadata      GitContextErrorCause = "git_metadata_failure"
	GitContextCauseIncomplete       GitContextErrorCause = "incomplete_response"
	GitContextCauseUnknown          GitContextErrorCause = "unknown"
)

type GitContextError struct {
	err          error
	Transport    string
	Operation    string
	CWD          string
	Cause        GitContextErrorCause
	CauseMessage string
	Remediation  string
	RawError     string
	Stderr       string
}

func NewGitContextError(
	transport, operation, cwd string,
	cause GitContextErrorCause,
	err error,
	stderr string,
) *GitContextError {
	causeMessage, remediation := gitContextErrorDefaults(cause)
	raw := ""
	if err != nil {
		raw = strings.TrimSpace(err.Error())
	}
	return &GitContextError{
		Transport:    transport,
		Operation:    operation,
		CWD:          cwd,
		Cause:        cause,
		CauseMessage: causeMessage,
		Remediation:  remediation,
		RawError:     raw,
		Stderr:       strings.TrimSpace(stderr),
		err:          err,
	}
}

func (e *GitContextError) Error() string {
	if e == nil {
		return ""
	}
	if e.RawError != "" {
		return e.RawError
	}
	return fmt.Sprintf("git context inspection failed: %s", e.Cause)
}

func (e *GitContextError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func gitContextErrorDefaults(cause GitContextErrorCause) (string, string) {
	switch cause {
	case GitContextCauseNotGitRepo:
		return "current directory is outside a Git repository",
			"move the pane to a Git repository or worktree directory, or update the pane cwd to the repository root"
	case GitContextCauseGitNotInstalled:
		return "git is not installed or not available in PATH",
			"install Git on the target host and ensure it is available in PATH"
	case GitContextCauseInvalidCWD:
		return "working directory is invalid for Git inspection",
			"set the pane cwd to a valid absolute path without invalid characters"
	case GitContextCauseSSHSessionFailed:
		return "SSH command execution failed before Git context could be resolved",
			"check SSH connectivity, authentication, and remote shell availability"
	case GitContextCauseGitMetadata:
		return "Git repository metadata could not be resolved",
			"verify the repository or worktree is intact and that .git metadata points to valid paths"
	case GitContextCauseIncomplete:
		return "remote Git context command returned incomplete output",
			"check the remote shell environment and rerun Git inspection in the target directory"
	default:
		return "Git context lookup failed for an unknown reason",
			"inspect the raw error and stderr details to determine the next action"
	}
}

func ClassifyGitFailureCause(stderr string, err error) GitContextErrorCause {
	lowerStderr := strings.ToLower(strings.TrimSpace(stderr))
	lowerErr := ""
	if err != nil {
		lowerErr = strings.ToLower(err.Error())
	}

	switch {
	case strings.Contains(lowerStderr, "not a git repository"),
		strings.Contains(lowerStderr, "not git repository"):
		return GitContextCauseNotGitRepo
	case strings.Contains(lowerStderr, "command not found"),
		strings.Contains(lowerStderr, "git: not found"),
		strings.Contains(lowerErr, "command not found"),
		strings.Contains(lowerErr, "git: not found"):
		return GitContextCauseGitNotInstalled
	case strings.Contains(lowerStderr, "not a gitdir"),
		strings.Contains(lowerStderr, "not a git repository:"),
		strings.Contains(lowerStderr, "bad object"),
		strings.Contains(lowerStderr, "invalid gitfile format"),
		strings.Contains(lowerStderr, "unable to read"):
		return GitContextCauseGitMetadata
	default:
		return GitContextCauseUnknown
	}
}
