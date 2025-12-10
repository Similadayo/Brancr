package tenantapi

import (
	"testing"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

// TestE2E_SignupFlow tests basic signup and tenant creation
func TestE2E_SignupFlow(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	defer logger.Sync()

	// Simulate signup by creating tenant directly
	tenant := storage.Tenant{
		Name:               "Test Business",
		Email:              "test@example.com",
		PasswordHash:       "hashed_password",
		OnboardingStep:     "business_profile",
		OnboardingComplete: false,
		Status:             "active",
	}

	result := db.Create(&tenant)
	if result.Error != nil {
		t.Fatalf("failed to create tenant: %v", result.Error)
	}

	// Verify tenant was created
	var createdTenant storage.Tenant
	db.First(&createdTenant, tenant.ID)

	if createdTenant.ID == 0 {
		t.Errorf("tenant not found after creation")
	}

	if createdTenant.Email != "test@example.com" {
		t.Errorf("tenant email mismatch: expected test@example.com, got %s", createdTenant.Email)
	}

	if createdTenant.OnboardingStep != "business_profile" {
		t.Errorf("expected onboarding_step=business_profile, got %s", createdTenant.OnboardingStep)
	}
}

// TestE2E_OnboardingFlow tests onboarding completion
func TestE2E_OnboardingFlow(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	defer logger.Sync()

	// Create tenant
	tenant := storage.Tenant{
		Name:               "Test Business",
		Email:              "test@example.com",
		OnboardingStep:     "business_profile",
		OnboardingComplete: false,
		Status:             "active",
	}
	db.Create(&tenant)

	// Create business profile
	bizProfile := storage.BusinessProfile{
		TenantID:    tenant.ID,
		Name:        "Test Business",
		Niche:       "E-commerce",
		Description: "Online store selling electronics",
		Tone:        "friendly",
		Location:    "San Francisco",
		Website:     "https://test.com",
	}
	db.Create(&bizProfile)

	// Create persona
	persona := storage.Persona{
		TenantID:   tenant.ID,
		BotName:    "Luna",
		Tone:       "friendly and professional",
		Language:   "en",
		Humor:      true,
		StyleNotes: "Be helpful and concise",
	}
	db.Create(&persona)

	// Mark onboarding as complete
	db.Model(&tenant).Updates(map[string]interface{}{
		"onboarding_step":     "complete",
		"onboarding_complete": true,
	})

	// Verify
	var updatedTenant storage.Tenant
	db.First(&updatedTenant, tenant.ID)

	if !updatedTenant.OnboardingComplete {
		t.Errorf("onboarding should be marked complete")
	}

	if updatedTenant.OnboardingStep != "complete" {
		t.Errorf("expected onboarding_step=complete, got %s", updatedTenant.OnboardingStep)
	}

	// Verify business profile exists
	var profile storage.BusinessProfile
	db.Where("tenant_id = ?", tenant.ID).First(&profile)
	if profile.ID == 0 {
		t.Errorf("business profile not found")
	}

	// Verify persona exists
	var personaRecord storage.Persona
	db.Where("tenant_id = ?", tenant.ID).First(&personaRecord)
	if personaRecord.TenantID == 0 {
		t.Errorf("persona not found")
	}
}

// TestE2E_SchedulePost tests post creation and scheduling
func TestE2E_SchedulePost(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	defer logger.Sync()

	// Create tenant
	tenant := storage.Tenant{
		Name:               "Test Business",
		Email:              "test@example.com",
		OnboardingComplete: true,
		Status:             "active",
	}
	db.Create(&tenant)

	// Create scheduled post
	scheduledTime := time.Now().Add(24 * time.Hour)
	post := storage.ScheduledPost{
		TenantID:       tenant.ID,
		Name:           "Test Post",
		Caption:        "Check out our new products!",
		MediaAssetIDs:  "[1,2,3]",
		Platforms:      `["instagram"]`,
		ScheduledAt:    &scheduledTime,
		Status:         "scheduled",
		EnhanceCaption: false,
	}
	db.Create(&post)

	// Verify post was created
	var createdPost storage.ScheduledPost
	db.Where("tenant_id = ? AND id = ?", tenant.ID, post.ID).First(&createdPost)

	if createdPost.ID == 0 {
		t.Errorf("scheduled post not found")
	}

	if createdPost.Status != "scheduled" {
		t.Errorf("expected status=scheduled, got %s", createdPost.Status)
	}

	if createdPost.Name != "Test Post" {
		t.Errorf("post name mismatch")
	}

	// Verify tenant isolation (post should not be visible to other tenants)
	otherTenant := storage.Tenant{
		Name:  "Other Business",
		Email: "other@example.com",
	}
	db.Create(&otherTenant)

	var otherTenantPost storage.ScheduledPost
	db.Where("tenant_id = ?", otherTenant.ID).First(&otherTenantPost)

	if otherTenantPost.ID != 0 {
		t.Errorf("post should not be visible to other tenant (tenant isolation violation)")
	}
}

// TestE2E_CustomerInteraction tests customer message and conversation flow
func TestE2E_CustomerInteraction(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	defer logger.Sync()

	// Create tenant
	tenant := storage.Tenant{
		Name:  "Test Business",
		Email: "test@example.com",
	}
	db.Create(&tenant)

	// Create customer
	customer := storage.Customer{
		TenantID:      tenant.ID,
		Channel:       "whatsapp",
		PlatformID:    "1234567890",
		DisplayName:   "John Doe",
		LastMessageAt: time.Now(),
	}
	db.Create(&customer)

	// Create conversation
	conversation := storage.Conversation{
		TenantID:         tenant.ID,
		CustomerID:       customer.ID,
		Platform:         "whatsapp",
		PlatformThreadID: "conv-123",
		Status:           "active",
		LastMessageAt:    time.Now(),
	}
	db.Create(&conversation)

	// Create interaction (message)
	interaction := storage.Interaction{
		TenantID:          tenant.ID,
		CustomerID:        customer.ID,
		ConversationID:    conversation.ID,
		Platform:          "whatsapp",
		Direction:         "incoming",
		MessageType:       "text",
		Content:           "Do you have this in stock?",
		PlatformMessageID: "msg-123",
		DetectedIntent:    "inquiry",
		DetectedTone:      "neutral",
		Confidence:        0.85,
		ResponseType:      "auto_reply",
		ResponseStatus:    "pending",
		SuggestedReply:    "Yes, we have this in stock!",
	}
	db.Create(&interaction)

	// Verify customer was created
	var storedCustomer storage.Customer
	db.Where("tenant_id = ? AND platform_id = ?", tenant.ID, "1234567890").First(&storedCustomer)
	if storedCustomer.ID == 0 {
		t.Errorf("customer not found")
	}

	// Verify conversation was created
	var storedConversation storage.Conversation
	db.Where("tenant_id = ? AND id = ?", tenant.ID, conversation.ID).First(&storedConversation)
	if storedConversation.ID == 0 {
		t.Errorf("conversation not found")
	}

	// Verify interaction was created
	var storedInteraction storage.Interaction
	db.Where("tenant_id = ? AND conversation_id = ?", tenant.ID, conversation.ID).First(&storedInteraction)
	if storedInteraction.ID == 0 {
		t.Errorf("interaction not found")
	}

	if storedInteraction.DetectedIntent != "inquiry" {
		t.Errorf("expected intent=inquiry, got %s", storedInteraction.DetectedIntent)
	}

	if storedInteraction.Confidence != 0.85 {
		t.Errorf("expected confidence=0.85, got %f", storedInteraction.Confidence)
	}

	// Verify tenant isolation for interactions
	otherTenant := storage.Tenant{
		Name:  "Other Business",
		Email: "other@example.com",
	}
	db.Create(&otherTenant)

	var otherTenantInteraction storage.Interaction
	db.Where("tenant_id = ?", otherTenant.ID).First(&otherTenantInteraction)

	if otherTenantInteraction.ID != 0 {
		t.Errorf("interaction should not be visible to other tenant")
	}
}

// TestMultiTenantIsolation verifies that data is properly isolated per tenant
func TestMultiTenantIsolation(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	defer logger.Sync()

	// Create two tenants
	tenant1 := storage.Tenant{Name: "Business 1", Email: "business1@example.com"}
	tenant2 := storage.Tenant{Name: "Business 2", Email: "business2@example.com"}

	db.Create(&tenant1)
	db.Create(&tenant2)

	// Create customers for each tenant
	customer1 := storage.Customer{TenantID: tenant1.ID, Channel: "whatsapp", PlatformID: "cust1", DisplayName: "Customer 1"}
	customer2 := storage.Customer{TenantID: tenant2.ID, Channel: "whatsapp", PlatformID: "cust2", DisplayName: "Customer 2"}

	db.Create(&customer1)
	db.Create(&customer2)

	// Verify isolation: tenant1 should only see customer1
	var tenant1Customers []storage.Customer
	db.Where("tenant_id = ?", tenant1.ID).Find(&tenant1Customers)

	if len(tenant1Customers) != 1 {
		t.Errorf("tenant1 should have 1 customer, got %d", len(tenant1Customers))
	}

	if tenant1Customers[0].DisplayName != "Customer 1" {
		t.Errorf("tenant1 should only see its own customer")
	}

	// Verify isolation: tenant2 should only see customer2
	var tenant2Customers []storage.Customer
	db.Where("tenant_id = ?", tenant2.ID).Find(&tenant2Customers)

	if len(tenant2Customers) != 1 {
		t.Errorf("tenant2 should have 1 customer, got %d", len(tenant2Customers))
	}

	if tenant2Customers[0].DisplayName != "Customer 2" {
		t.Errorf("tenant2 should only see its own customer")
	}
}

// TestMemoryItemStorage tests conversation memory persistence
func TestMemoryItemStorage(t *testing.T) {
	db := testutil.TestDB(t)
	logger := testutil.TestLogger(t)
	defer logger.Sync()

	tenant := storage.Tenant{Name: "Test Business", Email: "test@example.com"}
	db.Create(&tenant)

	customer := storage.Customer{TenantID: tenant.ID, Channel: "whatsapp", PlatformID: "cust1", DisplayName: "Customer"}
	db.Create(&customer)

	// Create memory items
	memories := []storage.MemoryItem{
		{
			TenantID:   tenant.ID,
			CustomerID: customer.ID,
			Text:       "Customer prefers morning shipping",
			Category:   "preference",
			CreatedAt:  time.Now(),
		},
		{
			TenantID:   tenant.ID,
			CustomerID: customer.ID,
			Text:       "Previously purchased Item A and Item B",
			Category:   "purchase_history",
			CreatedAt:  time.Now(),
		},
	}

	for _, mem := range memories {
		db.Create(&mem)
	}

	// Verify memories were stored
	var storedMemories []storage.MemoryItem
	db.Where("tenant_id = ? AND customer_id = ?", tenant.ID, customer.ID).Find(&storedMemories)

	if len(storedMemories) != 2 {
		t.Errorf("expected 2 memories, got %d", len(storedMemories))
	}

	// Verify content
	foundPref := false
	foundHistory := false
	for _, mem := range storedMemories {
		if mem.Category == "preference" && mem.Text == "Customer prefers morning shipping" {
			foundPref = true
		}
		if mem.Category == "purchase_history" {
			foundHistory = true
		}
	}

	if !foundPref {
		t.Errorf("preference memory not found")
	}
	if !foundHistory {
		t.Errorf("purchase history memory not found")
	}
}
