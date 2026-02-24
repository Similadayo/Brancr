# Running Database Migrations on Railway

## Problem
Migrations are not being applied to your PostgreSQL database on Railway.

## Solution

### Option 1: Run migrations manually (Recommended for immediate fix)

1. **Get your Railway database connection string:**
   - Go to your Railway project
   - Click on your PostgreSQL service
   - Copy the `DATABASE_URL` from the Variables tab

2. **Run migrations locally (if you have Railway CLI):**
   ```bash
   railway run go run scripts/run_migrations.go
   ```

3. **Or run migrations from your local machine:**
   ```bash
   export DATABASE_URL="postgres://user:pass@host:port/dbname"
   go run scripts/run_migrations.go
   ```

### Option 2: Add migration step to Railway deployment

Add this to your Railway service's build/start command:

**In Railway Dashboard:**
1. Go to your service settings
2. Add a new "Deploy" command or modify the start command:

```bash
# Build the app
go build -o bin/brancr ./cmd/brancr

# Run migrations before starting
go run scripts/run_migrations.go

# Start the app
./bin/brancr
```

**Or use a startup script:**

Create `scripts/start.sh`:
```bash
#!/bin/bash
set -e

echo "🔌 Running database migrations..."
go run scripts/run_migrations.go

echo "🚀 Starting application..."
exec ./bin/brancr
```

Then set Railway's start command to: `./scripts/start.sh`

### Option 3: Verify migrations are running on app startup

The app should automatically run migrations on startup (see `internal/core/app.go:216`). 

**Check if migrations are failing silently:**
1. Check Railway logs for migration errors
2. Look for messages like "sql migrations failed" or "migrations directory not found"

**Common issues:**
- Migrations path is wrong (should be `migrations` relative to app root)
- Database connection fails before migrations run
- Migration files aren't included in deployment

### Verify migrations were applied

After running migrations, check the database:

```sql
-- Check migration version
SELECT * FROM schema_migrations;

-- Verify tables exist
SELECT table_name 
FROM information_schema.tables 
WHERE table_schema = 'public' 
ORDER BY table_name;
```

You should see tables like:
- `conversation_ai_states`
- `ai_feedback_events`
- `sales_conversations` (with `last_ai_offer` and `escalation_pending` columns)
- etc.

### Troubleshooting

**If migrations still don't run:**

1. **Check file naming:** Migration files should use `-- +migrate Up` and `-- +migrate Down` comments (which they do)

2. **Check migration path:** The app looks for migrations in the `migrations/` directory relative to where the binary runs

3. **Check Railway working directory:** Make sure Railway is running from the project root, not a subdirectory

4. **Manual migration check:**
   ```bash
   # Install golang-migrate CLI
   go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
   
   # Check migration status
   migrate -path migrations -database "$DATABASE_URL" version
   
   # Run migrations manually
   migrate -path migrations -database "$DATABASE_URL" up
   ```

