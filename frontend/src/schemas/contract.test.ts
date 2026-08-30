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

// Both WebSocket fixtures are discriminated unions, so "the schema accepts
// the capture" says nothing about the variants the capture never contained.
// These two cases pin which variants the server actually produced, and name
// the ones it does not — the alternative being a union that grows a variant
// no Go code emits, with nothing to notice.
describe('discriminated union coverage', () => {
  function variantsIn(fixture: string): string[] {
    return [...new Set((readFixture(fixture) as { type: string }[]).map((frame) => frame.type))].sort()
  }

  it('captures every control frame /ws/{sessionID} emits', () => {
    expect(variantsIn('ws-control-frames')).toEqual(['replay', 'status'])
  })

  // 'resize' travels browser -> server, so the server never emits it, and
  // 'error' has no producer in internal/ws at all: ControlMessage carries a
  // Message field, but nothing constructs a status/replay frame with one.
  // Both are listed rather than deleted from the schema — useWebSocket
  // parses whatever arrives, and a client that stops accepting a shape the
  // server may later send is its own bug.
  it('names the control variants with no server-side producer', () => {
    const emitted = variantsIn('ws-control-frames')
    expect(['resize', 'error'].filter((variant) => emitted.includes(variant))).toEqual([])
  })

  it('captures every frame /ws/board-command emits', () => {
    expect(variantsIn('ws-board-command-frames')).toEqual(['busy', 'done', 'error', 'line'])
  })
})
