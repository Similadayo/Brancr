# AI Layer Consolidation Plan

_Last updated: 2026-06-02_

## Status checklist (living tracker)

**Track 1 — Cleanup (from initial assessment)** — ✅ complete
- [x] Delete orphan packages (`shadow`, `simulation`, `ai_client`)
- [x] Remove committed `brancr.exe` + root PII log dumps; `.gitignore` the binary

**Track 2 — AI consolidation (5 phases)**
- Phase 0 — Guardrails / test net — 🟡 in progress
  - [x] `go build ./...` + logic tests green as the baseline gate
  - [ ] Enable CGO (install gcc) so the 41 SQLite-backed test files run (`CGO_ENABLED=1 go test ./...`)
- Phase 1 — Extract shared infra from `internal/ai` — 🟡 ~40%
  - [x] LLM chat transport → `internal/llm` (embed+alias)
  - [x] Embeddings transport → `internal/llm`
  - [x] `ParseMenuText` / `ParseMenuFromImage` → free functions in `internal/catalog/ingest`
  - [x] `GenerateBusinessProfile` (`magic_profile.go`) → `internal/onboarding`
  - [x] `GenerateBusinessKnowledge` (`magic_knowledge.go`) → `internal/onboarding`
  - [x] `MemoryService` → `internal/recall` (name `internal/memory` was taken)
  - [ ] `caption_optimizer.go` — blocked: moving to `internal/social/captions` would create an import cycle; needs `AIClientInterface` + `BuildBaseMessages` moved to `internal/llm` first
  - [ ] Delete the `AIClient` shim + aliases once nothing depends on them
- Phase 2 — Telegram customer path → v4 (kills legacy `CustomerResponder` use) — ⬜ gated on test net
- Phase 3 — Absorb v3 `ActionExecutor` into v4 (removes `ai_v4 → ai/v3` dep) — ⬜ gated, highest risk
- Phase 4 — Delete legacy `internal/ai` reasoning + all `internal/ai/v3` (~30k+ LOC) — ⬜ final step

**Separate items (out of scope of the 5 phases — track independently)**
- [ ] Split `tenantapi/handler.go` (~10.7k LOC) by resource
- [ ] Rotate secrets that sat in the deleted root log files / `.env`
- [ ] De-duplicate Go↔Python logic (Nigerian price parsing, browse/variant/negotiation)
- [ ] Rename one `llm` package (`internal/llm` vs `internal/ai_v4/llm` collision)

## Why this exists

The Go backend has accumulated **three generations of AI code** (`internal/ai`,
`internal/ai/v3`, `internal/ai_v4`) totalling ~53k LOC. They are **entangled, not
stacked** — you cannot delete the old ones, because the new one runs on top of them.

The actual LLM reasoning now lives in the **Python service (`brancr-ai`)**. v4 calls
it over HTTP for intent analysis and all text generation. That means the Go "AI"
packages are no longer _reasoning_ code — they are **orchestration + execution +
legacy glue**. The goal of this plan is to collapse that glue down to a single,
clearly-named path, without breaking the live product.

This is a **deliberate refactor**, not a cleanup. Do it when the product is stable,
in small reversible steps, each behind a green build + test run.

### What is already done
**Cleanup pass (2026-06-02)**
- Deleted zero-importer orphan packages: `internal/shadow`, `internal/simulation`,
  `internal/ai_client` (the old Go→Python client, replaced by `ai_v4/python`).
- Removed committed `cmd/brancr/brancr.exe` and root log dumps (contained customer PII).
- Build + test-compile verified green after deletion.

**Phase 1, extraction 1 — LLM transport → `internal/llm` (2026-06-02)**
- Moved the low-level transport (`client.go`, `client_extension.go`, `model_selector.go`)
  out of `internal/ai` into a new `internal/llm` package: `llm.Client` with
  `Generate` / `GenerateWithUsage` / vision / `AnalyzeImage`, plus `Message`,
  `ContentPart`, `TokenUsage`, `GenerateResult`, `ModerationError`, model tier
  constants, and `ModelForIntent` / `ModelSelector`.
- **Technique (use this for the remaining extractions):** `AIClient` could not be a
  plain file-move because it is a god-object whose methods (`ParseMenuText`,
  `GenerateBusinessProfile`, `GenerateBusinessKnowledge`, ...) are spread across 5
  files, and Go requires all methods of a type to live in one package. So instead:
  `internal/ai` now keeps a thin shim — `type AIClient struct { *llm.Client }`
  (embedding promotes the transport methods) plus type aliases
  (`type Message = llm.Message`, etc.). External call sites (`ai.AIClient`,
  `ai.Message`, ...) compile unchanged; only 2 test files that built `&AIClient{...}`
  literals needed updating. This embed-+-alias pattern lets us relocate widely-used
  code with near-zero blast radius and full `go build` verification.
- Verified: `go build ./...` green; `go test ./...` green (CGO off; DB tests skip).

