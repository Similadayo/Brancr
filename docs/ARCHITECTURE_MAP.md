# Brancr — Architecture Map

_A working map of how the system fits together, written from a deep read of the
backbone (2026-06-02). Companion to [AI_CONSOLIDATION_PLAN.md](./AI_CONSOLIDATION_PLAN.md)._

## What Brancr is

A multi-tenant SaaS where small businesses (Nigerian market — NGN, Paystack,
Gupshup WhatsApp) run AI-driven sales & support across WhatsApp, Instagram,
Facebook, Telegram, and TikTok. Each tenant gets an AI agent that answers catalog
questions, negotiates, takes orders, collects fulfillment details, and escalates to
a human owner when needed. Owners manage everything from a Telegram bot and a
Next.js dashboard.

## The four codebases

| Repo | Stack | Role |
|---|---|---|
| `Brancr` | Go 1.25, Chi, GORM, Postgres, Redis | Core backend — the orchestrator and system of record |
| `brancr-ai` | Python, FastAPI | The LLM "brain" — NLU + text generation (GPT-5 nano/mini) |
| `Brancr landingpage` | Next.js 14 | Marketing site + tenant dashboard + customer portal |
| `brancr-motion` | Next.js | Brand/animation experiments |

These are **separate git repos** with no umbrella repo; the project root is not
versioned.

## Backend bootstrap (`cmd/brancr` → `internal/core`)

`main.go` → `config.Load()` → `core.NewApp(cfg)` → `app.Run()`.

