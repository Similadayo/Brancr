package tenantapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/similadayo/brancr/internal/ai"
)

// AIParser uses the AIClientInterface to extract structured items from raw text
type AIParser struct {
	AIClient ai.AIClientInterface
	Timeout  time.Duration
}

func NewAIParser(aiClient ai.AIClientInterface) *AIParser {
	return &AIParser{AIClient: aiClient, Timeout: 2 * time.Minute}
}

const (
	MaxBlocks = 50
	BatchSize = 5
)

// Parse calls the AI client to extract JSON items, using deterministic block segmentation for products.
func (a *AIParser) Parse(raw string, industry string) ([]ParsedItem, error) {
	// 2-PASS ARCHITECTURE:
	// For products/menu, we deterministicly split blocks by blank lines first.
	// This prevents the LLM from "over-splitting" single products into multiple items.
	if industry == "products" || industry == "menu" || industry == "food" || industry == "" {
		blocks := splitIntoBlocks(raw)

		// 1. HARD CAP: Protect system from massive payloads
		if len(blocks) > MaxBlocks {
			return nil, fmt.Errorf("too many items (%d). Max allowed is %d. Please split your file.", len(blocks), MaxBlocks)
		}

		return a.parseBatchesParallel(blocks, industry)
	}

	// LEGACY SERVICE LOGIC (Single pass)
	return a.parseSinglePass(raw, industry)
}

// ParseWithImages handles parsing from both text and images using vision AI
// If imageURLs is empty, falls back to text-only parsing
func (a *AIParser) ParseWithImages(raw string, imageURLs []string, industry string) ([]ParsedItem, error) {
	// Validation
	maxImages := 5
	if len(imageURLs) > maxImages {
		return nil, fmt.Errorf("too many images (%d). Max allowed is %d", len(imageURLs), maxImages)
	}

	// If no images provided, use regular text parsing
	if len(imageURLs) == 0 {
		return a.Parse(raw, industry)
	}

	// Build vision-aware prompt
	ctx, cancel := context.WithTimeout(context.Background(), a.Timeout)
	defer cancel()

	systemPrompt := "You are a strict Structured Data Extractor. Output only valid JSON arrays. No markdown, no explanations."
	userPrompt := buildVisionPrompt(raw, industry, len(imageURLs))

	// Use vision AI
	resp, err := a.AIClient.GenerateWithVision(ctx, systemPrompt, userPrompt, imageURLs)
	if err != nil {
		// Fallback: if vision fails, try text-only parsing
		fmt.Printf("Vision parsing failed: %v. Falling back to text-only.\n", err)
		if raw != "" {
			return a.Parse(raw, industry)
		}
		return nil, fmt.Errorf("vision parsing failed and no text provided: %w", err)
	}

	return unmarshalItems(resp)
}

func (a *AIParser) parseSinglePass(raw string, industry string) ([]ParsedItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.Timeout)
	defer cancel()

	prompt := buildServicePrompt(raw)
	messages := []ai.Message{
		{Role: "system", Content: "You are a strict Structured Data Extractor. Output only valid JSON arrays. No markdown, no explanations."},
		{Role: "user", Content: prompt},
	}

	resp, err := a.AIClient.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("ai extract failed: %w", err)
	}

	return unmarshalItems(resp)
}

func (a *AIParser) parseBatchesParallel(blocks []string, industry string) ([]ParsedItem, error) {
	var items []ParsedItem
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Chunk blocks into batches to reduce API calls
	batches := chunkBlocks(blocks, BatchSize)

	// Concurrency Limit (Safe for Standard Tier)
	sem := make(chan struct{}, 3)

	// DYNAMIC TIMEOUT:
	// Batch processing takes longer. Est 60s per batch to be safe.
	// We sum them up (assume worst case sequential) to ensure we don't timeout later batches.
	neededTime := time.Duration(len(batches)) * 60 * time.Second
	if neededTime < 5*time.Minute {
		neededTime = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(context.Background(), neededTime)
	defer cancel()

	for i, batch := range batches {
		wg.Add(1)

		// Stagger requests slightly
		time.Sleep(150 * time.Millisecond)

		go func(b []string, batchIdx int) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			// Extract Batch
			batchItems, err := a.extractBatch(ctx, b, industry)

			// GRACEFUL DEGRADATION:
			// If AI failed completely for this batch, fallback to raw blocks.
			if err != nil {
				fmt.Printf("Batch %d failed: %v. Using fallback.\n", batchIdx, err)
				batchItems = makeFallbackItems(b)
			}

			mu.Lock()
			items = append(items, batchItems...)
			mu.Unlock()
		}(batch, i)
	}

	wg.Wait()

	// Post-process all items
	for i := range items {
		normalizeItem(&items[i])
	}

	return items, nil
}

