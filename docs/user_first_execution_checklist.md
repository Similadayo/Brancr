# User-First AI Execution Checklist

Last updated: 2026-02-18
Owner: AI V4 migration workstream

## How to use this file
- Mark each item `[x]` only after code is merged and deployed.
- Add a short note with date and evidence link/log line.
- Keep this as the single source of truth for user-first migration progress.

## 1) Authority and memory ownership
- [x] Default bypass legacy pre-intent path in Go (`AI_V4_LEGACY_PRE_INTENT` opt-in only).
- [x] Enable Python text passthrough in send path (`AI_V4_PYTHON_TEXT_PASSTHROUGH` default on).
- [ ] Remove remaining Go conversational decision branches from `runner.go` so Python owns reply intent/state.
- [ ] Move focus-selection authority fully to Python contract; Go should only enforce safety constraints.
- [ ] Add API contract field: `focus_change_reason` from Python whenever focus changes.

## 2) SKU lock and context safety
- [x] Move SKU lock wording rendering to Python (`/api/v4/render-guardrail-reply`).
- [x] Gate SKU-lock prompts to mutation intents only (not generic/spec questions).
- [x] Prevent duplicate SKU-lock prompts in the same turn.
- [x] Block variant-only target drift (`512GB`) from overriding active focus in Go.
- [x] Classify best-price questions as `price_request` (not negotiation offer) unless explicit offer exists.
- [x] Add replay test: repeated `best price for 512GB` after iPhone context must not drift to Samsung.

## 3) State model upgrades (user-first)
- [ ] Add `frustration_level` to persisted conversation state.
- [ ] Add `pending_clarification` tracking to conversation state.
- [ ] Add `price_audit_trail` tracking to conversation/order state.
- [ ] Add `repeat_question_count` tracking and threshold actions.
- [ ] Add explicit `escalation_lock_state` with lock/unlock rules.

## 4) Guardrails and consistency
- [ ] Add contradiction guard before send (reply vs prior commitments/offers).
- [ ] Add escalation hard lock: no pricing/acceptance actions while escalated.
- [ ] Add explicit acceptance gate: no order acceptance without confirmed acceptance intent.
- [ ] Add low-confidence clarification gate for ambiguous price mentions.

## 5) Observability and debugging
- [x] Add `authority_trace` log object on every turn with fields: `who_set_reply`, `who_set_focus`, `who_blocked_action`, `guard_triggered`.
- [ ] Add metric: `ai.focus_drift_rate`.
- [ ] Add metric: `ai.escalation_contradiction_rate`.
- [ ] Add metric: `ai.false_switch_prompt_rate`.

## 6) Deployment and release safety
- [ ] Add deploy gate: prevent `Brancr`/`brancr-ai` version skew on production.
- [ ] Add release checklist requiring both services deployed for AI behavior changes.
- [ ] Add smoke test script for top replay scenarios post-deploy.

## 7) Replay suite expansion
- [ ] Add replay: question-with-number should not become offer.
- [ ] Add replay: correction turn must change next-turn behavior.
- [ ] Add replay: escalation state blocks negotiation responses.
- [ ] Add replay: general chat handled naturally with gentle redirect.
- [x] Add replay: ambiguous variant follows active focus unless explicit model switch.

## Execution notes
- 2026-02-18: Verified from production logs that drift still occurred on ambiguous `512GB` query because Python returned Samsung target explicitly and Go followed it.
- 2026-02-18: Added local fixes for mutation gating, best-price guard, variant-only drift blocking, and duplicate SKU-lock suppression.
- 2026-02-18: Added per-turn `authority_trace` logging in `internal/ai_v4/runner/runner.go` with reply/focus/block/guard attribution.
- 2026-02-18: Added regression test `TestConversationSimulation_AmbiguousVariantBestPrice_StaysOnFocusedModel` in `internal/ai_v4/runner/conversation_simulation_test.go`.
- 2026-02-18: Remaining priority is full authority centralization (Python memory/decision contract + Go thin execution).
