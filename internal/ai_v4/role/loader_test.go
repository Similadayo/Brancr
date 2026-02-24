package role

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestLoader_ApplyIndustryDefaults(t *testing.T) {
	loader := NewLoader(nil, zap.NewNop())
	tenantID := uuid.New()

	tests := []struct {
		industry      string
		wantOrders    bool
		wantNegotiate bool
	}{
		{"Restaurant", true, false},
		{"Fast Food", true, false},
		{"Fashion Retail", true, false},
		{"Legal Consultant", false, true},
		{"Plumbing Service", false, true},
		{"Unknown", false, false},
	}

	for _, tt := range tests {
		contract := &RoleContract{
			TenantID: tenantID,
			Industry: tt.industry,
		}
		loader.applyIndustryDefaults(contract)

		assert.Equal(t, tt.wantOrders, contract.CanTakeOrders, "Industry: %s", tt.industry)
		assert.Equal(t, tt.wantNegotiate, contract.CanNegotiate, "Industry: %s", tt.industry)
	}
}

func TestLoader_Load_SafeReturn(t *testing.T) {
	loader := NewLoader(nil, zap.NewNop())
	ctx := context.Background()
	tenantID := uuid.New()

	contract, err := loader.Load(ctx, tenantID)

	assert.NoError(t, err)
	assert.NotNil(t, contract)
	assert.Equal(t, tenantID, contract.TenantID)
	// Default unknown behaves safely (no orders)
	assert.False(t, contract.CanTakeOrders)
}
