package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrTaskNotFound is returned by FindTask for an ID that no host reports.
var ErrTaskNotFound = errors.New("task not found")

// ErrHostUnavailable is returned by FindTask when the task's host could not
// be collected.
var ErrHostUnavailable = errors.New("host unavailable")

// FindTask collects the host a task ID names and returns the task with that
// ID. Only that host is collected: "local:" names the panemux host and
// "ssh:<name>:" an ssh_connections key. When one key is a prefix of another
// ("dev" and "dev-2"), each key the ID could name is tried.
func (s *Service) FindTask(ctx context.Context, id string) (Task, error) {
	for _, host := range s.hostsOfTaskID(id) {
		result, list := s.collectHost(ctx, host)
		if result.Status != HostOK {
			return Task{}, fmt.Errorf("%w: collect %s: %s", ErrHostUnavailable, hostName(host), resultError(result))
		}
		for _, task := range list {
			if task.ID == id {
				return task, nil
			}
		}
	}
	return Task{}, fmt.Errorf("%w: %q", ErrTaskNotFound, id)
}

// hostsOfTaskID lists the hosts whose taskID prefix id carries.
func (s *Service) hostsOfTaskID(id string) []string {
	if strings.HasPrefix(id, hostPrefixOfTaskID("")) {
		return []string{""}
	}
	var hosts []string
	for _, name := range s.hostNames() {
		if strings.HasPrefix(id, hostPrefixOfTaskID(name)) {
			hosts = append(hosts, name)
		}
	}
	return hosts
}

// hostPrefixOfTaskID is the start every taskID of host shares: "local:" or
// "ssh:<host>:".
func hostPrefixOfTaskID(host string) string {
	return strings.TrimSuffix(taskID(host, "", ""), ":")
}
