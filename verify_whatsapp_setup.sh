#!/bin/bash

# WhatsApp Provider-Owned Setup Verification Script

echo "🔍 Verifying WhatsApp Provider-Owned Setup..."
echo ""

# Check environment variables
echo "1️⃣ Checking Environment Variables:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ -z "$BRANCR_WABA_ID" ]; then
    echo "❌ BRANCR_WABA_ID not set"
else
    echo "✅ BRANCR_WABA_ID: ${BRANCR_WABA_ID:0:10}..."
fi

if [ -z "$BRANCR_WABA_TOKEN" ]; then
    echo "❌ BRANCR_WABA_TOKEN not set"
else
    echo "✅ BRANCR_WABA_TOKEN: ${BRANCR_WABA_TOKEN:0:20}..."
fi

if [ -z "$META_SYSTEM_USER_TOKEN" ]; then
    echo "⚠️  META_SYSTEM_USER_TOKEN not set (optional)"
else
    echo "✅ META_SYSTEM_USER_TOKEN: ${META_SYSTEM_USER_TOKEN:0:20}..."
fi

if [ -z "$META_BUSINESS_ID" ]; then
    echo "⚠️  META_BUSINESS_ID not set (optional)"
else
    echo "✅ META_BUSINESS_ID: $META_BUSINESS_ID"
fi

echo ""
echo "2️⃣ Testing WABA Access (Direct API Call):"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ -n "$BRANCR_WABA_ID" ] && [ -n "$BRANCR_WABA_TOKEN" ]; then
    # Test fetching phone numbers from WABA
    response=$(curl -s -w "\n%{http_code}" \
        "https://graph.facebook.com/v20.0/${BRANCR_WABA_ID}/phone_numbers?access_token=${BRANCR_WABA_TOKEN}" \
        -H "Content-Type: application/json")
    
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')
    
    if [ "$http_code" = "200" ]; then
        phone_count=$(echo "$body" | grep -o '"id"' | wc -l)
        echo "✅ WABA connection successful!"
        echo "   Phone numbers found: $phone_count"
        echo "   WABA ID: $BRANCR_WABA_ID"
    else
        echo "❌ WABA connection failed (HTTP $http_code)"
        echo "   Response: $(echo "$body" | head -c 200)"
    fi
else
    echo "⚠️  Cannot test WABA access - missing credentials"
fi

echo ""
echo "3️⃣ Testing System User Token:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ -n "$META_SYSTEM_USER_TOKEN" ]; then
    response=$(curl -s -w "\n%{http_code}" \
        "https://graph.facebook.com/v20.0/me?access_token=${META_SYSTEM_USER_TOKEN}")
    
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')
    
    if [ "$http_code" = "200" ]; then
        echo "✅ System User Token is valid"
    else
        echo "❌ System User Token invalid (HTTP $http_code)"
        echo "   Response: $(echo "$body" | head -c 200)"
    fi
else
    echo "⚠️  System User Token not set"
fi

echo ""
echo "4️⃣ Next Steps:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "1. Start your Brancr server"
echo "2. Login to admin dashboard: /admin/login"
echo "3. Navigate to: /admin/whatsapp"
echo "4. Click 'Sync Phone Numbers' button"
echo "5. Check if phone numbers appear in the list"
echo ""

