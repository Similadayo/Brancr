# V4 SCOPE LOCK

This document defines the HARD BOUNDARIES for the V4 AI system.
Modifications outside these boundaries require a formal Architecture RFC.

## ⛔ STRICT PROHIBITIONS

### 1. No Hidden Logic
- **No business logic** inside `Pipeline`, `Router`, or `Runner`.
- Logic belongs strictly in `Decision Engine` (policy), `Executor` (action), or `Intent Engine` (classification).

### 2. No Direct LLM Calls
- **Social Renderer** is the ONLY component allowed to call LLM for text generation.
- **Intent/Decision** use LLM for classification only (JSON in/out).
- **Runner** MUST NOT call LLM directly.

### 3. No Memory Mutations Actions
- Memory is mutated **ONLY** by the `Flow Runner` at the end of a successful turn.
- Executors return `ExecutionResult`; they DO NOT write to memory directly.
- Components read from `ContextSnapshot` (Read-Only).

### 4. No New Actions Without Adapter
- All database interactions must happen via `Executor Adapter` -> `V3 Executor`.
- Do not add direct GORM calls in V4 logic flows.

## 🔒 FROZEN INTERFACES

- `AIRouter.HandleMessage`
- `FlowRunner.RunTurn`
- `SocialRenderer.Render`

## 🛑 KILL SWITCHES
The following kill switches must always be respected:
- `AI_V4_DISABLE_V4`
- `AI_V4_DISABLE_EXECUTION` 
- `AI_V4_DISABLE_LLM`
