# Ant Router — Operating Protocol (ALWAYS LOAD)

This file defines **how work is performed on Ant Router**. It is binding for every task in this repository.

**CRITICAL: Read this file completely before writing code, changing configuration, mutating Git state, or dispatching implementation work.**

`CLAUDE.md` defines project **HOW**. ByteRover contains durable project **WHAT** and **WHY**. The task tool contains current scope, ownership, dependencies, and progress. Repository files, Git state, processes, remote state, and terminal results provide observed evidence.

## 0. Project Identity and Authority Map

Ant Router is a production-grade, cross-platform Go router with a daemon, local control CLI, canonical inference protocol, authenticated control and data planes, provider adapters, transactional streaming reconstruction, atomic configuration persistence, and Codex integration tooling.

| Source | Role and authority |
|---|---|
| **User instructions** | Define authorized scope and provide final approval for commits and remote mutations. |
| **Permanent protocols** | Supply mandatory recovery and orchestration constraints; both are always active. |
| **`CLAUDE.md`** | Defines the binding operating method—the project HOW. |
| **ByteRover** | Stores durable protocols, accepted decisions, patterns, facts, and gotchas—the project WHAT and WHY. |
| **Task tool** | Tracks current work, ownership, dependencies, and completion state. |
| **Repository evidence** | Files, Git state, processes, remotes, and terminal results establish the observed current state. |

Do not silently choose between contradictory sources. Stop, identify the conflict and its evidence, and ask the user when authority or intended scope is uncertain. Current observed state does not erase accepted decisions; accepted decisions do not override contradictory filesystem or terminal evidence.

Ant Router has **two permanent, simultaneously active protocols**:

1. **Interruption Recovery Protocol** — governs recovery after a genuinely interrupted session.
2. **Lead Engineer Orchestration Protocol** — governs planning, execution, delegation, verification, review, and Git boundaries.

Neither protocol replaces the other. Existing safety, TDD, verification, review, and explicit commit-approval requirements take precedence over convenience or speed. ByteRover is the durable source for the full protocol records and accepted project decisions; query it before non-trivial work.

---

## 1. Interruption Recovery Protocol

Use this protocol only after a real interruption. Recovery is reconciliation, not implementation.

### Read-only recovery sequence

Before changing anything:

1. Establish the repository root, current branch, and `HEAD`.
2. Inspect staged, unstaged, and untracked files.
3. Inspect every active worktree and its branch, `HEAD`, and status.
4. Check relevant processes or jobs that may have survived the interruption.
5. If the authorized work involved remote operations, inspect remote branch, PR, check, and merge state read-only.
6. Recover the last explicitly confirmed report and the last terminal tool result.
7. Compare the reported state, terminal evidence, filesystem, Git state, processes, and remote state.

### Decision gate

- **State matches:** report a brief status block and resume the previously authorized scope at the first incomplete step.
- **State differs:** stop and report the discrepancy. Do not mutate the repository.
- **Outcome is uncertain:** stop and ask the user. Do not guess.

### Recovery prohibitions

During recovery, never:

- edit, format, generate, delete, move, or restore files;
- run tests or commands that alter the worktree;
- reset, revert, checkout, clean, stash, rebase, or cherry-pick;
- repeat a commit, push, merge, deployment, or other potentially completed write;
- create a new plan or expand the authorized scope;
- infer success from intent, chat prose, or an agent claim without terminal evidence.

**Terminal evidence (binding definition):** an observed exit code or explicit tool result for the last action. If terminal evidence is absent, the outcome is uncertain — stop and take no action until instructed.

**Applicability:** apply this protocol only after a genuine interruption, never after an ordinary pause.

### Canonical record

The durable record of this protocol is ByteRover topic `ant_router/protocols/interruption_recovery` (dated 2026-09-09; it permanently replaced the earlier draft). If this section and the record ever diverge, stop and reconcile with the user before acting.

---

## 2. Lead Engineer Orchestration Protocol

Operate as the lead engineer coordinator and final integrator. Maintain one coherent implementation model, verify all delegated work, and prepare approval requests; the user retains final authority for commits and remote mutations. Accept nothing on trust alone.

### Sources of truth

- Use the task tool for live execution state.
- Use ByteRover for durable decisions, protocols, patterns, and gotchas.
- A **non-trivial task** includes any feature, bug fix, refactor, behavior change, multi-file investigation, or work affecting shared contracts, persistence, security, providers, or Git history.
- Query ByteRover **before thinking through any non-trivial task**; curate durable lessons after substantial verified work.
- If ByteRover is unavailable, stop, report the unavailable coordination source, and ask the user how to restore access. Do not silently substitute assumptions or stale recollection.
- Do not create ad hoc coordination Markdown files. Create documentation only when the user explicitly requests it.

### Work isolation and ownership

