# Monitoring & Observability Setup Guide

This guide covers setting up monitoring, alerting, and observability for Brancr in production.

## Overview

Brancr provides several observability features out of the box:
- **Prometheus Metrics**: HTTP, database, and business metrics
- **Structured Logging**: JSON logs with correlation IDs
- **Health Checks**: Liveness, readiness, and dependency checks
- **Slow Query Detection**: Automatic logging of slow database queries

## Metrics (Prometheus)

### Endpoint

Access Prometheus metrics at:
```
GET /metrics
```

### Available Metrics

#### HTTP Metrics
- `http_requests_total`: Total HTTP requests by method, path, status
- `http_request_duration_seconds`: Request duration histogram
- `http_request_size_bytes`: Request body size
- `http_response_size_bytes`: Response body size

#### Database Metrics
- `db_queries_total`: Total database queries
- `db_query_duration_seconds`: Query duration
- `db_connections_active`: Active database connections
- `db_connections_idle`: Idle database connections

#### Business Metrics
- `posts_scheduled_total`: Total posts scheduled
- `posts_published_total`: Total posts published
- `posts_failed_total`: Total posts failed
- `ai_caption_generated_total`: Total AI captions generated

### Prometheus Configuration

Example `prometheus.yml`:

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'brancr'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
    scrape_interval: 10s
```

### Grafana Dashboard

Import the following dashboard JSON for Grafana:

```json
{
  "dashboard": {
    "title": "Brancr Metrics",
    "panels": [
      {
        "title": "HTTP Request Rate",
        "targets": [{
          "expr": "rate(http_requests_total[5m])"
        }]
      },
      {
        "title": "HTTP Error Rate",
        "targets": [{
          "expr": "rate(http_requests_total{status=~\"5..\"}[5m])"
        }]
      },
      {
        "title": "Database Query Duration",
        "targets": [{
          "expr": "histogram_quantile(0.95, db_query_duration_seconds_bucket)"
        }]
      },
      {
        "title": "Posts Published",
        "targets": [{
          "expr": "rate(posts_published_total[5m])"
        }]
      }
    ]
  }
}
```

## Logging

### Log Format

Brancr uses structured JSON logging with the following fields:

```json
{
  "level": "info",
  "ts": 1234567890.123,
  "caller": "handler.go:123",
  "msg": "request completed",
  "request_id": "abc-123-def",
  "tenant_id": 1,
  "method": "GET",
  "path": "/api/posts",
  "status": 200,
  "duration_ms": 45.2
}
```

### Log Levels

- **DEBUG**: Detailed information (development only)
- **INFO**: General informational messages
- **WARN**: Warning messages (slow queries, rate limit warnings)
- **ERROR**: Error messages (failed requests, exceptions)

### Correlation IDs

Every HTTP request receives a unique `request_id` that is:
- Added to all log entries for that request
- Returned in response headers (`X-Request-ID`)
- Propagated to downstream services

### Log Aggregation

#### ELK Stack (Elasticsearch, Logstash, Kibana)

Example Logstash configuration:

```ruby
input {
  file {
    path => "/var/log/brancr/*.log"
    codec => "json"
  }
}

filter {
  json {
    source => "message"
  }
  
  date {
    match => [ "ts", "UNIX" ]
  }
}

output {
  elasticsearch {
    hosts => ["localhost:9200"]
    index => "brancr-%{+YYYY.MM.dd}"
  }
}
```

#### Loki (Grafana)

Example Promtail configuration:

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://loki:3100/loki/api/v1/push

scrape_configs:
  - job_name: brancr
    static_configs:
      - targets:
          - localhost
        labels:
          job: brancr
          __path__: /var/log/brancr/*.log
```

## Health Checks

### Endpoints

#### Basic Health Check
```
GET /health
```
Returns: `{"status": "ok", "timestamp": "..."}`

#### Readiness Check
```
GET /health/ready
```
Checks:
- Database connectivity
- Redis connectivity (if configured)
- Connection pool status

Returns detailed status of each dependency.

#### Liveness Check
```
GET /health/live
```
Simple check to verify the application is running.

### Kubernetes Integration

Example Kubernetes liveness/readiness probes:

```yaml
livenessProbe:
  httpGet:
    path: /health/live
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /health/ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 3
```

## Alerting

### Recommended Alerts

#### High Error Rate
```yaml
alert: HighErrorRate
expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.1
for: 5m
annotations:
  summary: "High error rate detected"
  description: "Error rate is {{ $value }} errors/second"
```