// rawParsedItem matches the JSON output from AI (all strings for robustness)
type rawParsedItem struct {
	Name         string              `json:"name"`
	Price        string              `json:"price"`     // "1,200,000" or "1.2m"
	MinPrice     string              `json:"min_price"` // "1,100,000"
	MaxPrice     string              `json:"max_price"` // "1,200,000"
	Currency     string              `json:"currency"`
	Description  string              `json:"description"`
	Deliverables []string            `json:"deliverables"`
	Duration     string              `json:"duration"`
	Variants     map[string][]string `json:"variants"`
	Flags        map[string]bool     `json:"flags"`
}

func (a *AIParser) extractBatch(ctx context.Context, blocks []string, industry string) ([]ParsedItem, error) {
	prompt := buildBatchPrompt(blocks, industry)
	messages := []ai.Message{
		{Role: "system", Content: "You are a strict Data Extractor. Output EXACTLY ONE JSON Array. No markdown."},
		{Role: "user", Content: prompt},
	}

	// Use parent context (which is already dynamically sized)
	// Do NOT set a short sub-timeout here, as batch generation for 5 items can take >45s.

	resp, err := a.AIClient.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	cleanResp := strings.TrimPrefix(resp, "```json")
	cleanResp = strings.TrimPrefix(cleanResp, "```")
	cleanResp = strings.TrimSuffix(cleanResp, "```")

	var rawItems []rawParsedItem
	// Try parsing as array of RAW items (strings)
	if err := json.Unmarshal([]byte(cleanResp), &rawItems); err != nil {
		// Sometimes LLM returns single object if batch size is 1
		var single rawParsedItem
		if err2 := json.Unmarshal([]byte(cleanResp), &single); err2 == nil {
			rawItems = []rawParsedItem{single}
		} else {
			return nil, err
		}
	}

	// Convert Raw -> Parsed (Robust Type Conversion)
	var items []ParsedItem
	for _, raw := range rawItems {
		items = append(items, convertRawToParsed(raw))
	}

	return items, nil
}

func convertRawToParsed(raw rawParsedItem) ParsedItem {
	item := ParsedItem{
		Name:         raw.Name,
		Currency:     raw.Currency,
		Description:  raw.Description,
		Deliverables: raw.Deliverables,
		Duration:     raw.Duration,
		Variants:     raw.Variants,
		Flags:        raw.Flags,
	}

	// Parse Price Strings (handles commas, symbols, whitespace)
	if raw.Price != "" {
		if val, curr := parsePrice(raw.Price); val != nil {
			item.Price = val
			if item.Currency == "" {
				item.Currency = curr
			}
		}
	}
	if raw.MinPrice != "" {
		if val, curr := parsePrice(raw.MinPrice); val != nil {
			item.MinPrice = val
			if item.Currency == "" {
				item.Currency = curr
			}
		}
	}
	if raw.MaxPrice != "" {
		if val, curr := parsePrice(raw.MaxPrice); val != nil {
			item.MaxPrice = val
			if item.Currency == "" {
				item.Currency = curr
			}
		}
	}

	// POLICY: If price is unknown but we have a max price (negotiation ceiling), use that as the anchor price.
	if item.Price == nil && item.MaxPrice != nil {
		item.Price = item.MaxPrice
		item.Flags["price_from_max"] = true
	}

	return item
}

func chunkBlocks(blocks []string, size int) [][]string {
	var chunks [][]string
	for i := 0; i < len(blocks); i += size {
		end := i + size
		if end > len(blocks) {
			end = len(blocks)
		}
		chunks = append(chunks, blocks[i:end])
	}
	return chunks
}

func makeFallbackItems(blocks []string) []ParsedItem {
	var items []ParsedItem
	for _, b := range blocks {
		lines := strings.Split(b, "\n")
		name := "Unparsed Item"
		if len(lines) > 0 {
			name = lines[0] // Best guess
		}
		items = append(items, ParsedItem{
			Name:        name,
			Description: b,
			Flags:       map[string]bool{"ai_failed": true},
			Confidence:  0.0,
		})
	}
	return items
}

