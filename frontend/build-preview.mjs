import { build } from 'esbuild'
import { previewLicenseNotice } from './src/utils/previewLicense.ts'
import { mkdir, readFile, readdir, writeFile } from 'node:fs/promises'

// Mermaid's diagram loaders use dynamic imports. A single IIFE deliberately
// folds them into the trusted classic script: opaque origins need no CORS.
const result = await build({
  entryPoints: ['src/preview/mermaid.ts'],
  bundle: true,
  format: 'iife',
  platform: 'browser',
  target: 'es2020',
  minify: true,
  outfile: 'dist/preview-assets/mermaid.js',
  legalComments: 'inline',
  metafile: true,
  write: false,
})

// Compiled dependencies do not all retain their complete license text. Include
// notices for every package that actually contributes code, without embedding
// build-machine paths or requiring a second asset in the opaque-origin page.
const packages = new Set(Object.keys(result.metafile.inputs).flatMap(input => {
  const match = input.match(/^(.*node_modules\/(?:@[^/]+\/)?[^/]+)\//)
  return match ? [match[1]] : []
}))
const notices = []
for (const directory of [...packages].sort()) {
  const pkg = JSON.parse(await readFile(`${directory}/package.json`, 'utf8'))
  const files = (await readdir(directory, { withFileTypes: true }))
    .filter(file => file.isFile() && /^(?:licen[cs]e|notice)(?:[-.].*)?$/i.test(file.name))
    .map(file => file.name).sort()
  const texts = await Promise.all(files.map(file => readFile(`${directory}/${file}`, 'utf8')))
  notices.push(previewLicenseNotice(pkg, texts))
}
const licenseComment = `\n/*! Third-party notices\n${notices.join('\n\n').replace(/\*\//g, '* /')}\n*/\n`
await mkdir('dist/preview-assets', { recursive: true })
await writeFile('dist/preview-assets/mermaid.js',
  Buffer.concat([result.outputFiles[0].contents, Buffer.from(licenseComment)]))
