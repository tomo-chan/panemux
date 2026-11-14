# Preview decisions

## Decisions

* [2026-10-15: The split preview's isolation and bounds (#385)](file-preview-isolation.md) - Use opaque-origin isolation and root-based lookup instead of arbitrary absolute host-file previews.
* [2026-10-16: Localhost pages in the split preview, and one URL opener (#386)](localhost-preview.md) - Frame local development servers directly with constrained destinations and approval for output-selected links.
* [2026-11-01: Mermaid inside isolated Markdown previews (#471)](mermaid-runtime.md) - Bundle a nonce-authorized classic script for isolated Markdown diagrams instead of adding a runtime browser.
* [2026-11-02: Mermaid measurement and final-output sanitization (#477 review)](mermaid-sanitization.md) - Apply image policy during diagram measurement because final SVG sanitization cannot retract earlier requests.
* [2026-10-16: Previewing an SSH host's files and localhost URLs (#387)](remote-preview.md) - Read files through SFTP and stable separate relays while preserving pane host identity and tab lifetime.