**Phase 1, extraction 2 — embeddings transport → `internal/llm` (2026-06-02)**
- Moved `EmbeddingClient` (`embedding.go`) into `internal/llm` (same embed/alias
  technique; left `type EmbeddingClient = llm.EmbeddingClient` +
  `var NewEmbeddingClient = llm.NewEmbeddingClient` in `internal/ai`). Both OpenAI
  transports (chat + embeddings) now live in `internal/llm`.
- Verified: `go build ./...` + `go test ./...` green.

**Inflection point reached — clean "move + alias" extractions are exhausted.**
The remaining `internal/ai` keepers cannot be relocated by the simple alias trick:
- `caption_optimizer.go` depends on `BuildBaseMessages` (prompt helper in `prompts.go`).
- `menu_parser.go`, `magic_profile.go`, `magic_knowledge.go` define **domain methods
  on the `AIClient` god-object** (`ParseMenuText`, `ParseMenuFromImage`,
  `GenerateBusinessProfile`, `GenerateBusinessKnowledge`).
- `MemoryService` (`memory.go`) — `internal/memory` is already taken (v4 memory),
  and it depends on the `Message` type; needs a new home + alias.

The next sub-phase is **god-object decomposition**: convert those domain methods from
`func (c *AIClient) X(...)` into free functions `func X(c *llm.Client, ...)`, update
the `AIClientInterface` and all call sites, then move them to their domains
(`catalog/ingest`, `onboarding`). This is more invasive and touches runtime features
(menu parsing, profile generation) that **cannot be DB-tested in the current
environment** — so per the gating principle it should wait for a runnable DB test
suite (a `TEST_POSTGRES_DSN`) or be done with extra manual care.

---

## Target architecture (where we want to land)

```
Customer message
      │
      ▼
 social/MessageHandler ──► ai_v4 (orchestrator / authority)
                              │  • policy, catalog isolation, order math, delivery
                              │
                              ├──► brancr-ai (Python)        = the brain (LLM)
                              │     intent analysis, text generation
                              │
                              ├──► execution engine           = catalog/cart/payment actions
                              │     (today this is internal/ai/v3 — to be absorbed)
                              │
                              └──► shared infra               = embeddings, memory, client
                                    (today in internal/ai — to be extracted)
```

End state: **one** Go AI package (`ai_v4`) plus small, clearly-named shared infra
packages. No `v3`/legacy `ai` directories.

---

## Current reality (the facts this plan is built on)

| Package | Direct LLM calls? | Role today | Live? |
|---|---|---|---|
| `internal/ai_v4` | No — calls Python | Orchestrator, authority, the live customer path | ✅ live |
| `internal/ai/v3` | No | **Action executor** (`executor.go`, `composer.go`, `resolver.go`) — ShowProducts/Cart/Payment/Status. v4 maps onto `v3.Action` and calls it. | ✅ live (via v4) |
| `internal/ai` (v1/v2) | Yes (`client.go`, `embedding.go`) | Mixed: OpenAI client, **embeddings** (retrieval), **Telegram onboarding**, plus legacy `CustomerResponder` generation now bypassed on the customer path | ⚠️ partly live |

**Live, must-keep pieces inside `internal/ai`** (used outside the package):
- `client.go` / `client_extension.go` — `ai.AIClient`, `ai.NewAIClient`, `ai.Message`, `ai.TokenUsage`
- `embedding.go`, `memory.go`, `unified_memory.go` — `ai.MemoryService` (retrieval/embeddings, used in `ai_v4/runner`, `industry/retrieval`, `message_handler`)
- `menu_parser.go` — `ai.ParseMenuTextResult`, `ai.ParsedMenuItem` (catalog ingestion / onboarding)
- `caption_optimizer.go` — `ai.CaptionOptimizer` (social posting feature)
- `persona.go`, `business_profile.go`, `onboarder.go`, `proactive_assistant.go` — Telegram owner onboarding (`StartPersonaOnboarding`, `GetPersona`, `BusinessProfileResult`)
- `events.go`, `event_policy.go`, `intent*.go` — `ai.ExtractEvents`, `ai.DetectedIntent`, escalation event types
- `customer_output_safety.go`, `policy.go` — safety gate / policy types still referenced

**Legacy, Python-superseded (candidates for removal)** — the in-Go reasoning/generation
that the Python service replaced on the live path:
- `customer_responder.go`, `responder.go`, `sales_conversation*.go`,
  `pipeline_v2.go`, `humanizer.go`, `emotion.go`, `contextual_fallback.go`,
  `strategy_module.go`, `decision_resolver.go`, `negotiation_adapter.go`,
  `payment_instructions.go`, `response_templates.go`
- These must be verified individually — some export types still imported elsewhere
  (e.g. `ai.CustomerResponder` is still constructed in `core/services.go` and used by
  the Telegram `BotManager`). Removal requires migrating those call sites first.

---

## The plan — phased, each phase independently shippable

### Phase 0 — Guardrails before any change _(0.5 day)_
**Goal:** make regressions impossible to miss.
1. Confirm `go build ./...` and `go test ./...` are green on a clean checkout
   (note: tests need `CGO_ENABLED=1` for SQLite, per README; or set `TEST_POSTGRES_DSN`).
2. Capture a baseline of the live customer path with the existing simulation/replay
   scenarios (`internal/ai_v4/tests`, `internal/eval`). Record current outputs as the
   golden reference.
