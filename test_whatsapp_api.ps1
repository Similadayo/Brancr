# PowerShell script to test WhatsApp connection API
# Usage: .\test_whatsapp_api.ps1

param(
    [string]$ApiUrl = "http://localhost:8080",
    [string]$TenantToken = "your_tenant_token_here",
    [string]$PhoneNumber = "+2347039841392",
    [string]$ChannelId = "440617"
)

Write-Host "🧪 Testing WhatsApp Connection with Manual Channel ID" -ForegroundColor Cyan
Write-Host "==================================================" -ForegroundColor Cyan
Write-Host "API URL: $ApiUrl"
Write-Host "Phone Number: $PhoneNumber"
Write-Host "Channel ID: $ChannelId"
Write-Host ""

# Test 1: Connect with manual channel_id
Write-Host "📤 Test 1: Connecting WhatsApp with manual channel_id..." -ForegroundColor Yellow

$body = @{
    phone_number = $PhoneNumber
    provider = "respondio"
    channel_id = $ChannelId
} | ConvertTo-Json

$headers = @{
    "Content-Type" = "application/json"
    "Authorization" = "Bearer $TenantToken"
}

try {
    $response = Invoke-RestMethod -Uri "$ApiUrl/api/tenant/whatsapp/connect" `
        -Method POST `
        -Headers $headers `
        -Body $body `
        -ErrorAction Stop
    
    Write-Host "✅ Success!" -ForegroundColor Green
    $response | ConvertTo-Json -Depth 10
} catch {
    Write-Host "❌ Error: $_" -ForegroundColor Red
    if ($_.Exception.Response) {
        $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
        $responseBody = $reader.ReadToEnd()
        Write-Host "Response: $responseBody" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "==================================================" -ForegroundColor Cyan
Write-Host ""

# Test 2: Check connection status
Write-Host "📊 Test 2: Checking connection status..." -ForegroundColor Yellow

$statusHeaders = @{
    "Authorization" = "Bearer $TenantToken"
}

try {
    $statusResponse = Invoke-RestMethod -Uri "$ApiUrl/api/tenant/whatsapp/connection-status" `
        -Method GET `
        -Headers $statusHeaders `
        -ErrorAction Stop
    
    Write-Host "✅ Status retrieved!" -ForegroundColor Green
    $statusResponse | ConvertTo-Json -Depth 10
} catch {
    Write-Host "❌ Error: $_" -ForegroundColor Red
}

Write-Host ""
Write-Host "==================================================" -ForegroundColor Cyan
Write-Host "✅ Test completed!" -ForegroundColor Green

