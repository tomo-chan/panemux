---
type: Decision
title: "2026-11-02: Mermaid measurement and final-output sanitization (#477 review)"
description: "Apply image policy during diagram measurement because final SVG sanitization cannot retract earlier requests."
---

# 2026-11-02: Mermaid measurement and final-output sanitization (#477 review)

Mermaid needs a live DOM to measure its unsanitized SVG before returning the final drawing.
The existing Markdown image policy also applies during that measurement, so image metadata can
make external requests. Final SVG sanitization removes image elements but cannot retract an
already-issued request. Behavior/security guidance and a browser test cover both successful
image loading and a decode failure; there is no claim that diagram rendering is network-isolated.

The renderer now limits a document to 10 attempts and 50,000 UTF-16 code units, with 20,000 per
diagram and 100 edges where Mermaid supports the edge cap. These tighten the library defaults
without claiming a preemptive CPU deadline. Detection reuses the rendered Markdown AST, runtime
replacement is atomic, and build notices fall back to declared package licenses or fail when
neither source of license information exists. Mermaid 12.2.0 replaces the version whose KaTeX
dependency was affected by GHSA-238p-pmpm-9mq7.

Current contracts: [preview guide](../../security/preview.md).
