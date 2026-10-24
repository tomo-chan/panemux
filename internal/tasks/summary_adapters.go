package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"panemux/internal/agent"
)

// summaryAdapters is every agent panemux can summarize the sessions of,
// each registered under the registry ID whose Summary capability it
// implements, with the source that reads its conversation and the runner
// that summarizes it. A task is summarized by its own agent's runner; no
// other agent's CLI stands in for it. Nothing registers itself.
// TestSummaryAdapters_MatchTheRegistry keeps this list and the registry's
// declarations in step.
func summaryAdapters() []registeredSummary {
	return []registeredSummary{
		{id: agent.Claude, source: claudeSummarySource{}, runner: claudeSummaryRunner{}},
		{id: agent.Codex, source: codexSummarySource{}, runner: codexSummaryRunner{}},
	}
}

type registeredSummary struct {
	source summarySource
	runner summaryRunner
	id     agent.ID
}

// summarySource is an agent's part of reading a session's conversation on
// the host the session ran on. The task board defines it in its own terms;
// internal/agent only declares that an agent can be summarized.
type summarySource interface {
	// validSessionID reports whether id names a session of the agent that
	// can be summarized. Nothing else is ever put in a script.
	validSessionID(id string) bool
	// logScript is the fixed script that prints the session's log, run on
	// its host. log is the version the collection listed, which a source
	// may hold the read to; it returns ErrInvalidSummary for an ID
	// validSessionID refuses.
	logScript(sessionID string, log LogVersion) (string, error)
	// excerpt is what the runner is given from the script's output. ok is
	// false when the log holds nothing the source understands (unreadable);
	// an error is a log it could not read at all.
	excerpt(out []byte, sessionID string) (excerpt string, ok bool, err error)
	// version names how the excerpt is made. "" is the version every
	// source started at; raise it to anything else when a change would
	// make an earlier excerpt of the same log differ.
	version() string
}

// summaryRunner is an agent's part of summarizing an excerpt on the panemux
// host, with that agent's own CLI, authentication and settings.
type summaryRunner interface {
	// version names what, besides the excerpt, decides the answer: the
	// instruction, and the model and system prompt where they are set.
	version() string
	// model is the model the runner asks for, "" when it leaves the choice
	// to the operator's settings.
	model() string
	newSummarizer() SummarizeFunc
}

// summaryPlanned is the summary adapters, checked against the registry.
type summaryPlanned struct {
	adapters map[agent.ID]registeredSummary
}

var summaryPlan = mustSummaryPlan(agent.All(), summaryAdapters())

func mustSummaryPlan(defs []agent.Definition, regs []registeredSummary) summaryPlanned {
	plan, err := newSummaryPlan(defs, regs)
	//coverage:exempt runs only if summaryAdapters disagrees with the registry, which a test forbids
	if err != nil {
		panic("tasks: " + err.Error())
	}
	return plan
}

// newSummaryPlan checks regs against the registry: each is registered once,
// under an agent that declares Summary, with a source and a runner, and
// every agent that declares Summary has one.
func newSummaryPlan(defs []agent.Definition, regs []registeredSummary) (summaryPlanned, error) {
	known := map[agent.ID]agent.Definition{}
	for _, d := range defs {
		known[d.ID] = d
	}
	plan := summaryPlanned{adapters: map[agent.ID]registeredSummary{}}
	seen := map[agent.ID]bool{}
	for _, r := range regs {
		if err := checkRegistration("summary", agent.Summary, r.id, known, seen); err != nil {
			return summaryPlanned{}, err
		}
		switch {
		case r.source == nil:
			return summaryPlanned{}, fmt.Errorf("summary adapter %q has no source", r.id)
		case r.runner == nil:
			return summaryPlanned{}, fmt.Errorf("summary adapter %q has no runner", r.id)
		}
		plan.adapters[r.id] = r
	}
	for _, d := range defs {
		if d.Has(agent.Summary) && !seen[d.ID] {
			return summaryPlanned{}, fmt.Errorf("agent %q declares summary but has no summary adapter", d.ID)
		}
	}
	return plan, nil
}

// lookup is the adapter of the agent named name, if it has one.
func (p summaryPlanned) lookup(name string) (registeredSummary, bool) {
	r, ok := p.adapters[agent.ID(name)]
	return r, ok
}

