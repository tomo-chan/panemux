---
type: Decision
title: "2026-10-16: Localhost pages in the split preview, and one URL opener (#386)"
description: "Frame local development servers directly with constrained destinations and approval for output-selected links."
---

# 2026-10-16: Localhost pages in the split preview, and one URL opener (#386)

Direct framing preserves development servers' absolute resource paths and hot reload; proxying
under a path breaks them and another listener is unnecessary locally. Pages keep their own origin
for modules while the dashboard API guard denies the other port.
Review found redirects escaping the localhost preview. A srcdoc wrapper CSP constrains destinations;
IPv6 loopback becomes localhost because CSP cannot name [::1], accepting localhost cookies/storage
instead of removing IPv6 support. Cross-origin failure is unobservable, so explain limits instead
of server probing. The shared URL opener also routes output-chosen OSC 8 links through approval.
See [preview security](../../security/preview.md) and [behavior](../../behavior.md).
