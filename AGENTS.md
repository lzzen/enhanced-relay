# AGENTS.md — Orientation for AI contributors

Read this first. Detailed rules live in `.cursor/rules/` and design in `docs/`.

## What this is

An observable, programmable AI gateway built by **refactoring new-api** (not a
rewrite). We reuse new-api's user/billing/auth/console and build a new data-plane
pipeline with hooks, events, and plugins. See `docs/review-notes.md` for decisions.

## Golden rule

Humans define intent (requirements + tests + thresholds); AI implements and the
**automated acceptance gate** verifies. Never hand a human something the gate
hasn't verified. Never weaken the gate to pass.

## Commands

| Command | Use |
| --- | --- |
| `make verify-fast` | fast inner loop (no race) — local Windows OK |
| `make verify` | full gate (race + traceability) — needs CGO |
| `make ci` | full CI incl. mutation (Linux/WSL) |
| `make ci-docker` | full CI in a Linux container (Windows-friendly, no GitHub) |

Results: open `build/dashboard.html`. Evidence: `build/*.json`.

## Layout

```
cmd/gateway/        service entry (transport lands in Phase 0)
cmd/acceptance/     the acceptance gate (tests + traceability -> reports)
cmd/dashboard/      renders build/dashboard.html from evidence
internal/clock,idgen        determinism kit (inject, never call time/uuid directly)
internal/reqctx             RequestContext (the pipeline spine)
internal/hook,event,plugin  extension framework
internal/pipeline           stage orchestration
acceptance/requirements.json  requirement -> priority manifest (gate source)
scripts/ci.sh               the one CI pipeline (local == CI)
```

## Workflow (see .cursor/rules/00-core-workflow.mdc)

1. Add/confirm a `REQ-...` id in `acceptance/requirements.json`.
2. Write a failing test binding it via `req.Covers(t, "REQ-...")`.
3. Implement; make `make ci-docker` green.
4. Do not commit unless asked. Do not skip/weaken tests or golden files.

## Non-negotiables

- Determinism: inject `clock`/`idgen`; no direct `time.Now`/`uuid`/`rand`/`net`
  in `internal/**` outside tests.
- Reuse new-api subsystems; the real upstream target is server-config only (no
  SSRF/open-proxy). See `.cursor/rules/40-newapi-integration.mdc`.
