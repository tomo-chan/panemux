import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { z } from 'zod'
import * as schemas from './index'

// Gate G3(c) in docs/quality-gateway.md — Zod schema round-trips — and issue
// #191's item (c).
//
// index.test.ts, alongside this file, has 103 cases and not one of them sees
// JSON that Go produced: every input is a hand-written TypeScript object. So
// a field renamed on a Go struct, with the Zod schema left alone, keeps BOTH
// suites green and surfaces only when the dashboard fails to parse a live
// response in someone's browser. That is what "no round-trip" actually cost.
//
// Every fixture read here was captured from the router server.New() builds,
// by internal/server/contract_fixture_test.go, which rewrites them on every
// Go test run. The Go suite runs first — `make test`, `make check` and
// ci.yml all order it that way — so a Go-side rename reaches the fixture
// before vitest reads it and *this* file is what goes red.
//
// See ../../../testdata/api-contract/README.md for the capture rules and the
// short list of values normalized out of them.

// Resolved from the vitest root (frontend/) rather than from import.meta.url:
// under the jsdom environment these tests run in, import.meta.url is an http:
// URL, not a file: one.
const fixtureDir = resolve(process.cwd(), '..', 'testdata', 'api-contract')

if (!existsSync(fixtureDir)) {
  throw new Error(
    `no API contract fixtures at ${fixtureDir}. They are written by ` +
      '`make test-go` (internal/server/contract_fixture_test.go); see ' +
      'testdata/api-contract/README.md.'
  )
}

function readFixture(name: string): unknown {
  return JSON.parse(readFileSync(resolve(fixtureDir, `${name}.json`), 'utf8'))
}

interface RoundTrip {
  /** Fixture file name, without .json. */
  fixture: string
  /** The exported schema that owns this response, by name. */
  schemaName: keyof typeof schemas
  /** What to parse the fixture with — the schema itself, or an array of it. */
  schema: z.ZodTypeAny
}

// Every schema the frontend parses a *server response* with, paired with the
// captured response it must accept.
const roundTrips: RoundTrip[] = [
  {
    fixture: 'workspaces',
    schemaName: 'WorkspacesResponseSchema',
    schema: schemas.WorkspacesResponseSchema,
  },
  { fixture: 'display', schemaName: 'DisplayConfigSchema', schema: schemas.DisplayConfigSchema },
  {
    fixture: 'detect-shell',
    schemaName: 'DetectShellResponseSchema',
    schema: schemas.DetectShellResponseSchema,
  },
  {
    fixture: 'ssh-connections',
    schemaName: 'SSHConnectionsResponseSchema',
    schema: schemas.SSHConnectionsResponseSchema,
  },
  {
    fixture: 'ssh-config-hosts',
    schemaName: 'SSHConfigHostsResponseSchema',
    schema: schemas.SSHConfigHostsResponseSchema,
  },
  {
    fixture: 'directories',
    schemaName: 'DirectoryBrowserResponseSchema',
    schema: schemas.DirectoryBrowserResponseSchema,
  },
  { fixture: 'sessions', schemaName: 'SessionInfoListSchema', schema: schemas.SessionInfoListSchema },
  { fixture: 'git-info', schemaName: 'GitInfoSchema', schema: schemas.GitInfoSchema },
  {
    fixture: 'session-token',
    schemaName: 'BoardSessionTokenResponseSchema',
    schema: schemas.BoardSessionTokenResponseSchema,
  },
  { fixture: 'open-url', schemaName: 'OpenUrlResponseSchema', schema: schemas.OpenUrlResponseSchema },
  {
    fixture: 'board-status',
    schemaName: 'BoardStatusResponseSchema',
    schema: schemas.BoardStatusResponseSchema,
  },
  {
    fixture: 'board-messages',
    schemaName: 'BoardMessagesResponseSchema',
    schema: schemas.BoardMessagesResponseSchema,
  },
  {
    fixture: 'board-command-history',
    schemaName: 'BoardCommandHistoryResponseSchema',
    schema: schemas.BoardCommandHistoryResponseSchema,
  },
  // The two WebSocket fixtures hold one frame per element, in the order the
  // server sent them, so the schema that owns a single frame is wrapped here
  // rather than restated.
  {
    fixture: 'ws-control-frames',
    schemaName: 'WSControlMessageSchema',
    schema: z.array(schemas.WSControlMessageSchema),
  },
  {
    fixture: 'ws-board-command-frames',
    schemaName: 'BoardCommandFrameSchema',
    schema: z.array(schemas.BoardCommandFrameSchema),
  },
]