3. Add a short integration test that drives one full WhatsApp + one Telegram message
   end-to-end (mock Python responses) so both the customer path and the onboarding
   path are covered before refactoring.

**Exit:** green build/test + a golden reference for behaviour.

---

### Phase 1 — Extract shared infra out of `internal/ai` _(2–3 days)_
**Goal:** separate the must-keep infrastructure from the legacy reasoning so the
legacy can later be deleted cleanly.

Move these into new, single-purpose packages (names are suggestions):
- `internal/llm` ← `client.go`, `client_extension.go`, `model_selector.go` (the OpenAI/OpenRouter client + types)
- `internal/embeddings` ← `embedding.go` and the embedding side of `memory.go`
- `internal/memory` already exists — consolidate `ai.MemoryService` / `unified_memory.go` there
- `internal/catalog/ingest` ← `menu_parser.go`
- `internal/social/captions` ← `caption_optimizer.go`
- `internal/onboarding` already exists — move `persona.go`, `business_profile.go`,
  `onboarder.go`, `proactive_assistant.go` there

Do these **one package at a time**: move file → fix imports → `go build ./...` →
`go test ./...` → commit. Each move is mechanical and reversible.

**Risk:** low (mechanical moves). **Exit:** `internal/ai` no longer contains live
shared infra — only legacy reasoning + thin type shims.

---

### Phase 2 — Cut the Telegram path over to v4 _(3–5 days)_
**Goal:** remove the last _live_ dependency on legacy `CustomerResponder`.

Today `BotManager.Responder` is an `ai.CustomerResponder` and the Telegram customer
flow does not go through v4. Migrate it:
1. Make the Telegram inbound customer flow use the same `V4PipelineAdapter` the social
   handlers already use (`core/services.go:593`).
2. Keep owner/onboarding flows on the extracted onboarding package (Phase 1) — those
   are a separate concern from customer replies.
3. Run the Phase 0 golden reference against Telegram to confirm parity.

**Risk:** medium — this changes live behaviour for one channel. Ship behind a
per-tenant or env flag; roll out gradually; compare against golden outputs.
**Exit:** no live code path constructs `ai.CustomerResponder` for customer replies.

---

### Phase 3 — Absorb the v3 executor into v4 _(3–5 days)_
**Goal:** remove the `ai_v4 → ai/v3` dependency.

v4 reuses `v3.ActionExecutor` via `ai_v4/executor/adapter.go` (maps v4 actions onto
`v3.Action`). Bring that execution logic into v4:
1. Move `internal/ai/v3/executor.go`, `composer.go`, `resolver.go` (+ their direct
   deps: `context*.go`, `grounding.go`, types) into `internal/ai_v4/executor`.
2. Replace the `v3.Action`/`v3.ExecutionResult` mapping with native v4 types,
   deleting `adapter.go`'s translation layer.
3. Drop unused v3 reasoning files (`reasoner.go`, `micro_reasoning.go`,
   `style_analyzer.go`, etc. — most are superseded by Python).

**Risk:** medium-high — this is the catalog/cart/payment execution core. Heavy test
coverage required; lean on the simulation/regression scenarios already in the repo.
**Exit:** `internal/ai/v3` is no longer imported by anything → deletable.

---

### Phase 4 — Delete the legacy `internal/ai` reasoning + `internal/ai/v3` _(1–2 days)_
**Goal:** finish the consolidation.
1. After Phases 1–3, re-run the zero-importer check on each remaining `internal/ai`
   file and on `internal/ai/v3`.
2. Delete what has zero importers; `go build ./...` + `go test ./...` after each batch.
3. Remove the now-dead `core/llm_adapter.go`, `core/v3_adapter.go`, and the
   `V3Executor` field once nothing references them.

**Exit:** only `internal/ai_v4` + the extracted infra packages remain. ~30k+ LOC retired.

---

## Sequencing & effort

| Phase | What | Effort | Risk |
|---|---|---|---|
| 0 | Guardrails (build/test/golden) | 0.5d | — |
| 1 | Extract shared infra from `internal/ai` | 2–3d | low |
| 2 | Telegram path → v4 | 3–5d | medium |
| 3 | Absorb v3 executor into v4 | 3–5d | med-high |
| 4 | Delete legacy `internal/ai` + `v3` | 1–2d | low (after 1–3) |

**Total: ~2–3 focused weeks.** Phases 1 and 4 are safe anytime. Phases 2 and 3
change live behaviour — gate them and roll out gradually.

## Rules for every phase
- One concern per commit; `go build ./...` + `go test ./...` green before moving on.
- Never delete a package until a zero-importer check (incl. tests + `cmd/`) returns 0.
- Behaviour-changing phases (2, 3) ship behind a flag and are validated against the
  Phase 0 golden reference before the old path is removed.

## Explicitly out of scope (do separately)
- Splitting `internal/tenantapi/handler.go` (10.7k LOC) — worthwhile, unrelated.
- Consolidating the Python service's own internal versions (`api/` has its own churn).
- Rotating secrets that appeared in the deleted root log files / `.env`.
