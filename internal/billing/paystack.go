package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrPaystackAPIError = errors.New("paystack API error")
	ErrPaymentNotFound  = errors.New("payment not found")
)

// PaystackClient handles Paystack API interactions
type PaystackClient struct {
	SecretKey  string
	PublicKey  string
	BaseURL    string
	HTTPClient *http.Client
	Logger     *zap.Logger
}

// NewPaystackClient creates a new Paystack client
func NewPaystackClient(secretKey, publicKey string, logger *zap.Logger) *PaystackClient {
	return &PaystackClient{
		SecretKey: secretKey,
		PublicKey: publicKey,
		BaseURL:   "https://api.paystack.co",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		Logger: logger,
	}
}

// CreateCustomer creates a Paystack customer
func (c *PaystackClient) CreateCustomer(ctx context.Context, email, firstName, lastName, phone string) (*PaystackCustomer, error) {
	reqBody := map[string]interface{}{
		"email":      email,
		"first_name": firstName,
		"last_name":  lastName,
	}
	if phone != "" {
		reqBody["phone"] = phone
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/customer", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.SecretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("paystack API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		c.Logger.Error("paystack API error",
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(respBody)))
		return nil, fmt.Errorf("%w: status %d", ErrPaystackAPIError, resp.StatusCode)
	}

	var result PaystackResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !result.Status {
		return nil, fmt.Errorf("%w: %s", ErrPaystackAPIError, result.Message)
	}

	customer, ok := result.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid customer data format")
	}

	return &PaystackCustomer{
		ID:           int64(customer["id"].(float64)),
		Email:        customer["email"].(string),
		FirstName:    customer["first_name"].(string),
		LastName:     customer["last_name"].(string),
		Phone:        getString(customer, "phone"),
		CustomerCode: customer["customer_code"].(string),
	}, nil
}

// InitializeTransaction creates a payment link/transaction
func (c *PaystackClient) InitializeTransaction(ctx context.Context, req InitializeTransactionRequest) (*InitializeTransactionResponse, error) {
	reqBody := map[string]interface{}{
		"email":        req.Email,
		"amount":       int(req.Amount * 100), // Convert to kobo (smallest currency unit)
		"currency":     req.Currency,
		"reference":    req.Reference,
		"callback_url": req.CallbackURL,
		"metadata": map[string]interface{}{
			"tenant_id": req.TenantID,
			"plan_name": req.PlanName,
		},
	}

	if req.CustomerCode != "" {
		reqBody["customer"] = req.CustomerCode
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/transaction/initialize", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.SecretKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("paystack API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.Logger.Error("paystack API error",
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(respBody)))
		return nil, fmt.Errorf("%w: status %d", ErrPaystackAPIError, resp.StatusCode)
	}

	var result PaystackResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !result.Status {
		return nil, fmt.Errorf("%w: %s", ErrPaystackAPIError, result.Message)
	}

	data, ok := result.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid transaction data format")
	}

	authURL, _ := data["authorization_url"].(string)
	accessCode, _ := data["access_code"].(string)
	reference, _ := data["reference"].(string)

	return &InitializeTransactionResponse{
		AuthorizationURL: authURL,
		AccessCode:       accessCode,
		Reference:        reference,
	}, nil
}

// VerifyTransaction verifies a transaction by reference
func (c *PaystackClient) VerifyTransaction(ctx context.Context, reference string) (*PaystackTransaction, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/transaction/verify/"+reference, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("paystack API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, ErrPaymentNotFound
		}
		c.Logger.Error("paystack API error",
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(respBody)))
		return nil, fmt.Errorf("%w: status %d", ErrPaystackAPIError, resp.StatusCode)
	}

	var result PaystackResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !result.Status {
		return nil, fmt.Errorf("%w: %s", ErrPaystackAPIError, result.Message)
	}

	data, ok := result.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid transaction data format")
	}

	// Parse transaction data
	amount := data["amount"].(float64) / 100 // Convert from kobo
	status := data["status"].(string)
	gatewayResponse := ""
	if gr, ok := data["gateway_response"].(string); ok {
		gatewayResponse = gr
	}

	// Parse customer if available
	var customerCode string
	if customer, ok := data["customer"].(map[string]interface{}); ok {
		if code, ok := customer["customer_code"].(string); ok {
			customerCode = code
		}
	}

	// Parse metadata
	var tenantID uuid.UUID
	var planName string
	if metadata, ok := data["metadata"].(map[string]interface{}); ok {
		if tidStr, ok := metadata["tenant_id"].(string); ok {
			tenantID, _ = uuid.Parse(tidStr)
		}
		if pn, ok := metadata["plan_name"].(string); ok {
			planName = pn
		}
	}

	return &PaystackTransaction{
		Reference:       reference,
		Amount:          amount,
		Currency:        getString(data, "currency"),
		Status:          status,
		GatewayResponse: gatewayResponse,
		CustomerCode:    customerCode,
		TenantID:        tenantID,
		PlanName:        planName,
		PaidAt:          parseTime(data, "paid_at"),
		CreatedAt:       parseTime(data, "created_at"),
	}, nil
}