// Every other exported schema, with the reason it has no fixture of its own.
// The list is what makes the exhaustiveness check below readable rather than
// merely red: a schema added without a fixture has to be classified here, in
// the diff, and "component of X" is a claim a reviewer can check.
const fixtureless: Record<string, string> = {
  // Request bodies. panemux never receives these from the server, so there is
  // no server output to round-trip them against; index.test.ts covers them.
  CreateSessionRequestSchema: 'request body sent to POST /api/sessions',
  WorkspaceTabPositionRequestSchema: 'request body sent to PUT /api/workspaces/tab-position',
  WorkspaceVerticalBarWidthRequestSchema: 'request body sent to PUT /api/workspaces/vertical-bar-width',
  WorkspaceVerticalBarWidthSchema: 'the bare number inside WorkspaceVerticalBarWidthRequestSchema',

  // Components. Each is reached, and therefore validated, through a response
  // schema above — naming a fixture for it as well would assert nothing new.
  BoardModeSchema: 'component of PaneConfigSchema, via workspaces',
  PaneAgentBoardConfigSchema: 'component of PaneConfigSchema, via workspaces',
  PaneConfigSchema: 'component of LayoutNodeSchema, via workspaces',
  LayoutChildSchema: 'component of LayoutNodeSchema, via workspaces',
  LayoutNodeSchema: 'component of WorkspaceSchema, via workspaces',
  WorkspaceSchema: 'component of WorkspacesResponseSchema',
  TabPositionSchema: 'component of WorkspacesResponseSchema',
  SessionInfoSchema: 'component of SessionInfoListSchema',
  SessionStateSchema: 'component of WSControlMessageSchema',
  WorktreeInfoSchema: 'component of GitInfoSchema',
  SSHConfigHostSchema: 'component of SSHConfigHostsResponseSchema',
  DirectoryEntrySchema: 'component of DirectoryBrowserResponseSchema',
  BoardStatusEntrySchema: 'component of BoardStatusResponseSchema',
  BoardMessageSchema: 'component of BoardMessagesResponseSchema',
  BoardCommandHistoryEntrySchema: 'component of BoardCommandHistoryResponseSchema',
}

function exportedSchemaNames(): string[] {
  return Object.keys(schemas)
    .filter((name) => name.endsWith('Schema'))
    .sort()
}

describe('API contract fixtures', () => {
  it.each(roundTrips)('$schemaName accepts the captured $fixture response', ({ fixture, schema }) => {
    const captured = readFixture(fixture)

    const result = schema.safeParse(captured)
    expect(
      result.success ? null : JSON.stringify(result.error.issues, null, 2)
    ).toBeNull()
  })

  // Parsing is only half the contract. Zod *strips* keys a schema does not
  // declare, so a field renamed on a Go struct — the exact failure #191
  // describes — sails through safeParse whenever the old name was optional:
  // the old key is simply absent and the new one is silently dropped.
  // Requiring the parsed value to equal the captured one byte for byte is
  // what closes that: anything Zod dropped shows up as a difference.
  it.each(roundTrips)('$schemaName drops nothing from the captured $fixture response', ({ fixture, schema }) => {
    const captured = readFixture(fixture)

    expect(schema.parse(captured)).toEqual(captured)
  })

  // A schema the frontend parses a response with, and that nothing here
  // captures, is a contract nobody is checking. Adding one forces a choice:
  // give it a fixture, or say in `fixtureless` why it does not need one.
  it('classifies every exported schema as round-tripped or explained', () => {
    const roundTripped = roundTrips.map((r) => r.schemaName as string)
    const unclassified = exportedSchemaNames().filter(
      (name) => !roundTripped.includes(name) && !(name in fixtureless)
    )

    expect(unclassified).toEqual([])
  })

  // The complement: `fixtureless` must not outlive the schemas it names, or
  // it becomes a list of excuses for code that no longer exists.
  it('has no stale entries in the fixtureless list', () => {
    const exported = exportedSchemaNames()
    const stale = Object.keys(fixtureless).filter((name) => !exported.includes(name))

    expect(stale).toEqual([])
  })

  // Every captured file must be claimed by a round-trip above. Without this,
  // renaming a capture on the Go side leaves the old file behind and this
  // suite keeps happily validating a contract that no longer exists.
  it('reads every fixture the Go side captured', () => {
    const onDisk = readdirSync(fixtureDir)
      .filter((name) => name.endsWith('.json'))
      .map((name) => name.replace(/\.json$/, ''))
      .sort()

    expect(onDisk).toEqual(roundTrips.map((r) => r.fixture).sort())
  })
})

