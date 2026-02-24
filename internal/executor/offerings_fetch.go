package executor

import (
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/storage"
)

func FetchPrimaryOfferings(tenantID uuid.UUID) ([]domain.Offering, error) {

	// In a real implementation this would call storage.GetPrimaryOfferingsByTenant(tenantID)
	// For now, checks if storage package has this function or we query DB directly.
	// Since I cannot modify storage package easily without strict instructions,
	// I will assume for this step I am querying via a repository helper OR
	// if I must implementation the prompt's `storage.Get...` I need the DB connection.
	// The prompt assumes `items, err := storage.GetPrimaryOfferingsByTenant(tenantID)`.
	// I will call it. If it doesn't exist, I need to create it in `internal/storage/products.go` or similar.
	// But `storage` usually requires DB dependency injection.
	// `fetchPrimaryOfferings` in `executor` package doesn't have DB context unless passed?
	// `ExecuteSendOfferings` has `ResolvedContext`... but does it have DB?
	// `ResolvedContext` has data, but maybe not DB connection.
	// `pipeline` Config has DB.
	// Usually Executors have dependencies injected.
	// But `Registry` initialization creates them.
	// Step 2.5: `func ExecuteSendOfferings(ctx *types.ResolvedContext)`.
	// Direct function call. How does it access DB?
	// It doesn't.
	// So `fetchPrimaryOfferings` must create a distinct connection or `storage` has a global singleton? (Unlikely).
	// OR `ResolvedContext` should have access to a repository or LazyLoader?
	// OR `ExecuteSendOfferings` is a method on a struct with dependencies?
	// The Prompt defined it as a standalone function `func Execute...`.
	// This implies `storage.GetPrimaryOfferingsByTenant` handles DB access (maybe global DB? or mistake in prompt architecture?).
	// Given strictly following prompt: I will call `storage.GetPrimaryOfferingsByTenant`.
	// And I will add that function to `internal/storage` using a global DB or similar if possible, OR
	// assume `storage` has access.
	// Wait, idiomatic Go usually passes DB.
	// `internal/storage` probably does NOT have global DB.
	// `ResolvedContext` has `Merchant`.
	// Maybe `pipeline` injects DB into context?
	// `v3.PipelineConfig` has DB.
	// I'll assume for now `storage.GetPrimaryOfferingsByTenant` is a placeholder that I need to implement
	// likely requiring DB.
	// BUT `ExecuteSendOfferings` receives `ResolvedContext`.
	// If I can't pass DB, I can't fetch.
	// Unless... `ResolvedContext` contains `CompiledKnowledge`?
	// `ctx.CompiledKnowledge` (Step 2.5 didn't show it but struct showed it).
	// `CompiledKnowledge` might ALREADY contain products!
	// `internal/ai/v3/types.go`: `CompiledKnowledge interface{}`.
	// If `CompiledKnowledge` has products, I fetch from THERE.
	// That would be "Tenant-Safe" and "Normalized" (if compiled).
	// But Prompt Step 3.2 says: `items, err := storage.GetPrimaryOfferingsByTenant(tenantID)`.
	// It implies DB fetch.
	// This creates a dependency problem if DB is not available.
	// I'll check if `storage` has a way to get DB or if I should assume valid implementation exist.
	// I'll implement `storage.GetPrimaryOfferingsByTenant` in `internal/storage/products_access.go` (new file)
	// and make it panic or error if DB is missing, or Use a GLobal/Singleton pattern if `brancr` uses one.
	// Checking `internal/storage` for `db.go` or `global`.
	// Otherwise, I'll modify `ExecuteSendOfferings` to use `CompiledKnowledge` if valid, OR
	// return a stub and note the architectural gap.
	// The prompt implies "System Responsibility" -> Fetch.

	// I'll use a STUB in `storage` for now to satisfy the call, or check if `storage` exports a DB function.

	items, err := storage.GetPrimaryOfferingsByTenant(tenantID)
	if err != nil {
		return nil, err
	}

	return normalizeOfferings(items), nil
}
