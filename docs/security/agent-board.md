# Security: Agent Board

> Part of the [security design](../security.md). The general command-execution and `gosec` rules
> also apply.

### Agent board remote writes

Panemux may invoke only operator-installed agmsg scripts over a pane's existing SSH transport. It
must never install panemux, agmsg, or persistent helper scripts on a remote host.

Remote commands must satisfy all of these rules:

- pass every value as an argument and quote every argument at the single SSH command-construction
  boundary;
- allowlist `team`, `from`, and `to` as agmsg identifiers;
- base64-encode arbitrary message bodies, validate the encoded alphabet, and decode through a fixed
  wrapper using positional parameters;
- never interpolate caller-controlled data into wrapper or probe script text;
- leave SQL escaping to agmsg and never construct SQL in panemux;
- expand `~/` in `agmsg_path` to an absolute local or remote-home path before shell quoting.

Plain shell quoting is not sufficient evidence for the repository's CodeQL command-injection bar.
The encoded-body path deliberately places an allowlist check before the exec sink, but no real
CodeQL scan has yet confirmed that the analyzer recognizes the taint break. Treat this as structural
mitigation until verified.

The only fixed probes permitted are those needed to resolve the remote home directory and test for
agmsg's read script. Variable values must be positional arguments, not embedded in probe text.

Bootstrap's onboarding text is a PTY write, not an `exec.Command` sink. Shell-escaping rules do not
apply to that text, but all interpolated identifiers must still pass Agent Board validation so the
instruction and later agmsg routing agree.

### Agent-reported values in the dashboard UI

Every status field is untrusted free text. Sender validation proves only that a known pane identity
was used; it does not validate the content.

The dashboard must render agent-reported values as React text children. It must not use
`dangerouslySetInnerHTML` or place those values into DOM attributes. In particular, self-reported
URLs must not become links without an explicit scheme allowlist: escaping text does not make an
`href` safe, and script execution in the dashboard origin would expose the board bearer token.

The current dashboard shows `state`, `summary`, and `last_tool` as text. It intentionally omits the
self-reported repository, branch, and PR URL. Any future attribute-valued rendering reopens this
security sink and requires dedicated validation.