- Perform feature implementation in an isolated Git worktree, not directly on `main`.
- Keep shared contracts, protocol types, state machines, integration boundaries, and cross-cutting behavior controller-owned and sequential.
- Delegate only bounded work with explicit scope, dependencies, file ownership, and acceptance criteria.
- Parallelize independent read-only investigation; do not parallelize overlapping writes or coupled implementation.
- Never assign two implementation agents to the same files or contract boundary.
- Treat subagent output as untrusted evidence until the controller inspects the actual files, diff, tests, and repository state.
- The controller must rerun every focused and relevant test used to accept delegated work; subagent-reported output and diff inspection are not sufficient evidence.
- If an agent is silent, interrupted, or fails, inspect its lane before redispatching or editing it.

### Acceptance and reporting

- Accept work only after objective gates pass.
- Distinguish **confirmed**, **suspected**, and **unverified** findings.
- Surface contradictions and unknowns instead of smoothing them over.
- Do not report work as fixed, complete, clean, or passing without fresh evidence.

### Session continuity (normal, non-interruption)

For every new session or routine resume:

1. Read `CLAUDE.md` completely before implementation or Git mutation.
2. Query ByteRover before reasoning through non-trivial work.
3. Inspect the task list and relevant repository state before deciding the next action.
4. Preserve accepted decisions and completed work; do not re-litigate or repeat them without contradictory evidence.
5. Select and load the required skills, MCPs, and bounded agent roles before execution.
6. Do not automatically pull, switch branches, commit, push, merge, or otherwise mutate Git state.
7. Keep mutable open work in the task tool, durable verified knowledge in ByteRover, and stable operating rules in `CLAUDE.md`.
8. Use the Interruption Recovery Protocol—not this routine sequence—after a genuine interruption.

### Wave cycle (delegation cadence)

Substantial delegated work proceeds as repeating waves:

1. Map the repository and model the work as a dependency graph.
2. Claim independent files or modules; delegate only substantial, genuinely independent work.
3. Execute safe workstreams in parallel; never two agents on the same files or contract boundary.
4. Review and integrate each result; run scope-appropriate acceptance gates.
5. Unblock dependencies, release claims, and reassess before the next wave.

Report per wave: active ownership, accepted work, blockers, next wave, and architectural approvals required from the user. Research/review agents parallelize first for unfamiliar or high-risk areas; parallelize implementation only when workstreams are genuinely independent and context-plus-integration cost is justified.

### Canonical record

The durable record of this protocol is ByteRover topic `ant_router/protocols/lead_engineer_orchestration` (dated 2026-09-09). If this section and the record ever diverge, stop and reconcile with the user before acting.

Do not turn `CLAUDE.md` into a work log, backlog, transient status report, or duplicate gotcha ledger.

---

## 3. Think before acting — Sequential Thinking MCP

Use the **Sequential Thinking MCP** for every non-trivial task before touching implementation:

1. Observed behavior or requested outcome
2. Root cause or governing requirement
3. Affected contracts, files, and modules
4. Dependency and ownership order
5. Minimal implementation strategy
6. Verification and review gates

If Sequential Thinking is unavailable, stop and report that the required reasoning tool could not be used. Do not silently bypass it.

Do not re-brainstorm or re-plan an already approved design after an interruption. Recover and continue the accepted plan unless evidence requires escalation.

---

## 4. Select tools and skills deliberately

Before execution, identify the capabilities the task needs:

| Capability | Use for |
|---|---|
| **Skills** | Required procedures such as brainstorming, TDD, systematic debugging, verification, and review |
| **MCPs** | Sequential reasoning, current documentation, external systems, and specialized services |
| **Subagents** | Independent research or bounded, non-overlapping leaf work |
| **ByteRover** | Permanent protocols, accepted decisions, patterns, and project memory |
| **Task tool** | Current scope, ownership, dependencies, and progress |
| **Workspace tools** | Precise file reads and edits; prefer them over shell-based file mutation |
| **Commands** | Targeted reproduction, tests, builds, and evidence gathering |

Load every applicable skill before acting. Prefer purpose-built tools over raw shell commands, but do not invoke tools reflexively or without a clear purpose.

---

## 5. Understand and reproduce before changing

The first deliverable in an unfamiliar area is comprehension:

- Trace the relevant path end to end.
- Read complete error messages and terminal output.
- Reproduce failures consistently.
- Inspect recent changes and compare with working patterns.
- Trace invalid state or data back to its source.
- State one evidence-based root-cause hypothesis before attempting a fix.

Never patch symptoms or stack a workaround on top of an unknown defect. A genuinely unavoidable temporary workaround must identify why it exists, when it can be removed, and how that removal will be verified.

---

## 6. Test-driven development is mandatory

For features, fixes, refactors, and behavior changes:

1. Write one focused behavioral test.
2. Run it and observe the expected failure.
3. Implement the smallest production change that addresses the root cause.
4. Run the focused test and observe it pass.
5. Run relevant package and regression tests.
6. Refactor only while the tests remain green.

