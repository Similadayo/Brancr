#!/bin/bash

# Test WhatsApp Connection with Manual Channel ID
# This script tests the new /api/tenant/whatsapp/connect endpoint with channel_id

API_URL="${API_URL:-http://localhost:8080}"
TENANT_TOKEN="${TENANT_TOKEN:-your_tenant_token_here}"
PHONE_NUMBER="${PHONE_NUMBER:-+2347039841392}"
CHANNEL_ID="${CHANNEL_ID:-440617}"

echo "🧪 Testing WhatsApp Connection with Manual Channel ID"
echo "=================================================="
echo "API URL: $API_URL"
echo "Phone Number: $PHONE_NUMBER"
echo "Channel ID: $CHANNEL_ID"
echo ""

# Test 1: Connect with manual channel_id
echo "📤 Test 1: Connecting WhatsApp with manual channel_id..."
curl -X POST "$API_URL/api/tenant/whatsapp/connect" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TENANT_TOKEN" \
  -d "{
    \"phone_number\": \"$PHONE_NUMBER\",
    \"provider\": \"respondio\",
    \"channel_id\": \"$CHANNEL_ID\"
  }" \
  -w "\n\nHTTP Status: %{http_code}\n" \
  -s | jq '.' || echo "Response (raw):"

echo ""
echo "=================================================="
echo ""

# Test 2: Check connection status
echo "📊 Test 2: Checking connection status..."
curl -X GET "$API_URL/api/tenant/whatsapp/connection-status" \
  -H "Authorization: Bearer $TENANT_TOKEN" \
  -w "\n\nHTTP Status: %{http_code}\n" \
  -s | jq '.' || echo "Response (raw):"

echo ""
echo "=================================================="
echo "✅ Test completed!"

