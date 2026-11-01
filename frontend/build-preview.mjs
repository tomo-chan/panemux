import { build } from 'esbuild'

// Mermaid's diagram loaders use dynamic imports. A single IIFE deliberately
// folds them into the trusted classic script: opaque origins need no CORS.
await build({
  entryPoints: ['src/preview/mermaid.ts'],
  bundle: true,
  format: 'iife',
  platform: 'browser',
  target: 'es2020',
  minify: true,
  outfile: 'dist/preview-assets/mermaid.js',
  legalComments: 'inline',
})