// PaystackService handles billing-related Paystack operations
type PaystackService struct {
	Client         *PaystackClient
	DB             *gorm.DB
	Logger         *zap.Logger
	BillingService *BillingService
}

// NewPaystackService creates a new Paystack service
func NewPaystackService(client *PaystackClient, db *gorm.DB, logger *zap.Logger, billingService *BillingService) *PaystackService {
	return &PaystackService{
		Client:         client,
		DB:             db,
		Logger:         logger,
		BillingService: billingService,
	}
}

// CreatePaymentLink creates a Paystack payment link for a tenant's subscription
func (s *PaystackService) CreatePaymentLink(ctx context.Context, tenantID uuid.UUID, callbackURL string) (string, error) {
	// Get tenant
	var tenant storage.Tenant
	if err := s.DB.WithContext(ctx).First(&tenant, tenantID).Error; err != nil {
		return "", fmt.Errorf("tenant not found: %w", err)
	}

	// Get tenant plan
	plan, err := s.BillingService.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("tenant plan not found: %w", err)
	}

	// Get or create Paystack customer
	var customerCode string
	if plan.PaystackCustomerID != "" {
		customerCode = plan.PaystackCustomerID
	} else {
		// Create customer in Paystack
		customer, err := s.Client.CreateCustomer(ctx, tenant.Email, tenant.OwnerName, "", tenant.OwnerPhone)
		if err != nil {
			return "", fmt.Errorf("failed to create Paystack customer: %w", err)
		}
		customerCode = customer.CustomerCode

		// Update tenant plan with customer code
		if err := s.DB.WithContext(ctx).Model(plan).Update("paystack_customer_id", customerCode).Error; err != nil {
			s.Logger.Warn("failed to update Paystack customer ID",
				zap.Stringer("tenant_id", tenantID),
				zap.Error(err))
		}
	}

	// Generate unique reference
	reference := fmt.Sprintf("BRNCR-%s-%d", tenantID.String(), time.Now().Unix())

	// Initialize transaction
	initReq := InitializeTransactionRequest{
		Email:        tenant.Email,
		Amount:       plan.MonthlyPrice,
		Currency:     "NGN",
		Reference:    reference,
		CallbackURL:  callbackURL,
		CustomerCode: customerCode,
		TenantID:     tenantID,
		PlanName:     plan.PlanName,
	}

	resp, err := s.Client.InitializeTransaction(ctx, initReq)
	if err != nil {
		return "", fmt.Errorf("failed to initialize transaction: %w", err)
	}

	return resp.AuthorizationURL, nil
}

// HandleWebhook processes Paystack webhook events
func (s *PaystackService) HandleWebhook(ctx context.Context, event PaystackWebhookEvent) error {
	switch event.Event {
	case "charge.success":
		return s.handlePaymentSuccess(ctx, event.Data)
	case "charge.failed":
		return s.handlePaymentFailed(ctx, event.Data)
	case "subscription.create", "subscription.disable", "subscription.enable":
		// Future: handle subscription events
		s.Logger.Info("subscription event received (not yet implemented)",
			zap.String("event", event.Event))
		return nil
	default:
		s.Logger.Info("unhandled Paystack webhook event",
			zap.String("event", event.Event))
		return nil
	}
}

