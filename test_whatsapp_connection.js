/**
 * Test WhatsApp Connection API with Manual Channel ID
 * Usage: node test_whatsapp_connection.js
 */

const API_URL = process.env.API_URL || 'http://localhost:8080';
const TENANT_TOKEN = process.env.TENANT_TOKEN || 'your_tenant_token_here';
const PHONE_NUMBER = process.env.PHONE_NUMBER || '+2347039841392';
const CHANNEL_ID = process.env.CHANNEL_ID || '440617';

async function testWhatsAppConnection() {
  console.log('🧪 Testing WhatsApp Connection with Manual Channel ID');
  console.log('==================================================');
  console.log(`API URL: ${API_URL}`);
  console.log(`Phone Number: ${PHONE_NUMBER}`);
  console.log(`Channel ID: ${CHANNEL_ID}`);
  console.log('');

  // Test 1: Connect with manual channel_id
  console.log('📤 Test 1: Connecting WhatsApp with manual channel_id...');
  
  try {
    const response = await fetch(`${API_URL}/api/tenant/whatsapp/connect`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${TENANT_TOKEN}`,
      },
      body: JSON.stringify({
        phone_number: PHONE_NUMBER,
        provider: 'respondio',
        channel_id: CHANNEL_ID,
      }),
    });

    const data = await response.json();

    if (!response.ok) {
      console.error('❌ Error:', data);
      return;
    }

    console.log('✅ Success!');
    console.log(JSON.stringify(data, null, 2));
  } catch (error) {
    console.error('❌ Request failed:', error.message);
    return;
  }

  console.log('');
  console.log('==================================================');
  console.log('');

  // Test 2: Check connection status
  console.log('📊 Test 2: Checking connection status...');
  
  try {
    const response = await fetch(`${API_URL}/api/tenant/whatsapp/connection-status`, {
      method: 'GET',
      headers: {
        'Authorization': `Bearer ${TENANT_TOKEN}`,
      },
    });

    const data = await response.json();

    if (!response.ok) {
      console.error('❌ Error:', data);
      return;
    }

    console.log('✅ Status retrieved!');
    console.log(JSON.stringify(data, null, 2));
  } catch (error) {
    console.error('❌ Request failed:', error.message);
  }

  console.log('');
  console.log('==================================================');
  console.log('✅ Test completed!');
}

// Run the test
testWhatsAppConnection().catch(console.error);

