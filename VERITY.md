# VERITY.md — Quality Gate

> This project uses [Verity](https://verity.md) to enforce quality and security standards on AI-generated code.

**URL:** https://ofcamwrjwrkazqvdchko.supabase.co/functions/v1
**Project:** this repository
**Standard:** v1 (derived from this codebase · Trivy)

## Quality Dimensions
- comprehensibility
- modularity
- type_safety
- test_adequacy

## Security Patterns
- no-hardcoded-secrets
- input-sanitization
- parameterized-queries
- dependency-verification
- no-unsafe-deserialization
- access-control-checks
- config-file-integrity

## How It Works
This repository registers only a `SessionStart` hook, which snapshots the
working tree with `verity baseline capture` (skipped when the `verity` CLI is
not installed). Reviews run on demand — `verity analyze`, or the
`/verity-analyze` skill that `verity init` installs per machine — and:
1. Run static analysis via @codacy/analysis-cli
2. Send results + code to the Verity service
3. Have an independent model review the code
4. Return PASS / WARN / FAIL with actionable findings

_This Standard was derived from the codebase: languages, frameworks and
architecture were detected, and the rules come from Verity's research-backed
patterns reference. The project-specific rules are defined locally in
`custom_patterns` in `.verity/standard.yaml`; run `verity standard push` to
publish changes to them._