// ── Optional fields the captures never populate ───────────────────────────
//
// Accepting a capture proves nothing about a field the capture does not
// contain. `OpenUrlResponseSchema.port` is the case that made this necessary:
// every capture of POST /api/sessions/{id}/open-url goes through a *local*
// pane, which always answers `forwarded:false` with no `port`, so renaming
// that field on the Go side changed nothing anywhere and both suites stayed
// green.
//
// This is derived rather than declared. The Go side used to carry a prose note
// per fixture plus a hand-written list of the fixtures carrying one, which
// compared a set against itself: it could catch a declared gap going
// undeclared and nothing else. Optionality is stated in the schemas, so the
// walk below reads it from there.
//
// The rule: for every object the captured data contains, every optional key of
// the schema at that position which is absent from the data is an unexercised
// optional. Walking data-and-schema together rather than enumerating the
// schema alone is what makes it terminate — LayoutChildSchema is z.lazy and
// self-referential, and z.lazy hands back a fresh object on every call, so
// there is no schema identity to break a cycle on. The cost is that an
// optional nested under an absent optional is not reported (GitInfoSchema's
// `worktrees[].branch` is invisible while `worktrees` itself is absent); the
// parent is reported, which is the actionable half.

interface Coverage {
  /** Every optional field path the captures reach at all. */
  seen: Set<string>
  /** Those the captures populate at least once. */
  populated: Set<string>
}

/** Unwraps the modifier and indirection wrappers to the schema underneath. */
function unwrap(schema: z.ZodTypeAny, value: unknown): z.ZodTypeAny {
  const def = schema._def as { typeName?: string; innerType?: z.ZodTypeAny; getter?: () => z.ZodTypeAny }
  switch (def.typeName) {
    case 'ZodOptional':
    case 'ZodNullable':
    case 'ZodDefault':
      return unwrap(def.innerType as z.ZodTypeAny, value)
    case 'ZodLazy':
      return unwrap((def.getter as () => z.ZodTypeAny)(), value)
    case 'ZodUnion':
    case 'ZodDiscriminatedUnion': {
      // Pick the member this value actually is; a union whose members disagree
      // about optionality would otherwise be unanswerable.
      const options = (schema as unknown as { options: z.ZodTypeAny[] }).options
      const matched = options.find((option) => option.safeParse(value).success)
      return matched ? unwrap(matched, value) : schema
    }
    default:
      return schema
  }
}

// Appends a segment, collapsing a path that has just repeated itself.
//
// Without this the walk cannot terminate usefully on a recursive schema.
// LayoutChildSchema contains an array of itself, so a two-level split produces
// `…children[]` and `…children[].children[]` as *different* paths — and the
// deepest level never populates `direction` or `children`, because something
// has to be the leaf. Adding a level to satisfy it just creates a deeper level
// with the same gap, forever.
//
// So a suffix that equals the block immediately before it is dropped:
// `.children [] .children []` becomes `.children []`, and every depth of the
// layout tree is measured as the one schema it actually is.
function push(path: string[], segment: string): string[] {
  const next = [...path, segment]
  for (let size = 1; size * 2 <= next.length; size++) {
    const tail = next.slice(next.length - size)
    const before = next.slice(next.length - size * 2, next.length - size)
    if (tail.every((seg, i) => seg === before[i])) return next.slice(0, next.length - size)
  }
  return next
}

