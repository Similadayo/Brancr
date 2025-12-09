import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const p95Latency = new Trend('p95_latency');
const p99Latency = new Trend('p99_latency');

export const options = {
  stages: [
    { duration: '2m', target: 50 },   // Ramp up to 50 users
    { duration: '5m', target: 50 },   // Stay at 50 users
    { duration: '2m', target: 100 },  // Ramp up to 100 users
    { duration: '5m', target: 100 },  // Stay at 100 users
    { duration: '2m', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'], // 95% under 500ms, 99% under 1s
    http_req_failed: ['rate<0.01'], // Less than 1% errors
    errors: ['rate<0.01'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

// Test data - simulate multiple tenants
const tenants = Array.from({ length: 20 }, (_, i) => ({
  id: i + 1,
  token: `tenant_token_${i + 1}`, // In real test, use actual JWT tokens
}));

export default function () {
  const tenant = tenants[Math.floor(Math.random() * tenants.length)];
  
  // Test health endpoint
  let res = http.get(`${BASE_URL}/health`);
  check(res, {
    'health check status 200': (r) => r.status === 200,
  });
  errorRate.add(res.status >= 400);
  
  sleep(1);
  
  // Test API endpoint with tenant auth
  const headers = {
    'Authorization': `Bearer ${tenant.token}`,
    'Content-Type': 'application/json',
  };
  
  // Test GET /api/tenant/posts (if available)
  res = http.get(`${BASE_URL}/api/tenant/posts`, { headers });
  check(res, {
    'posts endpoint status 200 or 401': (r) => r.status === 200 || r.status === 401,
  });
  errorRate.add(res.status >= 400 && res.status !== 401);
  
  if (res.status === 200) {
    p95Latency.add(res.timings.duration);
    p99Latency.add(res.timings.duration);
  }
  
  sleep(2);
  
  // Test POST endpoint (create post)
  const postData = JSON.stringify({
    name: `Load test post ${Date.now()}`,
    caption: 'Test caption',
    platforms: ['instagram'],
    scheduled_at: new Date(Date.now() + 3600000).toISOString(),
  });
  
  res = http.post(`${BASE_URL}/api/tenant/posts`, postData, { headers });
  check(res, {
    'create post status 201 or 401 or 429': (r) => 
      r.status === 201 || r.status === 401 || r.status === 429,
  });
  errorRate.add(res.status >= 400 && res.status !== 401 && res.status !== 429);
  
  sleep(1);
}

export function handleSummary(data) {
  return {
    'stdout': textSummary(data, { indent: ' ', enableColors: true }),
    'load_test_results.json': JSON.stringify(data, null, 2),
  };
}

function textSummary(data, options) {
  // Simple text summary
  return `
Load Test Results
==================
Duration: ${data.state.testRunDurationMs / 1000}s
VUs: ${data.metrics.vus.values.count}
Requests: ${data.metrics.http_reqs.values.count}
Errors: ${data.metrics.http_req_failed.values.rate * 100}%
P95 Latency: ${data.metrics.http_req_duration.values['p(95)']}ms
P99 Latency: ${data.metrics.http_req_duration.values['p(99)']}ms
  `;
}