func splitIntoBlocks(raw string) []string {
	lines := strings.Split(raw, "\n")

	var blocks []string
	var current []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			if len(current) > 0 {
				blocks = append(blocks, strings.Join(current, "\n"))
				current = nil
			}
			continue
		}

		current = append(current, line)
	}

	if len(current) > 0 {
		blocks = append(blocks, strings.Join(current, "\n"))
	}

	return blocks
}

func unmarshalItems(resp string) ([]ParsedItem, error) {
	var items []ParsedItem
	if err := json.Unmarshal([]byte(resp), &items); err != nil {
		cleanResp := strings.TrimPrefix(resp, "```json")
		cleanResp = strings.TrimPrefix(cleanResp, "```")
		cleanResp = strings.TrimSuffix(cleanResp, "```")
		if err2 := json.Unmarshal([]byte(cleanResp), &items); err2 != nil {
			return nil, fmt.Errorf("ai response unmarshal: %w", err)
		}
	}
	// Post-process
	for i := range items {
		normalizeItem(&items[i])
	}
	return items, nil
}

// buildBatchPrompt asks for objects from blocks, allowing merging for variants/headers
func buildBatchPrompt(blocks []string, industry string) string {
	// Enhanced schema with STRING prices for robust parsing
	productSchema := `{
  "name": "string (REQUIRED)",
  "price": "string (e.g. '1500000', '50000') - MUST be quoted string, not a number",
  "min_price": "string or null - MUST be quoted string",
  "max_price": "string or null - MUST be quoted string",
  "currency": "string (NGN, USD, GBP, EUR)",
  "description": "string (multi-line allowed) - DO NOT include price here",
  "deliverables": ["array of strings - specific items included"],
  "variants": {"string": ["array", "of", "strings"]}, 
  "duration": "string (e.g. '2 weeks', '1 month', '30 minutes')",
  "billing_period": "string (e.g. 'one-time', 'monthly', 'yearly', 'hourly')",
  "category": "string",
  "type": "string",
  "flags": {"negotiable": "boolean", "has_variants": "boolean"}
}`

	var blockText strings.Builder
	for i, b := range blocks {
		blockText.WriteString(fmt.Sprintf("-- BLOCK %d --\n%s\n\n", i+1, b))
	}

	// Industry-specific examples with STRING prices
	examples := ""
	switch industry {
	case "products", "menu", "food":
		examples = `
EXAMPLES:
Input: "Jollof Rice\nServed with chicken and plantain\n₦3,500\nAvailable: Lunch & Dinner"
Output: [{"name": "Jollof Rice", "description": "Served with chicken and plantain. Available: Lunch & Dinner", "price": "3500", "currency": "NGN", "type": "menu_item"}]

Input: "iPhone 15 Pro Max\n256GB Storage\n₦1,500,000 - ₦1,650,000\nBrand new, sealed box\nColors: Natural Titanium, Blue Titanium"
Output: [{"name": "iPhone 15 Pro Max", "description": "Brand new, sealed box", "min_price": "1500000", "max_price": "1650000", "price": null, "currency": "NGN", "type": "product", "variants": {"storage": ["256GB"], "color": ["Natural Titanium", "Blue Titanium"]}, "flags": {"has_variants": true, "negotiable": true}}]

Input (Header + Variants):
-- BLOCK 1 --
iPhone 13 Pro Max
Battery Health 89%, very clean, no scratches
UK used, comes with original box and charger

-- BLOCK 2 --
256GB / Silver, Gold, Graphite
Price: ₦1,150,000 – ₦1,250,000

-- BLOCK 3 --
512GB / Silver, Gold, Graphite
Price: ₦1,250,000 – ₦1,350,000

Output: [
  {"name": "iPhone 13 Pro Max (256GB)", "description": "Battery Health 89%, very clean, no scratches. UK used, comes with original box and charger. Silver, Gold, Graphite", "min_price": "1150000", "max_price": "1250000", "currency": "NGN", "type": "product"},
  {"name": "iPhone 13 Pro Max (512GB)", "description": "Battery Health 89%, very clean, no scratches. UK used, comes with original box and charger. Silver, Gold, Graphite", "min_price": "1250000", "max_price": "1350000", "currency": "NGN", "type": "product"}
]
`
	default: // services
		examples = `
EXAMPLES:
Input: "Website Design & Development\n₦150,000 - ₦250,000\nIncludes: Homepage, 5 pages, mobile responsive, 1 month support\nDelivery: 2-3 weeks"
Output: [{"name": "Website Design & Development", "min_price": "150000", "max_price": "250000", "price": null, "currency": "NGN", "deliverables": ["Homepage", "5 pages", "mobile responsive", "1 month support"], "duration": "2-3 weeks", "type": "service"}]

Input: "Social Media Management\n₦50,000/month\nIncludes: Daily posts, content creation, engagement monitoring\nRequires: Brand guidelines"
Output: [{"name": "Social Media Management", "price": "50000", "currency": "NGN", "billing_period": "monthly", "deliverables": ["Daily posts", "content creation", "engagement monitoring"], "description": "Requires: Brand guidelines", "type": "service"}]
`
	}

	return fmt.Sprintf(`You are extracting %s catalog items from %d text blocks.

CRITICAL RULES FOR MERGING & INHERITANCE:
1. **Context Awareness**: The input is split into blocks by blank lines. Sometimes a product is split across multiple blocks (e.g. Header + Variants).
2. **Header Inheritance**: If Block N describes a product (Name + Description) but has NO price, and Block N+1 defines a specific version/variant with a price, consider Block N a "Header".
3. **Action**: If you see a Header followed by Variants:
   - Extract a SEPARATE item for EACH Variant block.
   - COPY the Name and Description from the Header to each Variant item.
   - Append the variant details (e.g. "256GB") to the Name.
   - Do NOT output the Header as a separate item if it was used to populate the variants.
4. **Independent Items**: If blocks are unrelated, extract them as separate independent items.

STANDARD RULES:
1. Multi-line descriptions are COMMON - combine all descriptive lines.
2. If no price found and it's NOT a header for subsequent items, set price=null and flag {"missing_price": true}.
3. Extract deliverables as an array if mentioned.
4. For price ranges (e.g. "₦1,000 - ₦5,000"), set min_price and max_price.
5. All price fields MUST be quoted strings.

OUTPUT FORMAT:
- MUST be a valid JSON array of objects.
- The number of output objects does NOT need to match the number of input blocks (due to merging/splitting).
- Use the schema below.

SCHEMA:
%s

EXAMPLES:
%s

INPUT BLOCKS:
%s`, industry, len(blocks), productSchema, examples, blockText.String())
}