// handlePaymentSuccess processes successful payment
func (s *PaystackService) handlePaymentSuccess(ctx context.Context, data interface{}) error {
	transactionData, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid transaction data format")
	}

	reference, _ := transactionData["reference"].(string)
	if reference == "" {
		return fmt.Errorf("missing transaction reference")
	}

	// Verify transaction
	transaction, err := s.Client.VerifyTransaction(ctx, reference)
	if err != nil {
		return fmt.Errorf("failed to verify transaction: %w", err)
	}

	if transaction.Status != "success" {
		return fmt.Errorf("transaction not successful: %s", transaction.Status)
	}

	// Update tenant plan
	plan, err := s.BillingService.GetTenantPlan(ctx, transaction.TenantID)
	if err != nil {
		return fmt.Errorf("tenant plan not found: %w", err)
	}

	now := time.Now()
	updates := map[string]interface{}{
		"payment_status":        "paid",
		"last_payment_date":     now,
		"last_payment_amount":   transaction.Amount,
		"payment_failure_count": 0,
	}

	// Update billing cycle
	if plan.BillingCycleEnd != nil && now.After(*plan.BillingCycleEnd) {
		// Start new billing cycle
		updates["billing_cycle_start"] = now
		nextBilling := now.AddDate(0, 1, 0)
		updates["billing_cycle_end"] = nextBilling
		updates["next_billing_date"] = nextBilling
	}

	// Activate plan if it was paused
	if plan.BillingStatus == "paused" {
		status := "active"
		if plan.PlanName == "Trial" {
			status = "trial"
		}
		updates["billing_status"] = status
	}

	if err := s.DB.WithContext(ctx).Model(plan).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update tenant plan: %w", err)
	}

	s.Logger.Info("payment processed successfully",
		zap.Stringer("tenant_id", transaction.TenantID),
		zap.String("reference", reference),
		zap.Float64("amount", transaction.Amount))

	return nil
}

// handlePaymentFailed processes failed payment
func (s *PaystackService) handlePaymentFailed(ctx context.Context, data interface{}) error {
	transactionData, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid transaction data format")
	}

	reference, _ := transactionData["reference"].(string)
	metadata, _ := transactionData["metadata"].(map[string]interface{})

	var tenantID uuid.UUID
	if tidStr, ok := metadata["tenant_id"].(string); ok {
		tenantID, _ = uuid.Parse(tidStr)
	}
	if pn, ok := metadata["plan_name"].(string); ok {
		_ = pn
	}

	if tenantID == uuid.Nil {
		return fmt.Errorf("missing tenant_id in transaction metadata")
	}

	// Get tenant plan
	plan, err := s.BillingService.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("tenant plan not found: %w", err)
	}

	// Increment failure count
	failureCount := plan.PaymentFailureCount + 1
	updates := map[string]interface{}{
		"payment_status":        "failed",
		"payment_failure_count": failureCount,
	}

	// After 3 failures, pause the plan
	if failureCount >= 3 {
		updates["billing_status"] = "paused"
	}

	if err := s.DB.WithContext(ctx).Model(plan).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update tenant plan: %w", err)
	}

	s.Logger.Warn("payment failed",
		zap.Stringer("tenant_id", tenantID),
		zap.String("reference", reference),
		zap.Int("failure_count", failureCount))

	return nil
}

// Helper types and functions

type PaystackCustomer struct {
	ID           int64
	Email        string
	FirstName    string
	LastName     string
	Phone        string
	CustomerCode string
}

type InitializeTransactionRequest struct {
	Email        string
	Amount       float64
	Currency     string
	Reference    string
	CallbackURL  string
	CustomerCode string
	TenantID     uuid.UUID
	PlanName     string
}

type InitializeTransactionResponse struct {
	AuthorizationURL string
	AccessCode       string
	Reference        string
}

type PaystackTransaction struct {
	Reference       string
	Amount          float64
	Currency        string
	Status          string
	GatewayResponse string
	CustomerCode    string
	TenantID        uuid.UUID
	PlanName        string
	PaidAt          *time.Time
	CreatedAt       *time.Time
}

type PaystackWebhookEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

type PaystackResponse struct {
	Status  bool        `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func parseTime(m map[string]interface{}, key string) *time.Time {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			if t, err := time.Parse(time.RFC3339, str); err == nil {
				return &t
			}
		}
	}
	return nil
}
