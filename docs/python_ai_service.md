# Python AI Service Location

**The Python AI service has been moved to a separate repository.**

## Location

The Python AI service is now located at:
```
../brancr-ai/
```

This is a **separate Git repository** from the main Brancr backend.

## Why Separate?

1. **Independent deployment** - AI service can be deployed separately
2. **Different tech stack** - Python vs Go
3. **Independent scaling** - Scale AI service independently
4. **Clear boundaries** - Enforces microservice architecture
5. **Easier development** - Python devs don't need Go setup

## Running Both Services

### Terminal 1: Python AI Service
```bash
cd ../brancr-ai
python -m venv venv
source venv/bin/activate  # Windows: venv\Scripts\activate
pip install -r requirements.txt
python app.py
```

### Terminal 2: Go Backend
```bash
cd brancr
export AI_SERVICE_URL=http://localhost:8000
go run cmd/server/main.go
```

## Docker Compose

The `docker-compose.yml` in the Brancr root can still orchestrate both services:

```yaml
services:
  ai-service:
    build:
      context: ../brancr-ai  # Note: relative path
      dockerfile: Dockerfile
    # ... rest of config
```

## Development Workflow

1. **AI Service Changes**: Work in `../brancr-ai/`
2. **Backend Changes**: Work in `brancr/`
3. **Both run independently** with HTTP communication

## Environment Variable

The Go backend uses `AI_SERVICE_URL` to find the Python service:

```bash
# Default (if not set)
AI_SERVICE_URL=http://localhost:8000

# Production
AI_SERVICE_URL=https://ai.brancr.com

# Docker Compose
AI_SERVICE_URL=http://ai-service:8000
```

## Fallback Behavior

If the Python service is unavailable, the Go backend automatically falls back to the V3 adapter (in-process AI logic).

This means:
- ✅ System always works
- ✅ Python service can be down during development
- ✅ Gradual migration is safe

---

**See**: `../brancr-ai/README.md` for Python service documentation
