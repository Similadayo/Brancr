# AI Reliability Execution Checklist

Date: 2026-02-11
Owner: AI Platform
Scope: Phases 1-12 reliability and commerce authority hardening

## Rules
- Each phase is only "Done" when all objective checks are green.
- "Partial" means code exists but acceptance gate is incomplete.
- CI block requirement: any gate marked `CI_BLOCKING` must fail deployment on red.

## Phase Status

### Phase 1: Reliability Guardrails
Status: Partial
- [x] Stage regression lock and guardrail in runtime
  Evidence: `internal/ai_v4/runner/runner.go`
- [x] Requirements-complete advances to `payment_ready`
  Evidence: `internal/ai_v4/runner/order_flow.go`
- [x] Deterministic non-waiting fallback in empty reply path
  Evidence: `internal/ai_v4/runner/runner.go`
- [x] Timeout fallback returns actionable prompt (no passive wait language)
  Evidence: `internal/social/message_handler.go`
- [x] CI_BLOCKING tests
  Evidence: `internal/ai_v4/runner/order_flow_test.go`, `internal/social/timeout_fallback_test.go`

### Phase 2: Commerce Authority Boundary
Status: Partial
- [x] Go-side catalog/order state authority path present
  Evidence: `internal/ai_v4/runner/runner.go`
- [x] Catalog deterministic reply sourced from Go snapshot
  Evidence: `internal/ai_v4/runner/runner.go`
- [ ] Explicit policy test: reject LLM-only catalog claims under all catalog intents

### Phase 3: Advanced Catalog Retrieval Engine
Status: Partial
- [x] Multi-match behavior (exact/family/broad) implemented
  Evidence: `internal/ai_v4/runner/runner.go`
- [x] Typo-tolerant scoring using token + distance logic
  Evidence: `internal/ai_v4/catalog/resolver.go`
- [x] CI_BLOCKING tests for typo and series
  Evidence: `internal/ai_v4/catalog/resolver_test.go`, `internal/ai_v4/runner/catalog_reply_test.go`

### Phase 4: LLM-Assisted Normalization (Safe)
Status: Partial
- [x] Intent normalization pass in Go before routing
  Evidence: `internal/ai_v4/runner/runner.go`
- [ ] Confidence-gated route split (high/medium/low) as hard branch contract

### Phase 5: Deterministic Fallback Composer
Status: Partial
- [x] Catalog deterministic fallback composer available
  Evidence: `internal/ai_v4/runner/runner.go`
- [x] Weak-stall final guardrail replaces placeholder responses
  Evidence: `internal/ai_v4/runner/runner.go`
- [ ] Hard policy: never emit generic waiting messages for normal catalog inquiries (all paths)

### Phase 6: Fulfillment & Logistics Completion
Status: Partial
- [x] Requirement conditioning by delivery/pickup path
  Evidence: `internal/fulfillment/service.go`
- [x] Pickup config fields in API and models
  Evidence: `internal/tenantapi/handler.go`, `internal/storage/models.go`
- [x] Delivery and pickup progression to payment path in tests
  Evidence: `internal/ai_v4/runner/order_flow_test.go`

### Phase 7: Memory & Context Hardening
Status: Partial
- [x] DB-backed memory foundation
  Evidence: `internal/ai_v4/memory/store.go`
- [ ] Slot freshness/confidence strategy
- [ ] Context pack contract (relevant turns + slots + unresolved obligations)

### Phase 8: Test Gates (CI Blocking)
Status: Implemented
- [x] CI_BLOCKING deterministic gates added for typo/series/stage-regression/timeout-fallback
  Evidence: `.github/workflows/ci.yml`

### Phase 9: Canary + Auto Rollback
Status: Partial
- [x] Retrieval engine rollout control by canary percent
  Evidence: `internal/ai_v4/runner/retrieval_rollout.go`
- [ ] Automatic rollback action wired to trigger thresholds (currently alert-only)

### Phase 10: Production Observability
Status: Partial
- [x] Added counters for catalog match/fallback/stage regression/repeat prompt/timeout/escalation
  Evidence: `internal/observability/quality_metrics.go`
- [x] Added alert rules for these signals
  Evidence: `deploy/alerts/prometheus-alerts.yml`
- [ ] Weekly quality review loop automation tied to replay suite

### Phase 11: Data Quality Controls
Status: Partial
- [ ] Tenant catalog ingest normalization enforcement for schema stability

### Phase 12: Finalization Criteria
Status: Pending
- [ ] SLO targets green for 2-4 weeks
- [ ] Replay suite green for known failures
- [ ] No open P0/P1 regressions in stage/state/catalog handling

## CI Blocking Matrix

- Typo query correctness: `internal/ai_v4/catalog` tests
- Series query correctness: `internal/ai_v4/runner/catalog_reply_test.go`
- Stage regression prevention: `internal/ai_v4/runner/order_flow_test.go`
- Timeout fallback correctness: `internal/social/timeout_fallback_test.go`

## Rollout Controls

- `AI_V4_RETRIEVAL_ENGINE_ENABLED` default `true`
- `AI_V4_RETRIEVAL_ENGINE_CANARY_PERCENT` default `100`

## Next Completion Targets

1. Add hard policy test for "no LLM-only catalog claims".
2. Implement confidence-gated normalization routing branch contract.
3. Wire automatic rollback action to alert-triggering signal spikes.