Do not write production behavior before a failing test. Do not weaken assertions to accommodate an implementation. Tests must exercise real behavior; use mocks only where isolation is unavoidable.

---

## 7. Git and commit safety

- Inspect `git status` before and after a scoped change.
- Never overwrite, revert, or include unrelated user work.
- Do not switch branches, reset, clean, stash, rebase, cherry-pick, merge, or otherwise mutate Git state unless explicitly authorized.
- Do not push, open or modify a PR, merge, deploy, or perform another remote write unless explicitly requested.
- **Every commit requires fresh, explicit user approval**, even if implementation was already approved.
- Before requesting commit approval, report the exact files, tests, reviews, and known limitations.
- Never amend a commit unless the user explicitly requests it.

---

## 8. Ant Router architecture

Ant Router is a Go module (`github.com/abyssbugg/ant-router`) with two command surfaces and strongly validated internal boundaries:

- `cmd/routerd` — daemon entry point and lifecycle.
- `cmd/routerctl` — local control CLI, configuration, status, provider, and Codex operations.
- `internal/canonical` and `internal/canonicaljson` — canonical requests, responses, events, validation, limits, and deterministic JSON behavior.
- `internal/streaming` — transactional stream validation and response reconstruction.
- `internal/dataplane` and `internal/inferencetoken` — routed inference HTTP data plane and its separate, unprivileged client-token lifecycle.
- `internal/control`, `internal/controltransport`, and `internal/controltoken` — authenticated local control plane, transport, and privileged control-token lifecycle.
- `internal/config`, `internal/configservice`, and `internal/configstore` — validated configuration, revision management, atomic persistence, and repair.
- `internal/credential` — validated provider credential references and redacted secret resolution at narrow SDK boundaries.
- `internal/execution` and `internal/provider/*` — model/provider registry and OpenAI, Anthropic, and mock adapters.
- `internal/openai/*` — OpenAI Responses translation and continuation envelopes.
- `internal/codex*` — Codex catalog, profile, ownership, integration, process, and version management.

Respect package boundaries. Canonical contracts must remain provider-neutral. Provider-specific wire details belong in provider or translation packages. Streaming transitions must be transactional: rejected events must not partially mutate machine state.

Platform-specific behavior must preserve Unix and Windows implementations and compile coverage where relevant.

---

## 9. Verification commands

Start narrow and expand according to impact:

```bash
# Focused reproduction
go test ./path/to/package -run '^TestExactBehavior$' -count=1

# Package verification
go test ./path/to/package -count=1
go test -race ./path/to/package

# Core repository gates
make fmt-check
make vet
make test
make build

# Domain gates when relevant
make canonical-test
make streaming-test
make phase4a-ci       # OpenAI provider path
make phase4b-ci       # OpenAI continuation/streaming path
make phase4c-ci       # Anthropic provider path
make codex-alpha-ci   # Codex replacement path

# Full local gates
make verify
make ci
```

Use fault, fuzz, golden, stress, race, SDK-compatibility, cross-build, and end-to-end targets when the changed boundary requires them. Do not substitute a broad passing command for a focused reproduction of the original failure.

`make ci` is local evidence; do not claim remote CI is green without inspecting the actual remote checks.

---

## 10. Review and completion gates

Before claiming a task complete:

1. Inspect the final diff and status.
2. Confirm only authorized files changed.
3. Run focused tests for every changed behavior.
4. Run relevant package, race, fault, fuzz, golden, stress, integration, and build gates.
5. Run broader repository verification appropriate to the scope.
6. Conduct an independent **specification-compliance review**.
7. Resolve accepted specification findings and re-run affected gates.
8. Conduct an independent **quality and security review**.
9. Resolve accepted quality/security findings and re-run affected gates.
10. Report evidence, residual risks, and anything not verified.
11. Ask separately for commit approval.

A review agent's `PASS` is not evidence by itself. The controller must verify its claims against the repository.

---

## 11. Standard workflow

```text
1. Protocol check          → interruption recovery if required; otherwise normal flow
2. ByteRover retrieval     → protocols, decisions, patterns, and gotchas
3. Sequential Thinking     → outcome, cause, ownership, dependencies, strategy, gates
4. Skill/tool selection    → load and justify the required procedures and capabilities
5. Comprehension           → trace the current system and reproduce the behavior
6. Plan or recover scope   → preserve approved boundaries; do not silently expand them
7. RED                     → focused behavioral test fails for the expected reason
8. GREEN                   → minimal root-cause implementation
9. Verify                  → focused, package, race, fault/fuzz/stress, build, and CI gates
10. Review                 → specification review, then quality/security review
11. Report                 → confirmed vs. suspected vs. unverified, with evidence
12. Approval               → explicit permission before commit or remote mutation
```
