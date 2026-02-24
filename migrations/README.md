# Database Migrations

This directory contains SQL migration files for database schema changes.

## Migration File Naming

Migrations follow the naming convention:
- `{version}_{up|down}_{description}.sql`
- Example: `000001_up_create_users_table.sql`, `000001_down_drop_users_table.sql`

## Creating Migrations

### Using golang-migrate CLI

1. Install golang-migrate:
   ```bash
   go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
   ```

2. Create a new migration:
   ```bash
   migrate create -ext sql -dir migrations -seq add_enhance_caption_column
   ```

3. Edit the generated `{version}_up_*.sql` and `{version}_down_*.sql` files

### Manual Creation

1. Create two files:
   - `{next_version}_up_{description}.sql` - Migration to apply
   - `{next_version}_down_{description}.sql` - Rollback migration

2. Version numbers should be sequential (000001, 000002, etc.)

## Running Migrations

Migrations are automatically run on application startup. You can also run them manually:

```bash
# Using Makefile
make migrate

# Or using golang-migrate CLI directly
migrate -path migrations -database "postgres://user:pass@localhost/dbname?sslmode=disable" up
```

## Migration Best Practices

1. **Always create both UP and DOWN migrations**
   - UP: Applies the change
   - DOWN: Reverses the change

2. **Test migrations in development first**
   - Test both up and down migrations
   - Verify data integrity after migration

3. **Use transactions when possible**
   - Wrap migrations in transactions for atomicity
   - Some operations (like ALTER TYPE) cannot be rolled back

4. **Keep migrations small and focused**
   - One logical change per migration
   - Easier to review and rollback

5. **Never modify existing migrations**
   - Once applied to production, migrations are immutable
   - Create new migrations to fix issues

6. **Document complex migrations**
   - Add comments explaining why the migration is needed
   - Document any data transformations

## Example Migration

### UP Migration (`000001_up_add_enhance_caption.sql`)
```sql
-- Add enhance_caption column to scheduled_posts table
ALTER TABLE scheduled_posts 
ADD COLUMN enhance_caption BOOLEAN NOT NULL DEFAULT false;

-- Add comment for documentation
COMMENT ON COLUMN scheduled_posts.enhance_caption IS 
'If true, enhance existing caption with AI (Mode 3); if false, use caption as-is (Mode 2)';
```

### DOWN Migration (`000001_down_remove_enhance_caption.sql`)
```sql
-- Remove enhance_caption column from scheduled_posts table
ALTER TABLE scheduled_posts 
DROP COLUMN IF EXISTS enhance_caption;
```

## Migration Status

Check migration status:
```bash
migrate -path migrations -database "postgres://..." version
```

## Troubleshooting

### Dirty Migration State

If a migration fails partway through, the database may be in a "dirty" state. To fix:

1. Manually fix the database state
2. Update the schema_migrations table:
   ```sql
   UPDATE schema_migrations SET dirty = false;
   ```
3. Re-run the migration

### Rollback

To rollback the last migration:
```bash
migrate -path migrations -database "postgres://..." down 1
```

To rollback multiple migrations:
```bash
migrate -path migrations -database "postgres://..." down {number}
```