// buildServicePrompt maintains enhanced logic for services with better examples
func buildServicePrompt(raw string) string {
	serviceSchema := `{
  "name": "string (REQUIRED)",
  "price": "string or null - MUST be quoted string (e.g. '50000')",
  "min_price": "string or null - MUST be quoted string",
  "max_price": "string or null - MUST be quoted string",
  "currency": "string (NGN, USD, GBP, EUR)",
  "type": "string",
  "description": "string (multi-line allowed) - DO NOT include price here",
  "deliverables": ["array of strings"],
  "duration": "string (e.g. '2 weeks', '1 month')",
  "billing_period": "string (e.g. 'one-time', 'monthly', 'yearly', 'hourly')",
  "flags": {"negotiable": "boolean"}
}`

	return fmt.Sprintf(`You are extracting SERVICE DEFINITIONS from a service catalog.

CRITICAL GROUPING RULE:
- The input text contains MULTI-LINE SERVICE BLOCKS
- ALL lines between service names belong to the SAME service
- Combine multi-line descriptions into the description field

DELIVERABLES EXTRACTION:
- Look for: "Includes", "Deliverables", "You get", bullet points
- Extract as array: ["item1", "item2"]

PRICING RULES (ALL prices MUST be quoted strings):
- Single price: "₦50,000" → price="50000", min_price=null, max_price=null
- Range: "₦50,000 - ₦100,000" → min_price="50000", max_price="100000", price=null
- Hourly/Monthly rates: Extract billing_period from context
- Remove commas when outputting prices

DURATION & BILLING:
- Look for: "2 weeks", "1 month", "delivered in X days"
- Extract billing period: "per month", "/month", "monthly" → billing_period="monthly"

EXAMPLES:
Input: "Website Development\nCustom website with CMS\n₦200,000 - ₦500,000\nIncludes: Design, Development, Hosting setup\nDelivery: 3-4 weeks"
Output: {"name": "Website Development", "description": "Custom website with CMS", "min_price": "200000", "max_price": "500000", "price": null, "currency": "NGN", "deliverables": ["Design", "Development", "Hosting setup"], "duration": "3-4 weeks", "billing_period": "one-time", "type": "service"}

Output a JSON Array with all extracted services.

SCHEMA:
%s

TEXT:
%s`, serviceSchema, raw)
}

// Helper to clean up unused function
func buildExtractionPrompt(raw string, industry string) string {
	// Deprecated by new architecture, but kept to satisfy interface if needed,
	// though we are replacing the caller logic.
	return ""
}