function collect(schema: z.ZodTypeAny, value: unknown, path: string[], out: Coverage): void {
  const resolved = unwrap(schema, value)
  const def = resolved._def as { typeName?: string; type?: z.ZodTypeAny; valueType?: z.ZodTypeAny }

  if (def.typeName === 'ZodArray' && Array.isArray(value)) {
    for (const item of value) collect(def.type as z.ZodTypeAny, item, push(path, '[]'), out)
    return
  }
  if (def.typeName === 'ZodRecord' && value !== null && typeof value === 'object') {
    for (const item of Object.values(value)) {
      collect(def.valueType as z.ZodTypeAny, item, push(path, '{}'), out)
    }
    return
  }
  if (def.typeName !== 'ZodObject' || value === null || typeof value !== 'object') return

  const shape = (resolved as unknown as { shape: Record<string, z.ZodTypeAny> }).shape
  const data = value as Record<string, unknown>
  for (const [key, child] of Object.entries(shape)) {
    const childPath = push(path, `.${key}`)
    const joined = childPath.join('')
    if (child.isOptional()) out.seen.add(joined)
    if (!(key in data)) continue
    out.populated.add(joined)
    collect(child, data[key], childPath, out)
  }
}

// Populated *anywhere* in the corpus counts. Array indices collapse to `[]`
// and record keys to `{}`, so one ssh pane carrying `connection` covers the
// key for every sibling that omits it — the question the gate asks is whether
// a renamed Go tag would leave the key missing everywhere, not whether every
// element happens to carry it.
function unexercisedOptionalPaths(): string[] {
  const out: Coverage = { seen: new Set(), populated: new Set() }
  for (const { fixture, schema } of roundTrips) {
    collect(schema, readFixture(fixture), [fixture], out)
  }
  return [...out.seen].filter((path) => !out.populated.has(path)).sort()
}

// Every optional field no capture populates, with the reason it is not
// captured. A new entry has to be justified here, in the diff — and an entry
// that starts being exercised has to be deleted, so the list cannot rot into a
// set of excuses for coverage that already exists.
const unexercisedOptionals: Record<string, string> = {
  // Only reachable for a pane whose live cwd is a real repository: that needs
  // session.CWDGetter (procfs on Linux, lsof on macOS) and a `gh pr view`
  // lookup, neither of which make check may depend on.
  'git-info.branch': 'needs a pane whose live cwd is a real git repository',
  'git-info.repo': 'needs a pane whose live cwd is a real git repository',
  'git-info.repo_url': 'needs a pane whose live cwd is a real git repository',
  'git-info.pr_number': 'needs a `gh pr view` lookup against a real PR',
  'git-info.pr_url': 'needs a `gh pr view` lookup against a real PR',
  'git-info.worktrees': 'needs a pane whose live cwd is a real git repository',

  // forwarded:true with a port needs an *ssh* pane, so panemux has somewhere
  // to forward to — a second host make check cannot assume. A local pane's
  // callback already resolves on this host, so the answer is always
  // forwarded:false with a reason and no port.
  'open-url.port': 'a forward is only opened for an ssh pane, which needs a second host',
}

