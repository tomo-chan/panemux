---
type: Decision
title: "Dashboard hosts are listed by name; details come from `~/.ssh/config` (2026-09-29, issue #272)"
description: "Rationale for Dashboard hosts are listed by name; details come from `~/.ssh/config`."
---

# Dashboard hosts are listed by name; details come from `~/.ssh/config` (2026-09-29, issue #272)

A named ssh_connections entry can reuse OpenSSH connection details without copying them.
Collecting every OpenSSH host was rejected because most need not run agents. Existing entries
accidentally imported by older validation are not deleted behind the operator's back.
Merging name-only entries exposed ProxyJump cycles and unchecked Add SSH Host values. Cycle
tracking prevents recursive resolution; rejecting control characters in API values and AppendHost
prevents injected directives, including ProxyCommand. This existing flaw was fixed with the feature
rather than left behind a security claim that no API writes ProxyCommand.
See [task behavior](../../behavior/tasks.md) and [SSH security](../../security/command-execution.md).