// normalizeItem applies deterministic rules to clean up and score the parsed item
func normalizeItem(item *ParsedItem) {
	// 1. Initialize Flags
	if item.Flags == nil {
		item.Flags = map[string]bool{}
	}

	// 1b. Default Currency (Deterministic)
	if item.Currency == "" {
		item.Currency = "NGN"
	}

	// 2. Score Confidence based on Completeness (Start at 1.0, not 0.8 default)
	confidence := 1.0

	if item.Name == "" {
		confidence -= 0.4
		item.Flags["missing_name"] = true
	}

	// Pricing Check
	if item.Price == nil && item.MinPrice == nil && item.MaxPrice == nil {
		confidence -= 0.2
		item.Flags["missing_price"] = true
	}

	// Negotiation-only check (User requirement 6)
	if item.Price == nil && (item.MinPrice != nil || item.MaxPrice != nil) {
		item.Flags["negotiation_only"] = true
		// Ensure we don't penalize confidence for missing fixed price if negotiation is present
	}

	// Negotiation Logic
	if item.MinPrice != nil || item.MaxPrice != nil {
		item.Flags["negotiable"] = true
		item.Flags["has_price_range"] = true
	}

	// Duration & Billing period logic
	if item.Duration == "" {
		confidence -= 0.1
	} else {
		// Infer billing period from duration if missing
		if item.BillingPeriod == "" {
			dur := strings.ToLower(item.Duration)
			if strings.Contains(dur, "month") {
				item.BillingPeriod = "monthly"
			} else if strings.Contains(dur, "year") || strings.Contains(dur, "annum") {
				item.BillingPeriod = "yearly"
			} else if strings.Contains(dur, "hour") {
				item.BillingPeriod = "hourly"
			} else if strings.Contains(dur, "one-time") || strings.Contains(dur, "project") {
				item.BillingPeriod = "one-time"
			}
		}
	}

	// Deliverables Check
	if len(item.Deliverables) == 0 {
		confidence -= 0.1
		item.Deliverables = []string{} // Ensure non-nil
	}

	// Clamp confidence
	if confidence < 0 {
		confidence = 0
	}
	item.Confidence = confidence
}

// buildVisionPrompt creates a prompt for vision AI that handles both text and images
func buildVisionPrompt(textInput string, industry string, imageCount int) string {
	schema := `{
  "name": "string (REQUIRED)",
  "price": "number or null",
  "min_price": "number or null",
  "max_price": "number or null",
  "currency": "string", 
  "description": "string",
  "deliverables": ["array of strings"],
  "variants": {"string": ["array of strings"]},
  "duration": "string",
  "billing_period": "string", 
  "category": "string",
  "type": "string",
  "flags": "object"
}`

	contextText := ""
	if textInput != "" {
		contextText = fmt.Sprintf("\n\nADDITIONAL CONTEXT:\n%s", textInput)
	}

	imageInstruction := ""
	if imageCount == 1 {
		imageInstruction = "You are viewing 1 image containing catalog items."
	} else {
		imageInstruction = fmt.Sprintf("You are viewing %d images containing catalog items.", imageCount)
	}

	return fmt.Sprintf(`%s

Extract ALL items visible in the image(s). This is a %s catalog.

CRITICAL RULES:
1. Extract EVERY item you see in the images
2. Look for: names, prices, descriptions, categories
3. Handle price ranges: "₦1,000 - ₦5,000" → min_price=1000, max_price=5000, price=null
4. For single prices: "₦3,500" → price=3500, min_price=null, max_price=null
5. Currency symbols: ₦=NGN, $=USD, £=GBP, €=EUR
6. Extract deliverables from text like "Includes:", bullet points
7. If text is in the image, extract it accurately - do NOT invent information
8. Output ONLY valid JSON array, no markdown, no explanations

PRICE EXTRACTION FROM IMAGES:
- Look carefully at price text/numbers in images
- Common formats: "₦3,500", "NGN 3500", "$50", "₦1,000 - ₦5,000"
- Output raw numbers: 3500, NOT "3,500"

QUALITY GUIDELINES:
- If image quality is poor and text is unclear, set confidence lower
- If you cannot read a price, set price=null and flags.missing_price=true
- Group related information together (e.g., name + description + price = one item)

OUTPUT SCHEMA:
%s
%s

Output a JSON array of all extracted items:`, imageInstruction, industry, schema, contextText)
}
