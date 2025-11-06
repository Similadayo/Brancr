# Next Steps - Codebase Improvement Roadmap

This document outlines the recommended next steps for continuing to improve the Brancr codebase.

## ✅ Completed Improvements

1. ✅ Service layer pattern with dependency injection
2. ✅ Interface definitions in `pkg/interfaces/`
3. ✅ Middleware organization in `internal/middleware/`
4. ✅ Consistent error types in `pkg/errors/`
5. ✅ Architecture documentation

## 🎯 Recommended Next Steps (Priority Order)

### Phase 1: Testing Infrastructure (High Priority)

**Why**: The codebase currently lacks tests. With interfaces in place, we can now add comprehensive tests.

**Tasks**:

1. Add test utilities and helpers
   - Create `internal/test/` package with:
     - Mock implementations of interfaces
     - Test database setup/teardown
     - Test fixtures
2. Add unit tests for core services
   - Test `Services` initialization
   - Test AI client error handling
   - Test bot manager operations
3. Add integration tests
   - Test webhook handlers
   - Test scheduler operations
   - Test social platform integrations

**Estimated Impact**: High - Improves code reliability and prevents regressions

---

### Phase 2: Repository Pattern (Medium Priority)

**Why**: Currently database operations are scattered. A repository pattern would centralize data access.

**Tasks**:

1. Create repository interfaces in `pkg/interfaces/storage.go`
2. Implement repositories in `internal/storage/repositories/`
   - `tenant_repository.go`
   - `customer_repository.go`
   - `social_account_repository.go`
   - `scheduled_post_repository.go`
3. Refactor services to use repositories instead of direct DB access
4. Add repository tests

**Estimated Impact**: Medium - Improves testability and data access consistency

---

### Phase 3: Package Reorganization (Medium Priority)

**Why**: Large packages (bot, social, ai) have many files. Breaking them into sub-packages improves navigation.

**Option A: Conservative Approach** (Recommended)

- Keep current structure but add better documentation
- Create sub-packages only for clearly separable concerns
- Example: Move platform-specific code (`facebook.go`, `instagram.go`) to `internal/social/platforms/`

**Option B: Aggressive Approach**

- Break down all large packages:
  - `internal/bot/` → `internal/bot/{manager,handlers,flows,onboarding,media}`
  - `internal/social/` → `internal/social/{platforms,scheduler,webhooks,handlers}`
  - `internal/ai/` → `internal/ai/{client,memory,intent,responder}`

**Recommendation**: Start with Option A, migrate to Option B gradually.

**Estimated Impact**: Medium - Improves code navigation but requires careful import updates

---

### Phase 4: Event-Driven Architecture (Low Priority)

**Why**: Decouple components and enable better extensibility.

**Tasks**:

1. Create event system in `pkg/events/`
   - Event types
   - Event bus
   - Event handlers
2. Replace direct function calls with events:
   - Bot events (message received, bot started)
   - Social events (post scheduled, message received)
   - AI events (intent detected, escalation needed)
3. Add event logging and monitoring

**Estimated Impact**: Low initially, High long-term - Enables plugin architecture

---

### Phase 5: Configuration Management (Low Priority)

**Why**: Hardcoded configuration limits deployment flexibility.

**Tasks**:

1. Move to structured config (YAML/TOML)
2. Add config validation
3. Add environment-specific configs
4. Add config hot-reloading (optional)

**Estimated Impact**: Low - Current `.env` approach works well

---

### Phase 6: Observability (Medium Priority)

**Why**: Better monitoring and debugging capabilities.

**Tasks**:

1. Add structured metrics
   - Prometheus metrics
   - Custom business metrics
2. Add distributed tracing
   - OpenTelemetry integration
   - Request tracing
3. Enhance logging
   - Request correlation IDs
   - Structured log fields
   - Log levels per environment

**Estimated Impact**: Medium - Improves production debugging

---

## 🔧 Immediate Quick Wins

These can be implemented quickly for immediate benefit:

### 1. Add Health Check Endpoints

```go
// internal/core/router.go
r.Get("/health", healthHandler)
r.Get("/health/ready", readinessHandler)
r.Get("/health/live", livenessHandler)
```

### 2. Add Request ID Middleware

```go
// internal/middleware/request_id.go
// Generates unique request IDs for tracing
```

### 3. Add Rate Limiting

```go
// internal/middleware/ratelimit.go
// Protect endpoints from abuse
```

### 4. Add API Documentation

- Generate OpenAPI/Swagger docs
- Document all endpoints

### 5. Add Makefile

```makefile
# Common development tasks
.PHONY: test build run migrate
```

---

## 📊 Recommended Sequence

For a production-ready system, follow this sequence:

1. **Week 1-2**: Testing Infrastructure (Phase 1)
2. **Week 3-4**: Repository Pattern (Phase 2)
3. **Week 5**: Package Reorganization (Phase 3) - Conservative
4. **Week 6**: Observability (Phase 6) - Metrics and Logging
5. **Ongoing**: Event-Driven Architecture (Phase 4) - As needed

---

## 🚨 Important Considerations

### Before Major Refactoring

1. **Ensure tests are in place** - Refactoring without tests is risky
2. **Document current behavior** - Understand what works before changing
3. **Incremental changes** - Don't refactor everything at once
4. **Backward compatibility** - Maintain API contracts during transitions

### Code Quality Checks

- Run `go vet` and `golangci-lint` regularly
- Ensure all code compiles before committing
- Update documentation as you refactor

---

## 📝 Notes

- The current architecture is already well-organized after our improvements
- Focus on testing and observability before major structural changes
- Package reorganization can be done incrementally
- Always measure impact before and after changes

---

## 🤝 Contributing Guidelines

When adding new features:

1. Add the service to `internal/core/services.go`
2. Define interfaces in `pkg/interfaces/`
3. Add tests for new functionality
4. Update `ARCHITECTURE.md` if structure changes
5. Follow existing error handling patterns