#### Slow Database Queries
```yaml
alert: SlowDatabaseQueries
expr: histogram_quantile(0.95, db_query_duration_seconds_bucket) > 1
for: 5m
annotations:
  summary: "Slow database queries detected"
  description: "95th percentile query time is {{ $value }}s"
```

#### Database Connection Pool Exhaustion
```yaml
alert: DatabasePoolExhausted
expr: db_connections_active / db_connections_max > 0.9
for: 5m
annotations:
  summary: "Database connection pool nearly exhausted"
  description: "{{ $value }}% of connections in use"
```

#### High Post Failure Rate
```yaml
alert: HighPostFailureRate
expr: rate(posts_failed_total[5m]) / rate(posts_scheduled_total[5m]) > 0.1
for: 10m
annotations:
  summary: "High post failure rate"
  description: "{{ $value }}% of posts are failing"
```

#### Service Down
```yaml
alert: ServiceDown
expr: up{job="brancr"} == 0
for: 1m
annotations:
  summary: "Brancr service is down"
  description: "Service has been down for more than 1 minute"
```

### Alertmanager Configuration

Example `alertmanager.yml`:

```yaml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 12h
  receiver: 'web.hook'

receivers:
  - name: 'web.hook'
    webhook_configs:
      - url: 'https://hooks.slack.com/services/YOUR/WEBHOOK/URL'
        send_resolved: true
```

## Slow Query Detection

Brancr automatically logs slow database queries:

- **Development**: All queries are logged
- **Production**: Queries taking >1 second are logged as warnings

Example log entry:

```json
{
  "level": "warn",
  "msg": "slow database query",
  "sql": "SELECT * FROM scheduled_posts WHERE tenant_id = $1",
  "rows": 100,
  "duration": "1.5s",
  "threshold": "1s"
}
```

### Query Optimization

When slow queries are detected:

1. **Check Indexes**: Ensure proper indexes on frequently queried columns
2. **Review Query Logic**: Look for N+1 query patterns
3. **Add Caching**: Cache frequently accessed data
4. **Query Analysis**: Use `EXPLAIN ANALYZE` in PostgreSQL

## Distributed Tracing (Future)

While not yet implemented, distributed tracing can be added using:

- **OpenTelemetry**: Standard observability framework
- **Jaeger**: Distributed tracing backend
- **Zipkin**: Alternative tracing backend

### Implementation Plan

1. Add OpenTelemetry SDK
2. Instrument HTTP handlers
3. Instrument database queries
4. Instrument external API calls
5. Configure trace exporter

## Best Practices

### Logging
- ✅ Use structured logging with consistent fields
- ✅ Include correlation IDs in all logs
- ✅ Log at appropriate levels (don't log sensitive data)
- ✅ Use context for request-scoped data

### Metrics
- ✅ Use consistent metric naming (`service_metric_unit`)
- ✅ Include relevant labels (tenant_id, platform, etc.)
- ✅ Avoid high-cardinality labels
- ✅ Document all metrics

### Alerting
- ✅ Set appropriate thresholds (avoid alert fatigue)
- ✅ Use alert grouping to reduce noise
- ✅ Include runbooks in alert descriptions
- ✅ Test alerting channels regularly

### Performance
- ✅ Monitor p95 and p99 latencies, not just averages
- ✅ Track error rates separately from success rates
- ✅ Monitor resource usage (CPU, memory, connections)
- ✅ Set up capacity planning alerts

## Tools & Services

### Open Source
- **Prometheus**: Metrics collection
- **Grafana**: Visualization
- **Loki**: Log aggregation
- **Jaeger**: Distributed tracing
- **Alertmanager**: Alert routing

### Managed Services
- **Datadog**: Full-stack observability
- **New Relic**: APM and monitoring
- **Sentry**: Error tracking
- **LogRocket**: Session replay and logging
- **Honeycomb**: Observability platform

## Example Setup

### Docker Compose for Monitoring Stack

```yaml
version: '3.8'

services:
  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    ports:
      - "9090:9090"

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin

  loki:
    image: grafana/loki:latest
    ports:
      - "3100:3100"

  promtail:
    image: grafana/promtail:latest
    volumes:
      - ./promtail.yml:/etc/promtail/promtail.yml
      - /var/log/brancr:/var/log/brancr:ro
```

## References

- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
- [OpenTelemetry Documentation](https://opentelemetry.io/docs/)
- [Production Deployment Guide](./PRODUCTION_DEPLOYMENT.md)