// summarizable reports whether a task of agentName with sessionID can be
// summarized: its agent has an adapter, and the adapter accepts the ID.
func (p summaryPlanned) summarizable(agentName, sessionID string) bool {
	r, ok := p.lookup(agentName)
	return ok && r.source.validSessionID(sessionID)
}

// summaryPipelineVersion names what every agent's summaries share: how the
// answer is handled. Raise it when a change there would make an earlier
// summary of the same conversation differ; an adapter's own changes raise
// its source's or runner's version instead. A summary made by another
// version is shown but never current.
const summaryPipelineVersion = 1

// summarizerVersion identifies agentName's summarizer: what, besides the
// conversation, decides its answer. It is "" for an agent with no adapter.
func (p summaryPlanned) summarizerVersion(agentName string) string {
	r, ok := p.lookup(agentName)
	if !ok {
		return ""
	}
	// A source still at its first version adds nothing, so the summaries
	// saved before the adapters existed keep their versions.
	instruction := r.runner.version()
	if v := r.source.version(); v != "" {
		instruction = v + "\x00" + instruction
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%d\x00%s\x00%s\x00%s",
		summaryPipelineVersion, agentName, instruction, summarySchema))
	return hex.EncodeToString(sum[:])
}

// summaryInputHash identifies what agentName's summarizer is given: the
// excerpt, and the summarizer itself. Two equal hashes would get the same
// answer.
func (p summaryPlanned) summaryInputHash(agentName, excerpt string) string {
	sum := sha256.Sum256([]byte(p.summarizerVersion(agentName) + "\x00" + excerpt))
	return hex.EncodeToString(sum[:])
}

// summarizerVersion is the production plan's.
func summarizerVersion(agentName string) string {
	return summaryPlan.summarizerVersion(agentName)
}

// errSummaryCLIMissing is a runner's error when its agent's CLI is not on
// the panemux host's PATH. It names no path.
func errSummaryCLIMissing(id agent.ID) error {
	return fmt.Errorf("%s CLI was not found on the panemux host", id)
}

type claudeSummarySource struct{}

func (claudeSummarySource) validSessionID(id string) bool { return validSessionID.MatchString(id) }

func (claudeSummarySource) logScript(sessionID string, _ LogVersion) (string, error) {
	return buildTranscriptScript(sessionID)
}

func (claudeSummarySource) excerpt(out []byte, _ string) (string, bool, error) {
	data, err := parseTranscriptOutput(out)
	if err != nil {
		return "", false, err
	}
	excerpt, ok := buildExcerpt(data)
	return excerpt, ok, nil
}

func (claudeSummarySource) version() string { return "" }

type claudeSummaryRunner struct{}

// version holds Claude's system prompt and model, which decide its answer
// too (issue #353).
func (claudeSummaryRunner) version() string {
	return summaryInstruction + "\x00" + summarySystemPrompt + "\x00" + summaryModel
}

func (claudeSummaryRunner) model() string { return summaryModel }

func (claudeSummaryRunner) newSummarizer() SummarizeFunc { return newClaudeSummarizer(claudeBin, nil) }

type codexSummarySource struct{}

func (codexSummarySource) validSessionID(id string) bool {
	return validSessionID.MatchString(id) && validUUID.MatchString(id)
}

func (codexSummarySource) logScript(sessionID string, log LogVersion) (string, error) {
	return buildCodexTranscriptScriptForLog(sessionID, log)
}

func (codexSummarySource) excerpt(out []byte, sessionID string) (string, bool, error) {
	data, err := parseCodexTranscriptOutput(out, sessionID)
	if err != nil {
		return "", false, err
	}
	excerpt, ok := buildCodexExcerpt(data)
	return excerpt, ok, nil
}

func (codexSummarySource) version() string { return "" }

type codexSummaryRunner struct{}

func (codexSummaryRunner) version() string { return codexSummaryInstruction }

// model is "": Codex summarizes with the operator's own model setting.
func (codexSummaryRunner) model() string { return "" }

func (codexSummaryRunner) newSummarizer() SummarizeFunc { return newCodexSummarizer() }
