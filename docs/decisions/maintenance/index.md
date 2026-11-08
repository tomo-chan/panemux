# Maintenance decisions

## Decisions

* [agmsg pin bump proposals (2026-10-31, issue #437)](agmsg-pin-proposals.md) - Use deduplicated GitHub App proposals for verified pins while withholding publishing secrets from upstream capture code.
* [2026-10-16: Installing the codex permission hook from the host menu (#294, #396)](codex-hook-install.md) - Merge embedded hook configuration conservatively and leave Codex trust decisions with the operator.
* [2026-10-17: Updating the codex permission hook by itself (#294, #402)](codex-hook-updates.md) - Offer per-host opt-in update-only checks instead of redundant binaries or automatic retries.
* [2026-10-15: Codex approval waits from a PermissionRequest hook (#294, #384)](codex-permission-observation.md) - Approximate approval waits from permission-hook records instead of detecting prompts in terminal output.
* [Release recovery uses the original source with GNU coreutils (2026-11-02, PR #479)](release-source-recovery.md) - Recover drafts from their original source using GNU coreutils instead of overlaying newer code or ignoring lock errors.