describe('optional field coverage', () => {
  it('names every optional field the captures leave unpopulated', () => {
    expect(unexercisedOptionalPaths()).toEqual(Object.keys(unexercisedOptionals).sort())
  })

  // The walker is the only thing standing behind the list above, so it is
  // checked against a schema whose answer is known rather than trusted.
  it('sees through arrays, records, lazy recursion and unions', () => {
    const inner = z.object({ required: z.string(), missing: z.string().optional() })
    const schema = z.object({
      list: z.array(inner),
      map: z.record(z.string(), inner),
      lazy: z.lazy(() => inner),
      union: z.discriminatedUnion('type', [
        z.object({ type: z.literal('a'), onlyOnA: z.string().optional() }),
        z.object({ type: z.literal('b'), onlyOnB: z.string().optional() }),
      ]),
    })
    const data = {
      list: [{ required: 'x' }],
      map: { k: { required: 'x' } },
      lazy: { required: 'x' },
      union: { type: 'a' },
    }

    const out: Coverage = { seen: new Set(), populated: new Set() }
    collect(schema, data, ['root'], out)

    // onlyOnB is absent because the value is the 'a' member, not because the
    // capture failed to populate it — picking the matching member is what
    // keeps a union from reporting every other member's fields as gaps.
    expect([...out.seen].filter((path) => !out.populated.has(path)).sort()).toEqual([
      'root.lazy.missing',
      'root.list[].missing',
      'root.map{}.missing',
      'root.union.onlyOnA',
    ])
  })

  // The rule the layout tree depends on, checked on its own: a self-recursive
  // schema must be measured as one schema however deep the data nests it.
  // Without the collapse, `kids[].kids[].label` is a separate path from
  // `kids[].label` and the deepest level can never populate `kids` — so the
  // gap list would grow by one entry for every level a fixture happens to
  // have, none of them actionable.
  it('measures a self-recursive schema at one depth', () => {
    interface Node {
      label?: string
      kids?: Node[]
    }
    const NodeSchema: z.ZodType<Node> = z.lazy(() =>
      z.object({ label: z.string().optional(), kids: z.array(NodeSchema).optional() })
    )
    // Three levels deep, and deliberately sparse at each: the middle node has
    // no label, the leaf has no kids.
    const data = { label: 'root', kids: [{ kids: [{ label: 'leaf' }] }] }

    const out: Coverage = { seen: new Set(), populated: new Set() }
    collect(NodeSchema, data, ['tree'], out)

    // Four paths, not six: the third level collapses onto the second. The root
    // stays distinct from the nested ones because it genuinely is a different
    // position — the same split the real tree has between LayoutNodeSchema and
    // LayoutChildSchema — but no depth beyond it adds anything, which is the
    // property the layout fixture depends on.
    expect([...out.seen].sort()).toEqual(['tree.kids', 'tree.kids[].kids', 'tree.kids[].label', 'tree.label'])
    // All four are populated somewhere: `label` on the root and the leaf,
    // `kids` on the root and the middle node. Sparseness at one level is not a
    // gap when a sibling occurrence covers it.
    expect([...out.seen].filter((path) => !out.populated.has(path))).toEqual([])
  })
})

// ── Discriminated union variants ──────────────────────────────────────────
//
// "The schema accepts the capture" says nothing about the variants the capture
// never contained. The first version of these cases compared the fixture's own
// variants to a hardcoded array, which cannot see the failure they were
// written for: adding a member no Go code emits leaves the fixture untouched,
// so both cases still passed. The declared set has to come from the schema.

/** The `type` literal of every member of a discriminated union. */
function declaredVariants(schema: z.ZodTypeAny): string[] {
  const options = (schema as unknown as { options: z.ZodTypeAny[] }).options
  return options
    .map((option) => {
      const shape = (option as unknown as { shape: Record<string, z.ZodTypeAny> }).shape
      return (shape.type._def as unknown as { value: string }).value
    })
    .sort()
}

describe('discriminated union coverage', () => {
  function variantsIn(fixture: string): string[] {
    return [...new Set((readFixture(fixture) as { type: string }[]).map((frame) => frame.type))].sort()
  }

  // 'resize' travels browser -> server, so the server never emits it, and
  // 'error' has no producer in internal/ws at all: ControlMessage carries a
  // Message field, but nothing constructs a frame with one. Both stay in the
  // schema rather than being deleted — useWebSocket parses whatever arrives,
  // and a client that stops accepting a shape the server may later send is its
  // own bug — so they are classified here instead.
  it('classifies every WSControlMessage variant as emitted or accounted for', () => {
    const emitted = variantsIn('ws-control-frames')

    expect(emitted).toEqual(['replay', 'status'])
    expect(declaredVariants(schemas.WSControlMessageSchema).filter((v) => !emitted.includes(v))).toEqual([
      'error',
      'resize',
    ])
  })

  it('emits every BoardCommandFrame variant the schema declares', () => {
    const emitted = variantsIn('ws-board-command-frames')

    expect(emitted).toEqual(['busy', 'done', 'error', 'line'])
    expect(declaredVariants(schemas.BoardCommandFrameSchema).filter((v) => !emitted.includes(v))).toEqual([])
  })
})