`core/app.go` (`NewApp`) does, in order:
1. Logger (zap, tee'd to a `LogBroadcaster` for admin streaming).
2. Postgres connect with retry + pooling; GORM `AutoMigrate`; SQL migrations
   (`migrations/`); index creation; seeds (admin, test tenant, industries, plans).
3. Idempotent data migration: legacy `BusinessMenuItem` → industry-typed
   `Product` / `MenuItem` / `Service`.
4. Optional Redis (graceful degradation if absent — features fall back to in-memory).
5. `NewServices(...)` — the full DI graph (see below).
6. Chi router + a raw `/ws` WebSocket handler mounted on a root `ServeMux`.
7. Admin Telegram bot (onboarding) + per-tenant bots, both with retry.
8. `app.Run()` starts the HTTP server and ~10 background goroutines: scheduler
   (posts), daily escalation reminders (9am UTC), trial expiry (10am), subscription
   expiry (8am), analytics refresh (2am), recurring templates (1min), WhatsApp phone
   sync (15min), metrics aggregation, job queue.

## The service graph (`core/services.go`)

`Services` is a god-struct holding every service; `NewServices` wires them with
explicit ordering (infra → fulfillment → AI → industry → email → bots → alerts →
billing → receipt → escalation → social → analytics → workflows → checkpoint).

Notable layers:
- **Infra:** job queue (Redis/asynq-style), blobstore (MinIO/S3), cache, rate
  limiter, quota service, worker pool, per-API circuit breakers (openai, gupshup,
  instagram, facebook, tiktok, cloudinary).
- **Billing:** `PlanTemplate` → `TenantPlan` (snapshot = source of truth), usage
  metering (`UsageEvent`/`UsageAggregation`), Gupshup cost passthrough, Paystack,
  enforcement, subscription.
- **Social:** Meta (FB/IG) OAuth + webhooks, Instagram Login, WhatsApp via three
  paths (Gupshup Partner API, Respond.io, Meta Direct), TikTok OAuth + webhooks,
  scheduler with AI caption optimization.
- **Bots:** `TenantBotManager` (per-tenant Telegram bots), `EscalationHandler`.

## The live customer-message path

```
Inbound (webhook: Meta / Gupshup / Respond.io / Telegram)
  → social.MessageHandler.ProcessMessage(ctx, msg)          [message_handler.go:107]
      → IntentDetector (legacy ai.DetectedIntent, still used for escalation routing)
      → Responder = V4PipelineAdapter  (core/services.go:593)
          → V4Router (authoritative_v4_only)
              → ai_v4 FlowRunner.RunTurn(req)                [runner/runner.go:2488]
      → ProductionMessageProcessor (buffering, conversation lock, reply sequencing,
        cost-control fallback, ShouldEscalateToHuman)
      → SendReply (platform-specific send)
```

### Inside `FlowRunner.RunTurn` — the heart of the AI

Go is the **authority on catalog, order, flow, and focus**; Python is the authority
on **understanding and phrasing**. Per turn:
1. Load memory (conversational + operational).
2. Build context snapshot.
3. Load tenant knowledge (the catalog) + conversation snapshot (stage, `ActiveItem`,
   `LastAIReply`, closeout state) + order snapshot (stage, items).
4. **Deterministic catalog work in Go:** browse-vs-detail mode detection, focus-item
   resolution (sticky focus, variant follow-ups like "the 256GB"), catalog kind from
   industry.
5. **Fulfillment check:** outstanding `Requirement`s (e.g. "send your delivery
   address to complete your order").
6. **Python brain** call for understanding + reply generation.
7. **Deterministic grounding/validation in Go:** order-total recalculation,
   availability/stock replies, and catalog-reply validation
   (`isCatalogReplyValid`, `replyIncludesAllCatalogItems`) — the anti-hallucination
   layer that constrains/overrides what the LLM is allowed to say.

The data model bakes in anti-hallucination rules: `TenantOffer` ("AI can ONLY
suggest offers stored here — never invent"), `SalesConversation.LastAIOffer`
("prevents backtracking — can only stay same or improve"), and explicit negotiation
counters/thresholds.

## The Python brain (`brancr-ai`)

FastAPI app (`app.py`) exposing the endpoints Go's `ai_v4/python/client.go` calls:
- `/api/v4/analyze-turn` (`turn_analyzer.py`) — the big NLU: Nigerian price parsing
  (`"1m"`, `"850k"`), objection/browse/variant detection, a `StateMachine`,
  `CorrectionMemory`, `negotiation_engine`, semantic understanding → `Decision` + `Reply`.
- `/api/v4/generate`, `/v4/emotion`, `/v4/negotiate`, `/v4/split`, plus
  order/payment/guardrail/knowledge reply renderers.

"Python suggests, Go decides" is literally true: Python returns suggestions/text;
Go's runner validates, grounds, and can override.

## The three AI generations (the central debt)

| Package | LLM calls? | Real role | Status |
|---|---|---|---|
| `internal/ai_v4` (~17k LOC) | via Python | Live orchestrator/authority | Current |
| `internal/ai/v3` (~11k LOC) | none | **Action executor** (ShowProducts/Cart/Payment/Status) — v4 runs on it | Load-bearing |
| `internal/ai` v1/v2 (~25k LOC) | yes (`client.go`→`internal/llm`, `embedding.go`) | Embeddings, Telegram onboarding, legacy `CustomerResponder` | Mixed: parts live, parts superseded |

`CustomerResponder` (v3-era) is still richly wired in `initIndustry` (OrderService,
SalesConversationManager, PaymentInstructionGen all hang off it) and serves the
Telegram owner path, but the **live social customer path uses the V4 adapter**, not
`CustomerResponder`.

## Observations worth acting on (debt, not bugs)

1. **Three entangled AI generations** — the consolidation target. See the plan.
2. **Logic duplicated across Go and Python** — Nigerian price parsing, browse
   detection, variant handling, and negotiation rules exist in *both* the Go runner
   and Python's `turn_analyzer.py`. Risk of drift; a future step is to pick one home
   per concern.
3. **Two packages named `llm`** — the new `internal/llm` (OpenAI transport) and the
   existing `internal/ai_v4/llm` (Python-service HTTP client). Different import
   paths, but the name collision invites confusion; consider renaming one
   (e.g. `ai_v4/llm` → `ai_v4/brain`).
4. **God-objects:** `tenantapi/handler.go` (~10.7k LOC, 148 funcs), the `Services`
   struct, and `runner.go` (~3.9k LOC). Splitting `tenantapi` by resource is
   low-risk, high-relief.
5. **`CustomerResponder` wiring is fragile** — a dozen services attach to it only
   inside an `if IndustryService.Retrieval != nil` block; OrderService creation is
   coupled to that branch.

## Depth caveat (honesty)

Read in full: `core/app.go`, `core/services.go`, `storage/models.go`, the LLM client,
`ai_v4/runner` (structure + `RunTurn`), Python `app.py`/`v4.py`/`turn_analyzer`
structure, and the AI-version dependency graph. **Not** read line-by-line:
`tenantapi/handler.go`, the individual webhook handlers (Gupshup/Meta/TikTok),
most of the 60-file `internal/ai` package internals, the Next.js frontends, and the
migrations. Those are understood by role and signature, not by every line.
