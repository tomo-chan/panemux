---
type: Decision
title: "2026-11-01: Mermaid inside isolated Markdown previews (#471)"
description: "Bundle a nonce-authorized classic script for isolated Markdown diagrams instead of adding a runtime browser."
---

# 2026-11-01: Mermaid inside isolated Markdown previews (#471)

Markdown keeps goldmark's GFM renderer and escaped fenced-code output. An AST check enables a
product-owned classic script only when a Mermaid block exists. Rendering in the preview itself
keeps local/SFTP, pane/popup, and independent-tab behavior on one path. Rendering in the dashboard
would require cross-origin messaging and a separate implementation for tabs; server-side Mermaid
CLI would add a browser and Node to the runtime distribution.

The renderer is bundled separately as one IIFE, with diagram loaders folded into that file.
Opaque-origin previews therefore need neither ESM CORS permission nor `allow-same-origin`.
A response-specific nonce authorizes the product script, without authorizing scripts selected by
the Markdown. The server hashes the bundled bytes to create its asset URL; a fixed build output
name replaces the proposed build manifest because there is only one runtime asset. Licenses stay
in the bundle. Diagram configuration directives and frontmatter are refused rather than trying
to enumerate the settings an evolving Mermaid version might expose. Per-block errors preserve
the original source. Input limits bound work but are not a preemptive rendering timeout.

MathJax, GeoJSON/TopoJSON maps and ASCII STL remain separate future work; the file-type boundary
is unchanged. GitHub compatibility concerns syntax and viewing behavior, not matching its
private integration or a deployed library version.


The local verification also exposed two test-harness assumptions: the login-shell fixture's
`.profile` sources `.bashrc` when macOS `sh` is Bash, and the growing tasks package exceeded the
coverage run's aggregate 60-second package budget while an individual test had run only one
second. The fixture probes the actual shell without inherited startup environment, and the
coverage package budget is 120 seconds. Production task-launch behavior is unchanged.

Current contracts: [preview guide](../../security/preview.md).
