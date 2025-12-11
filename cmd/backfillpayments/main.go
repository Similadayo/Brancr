package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
)

const backfillPaymentsSQL = `
BEGIN;

WITH tenant_slugs AS (
    SELECT id AS tenant_id,
           left(regexp_replace(upper(COALESCE(NULLIF(name, ''), 'TENANT' || id)), '[^A-Z0-9]+', '', 'g'), 10) AS tenant_slug
    FROM tenants
),
missing AS (
    SELECT o.id AS order_id,
           o.tenant_id,
           o.customer_id,
           o.payment_reference,
           o.total_amount,
           o.currency,
           o.created_at,
           ts.tenant_slug
    FROM orders o
    LEFT JOIN payments p ON p.order_id = o.id
    JOIN tenant_slugs ts ON ts.tenant_id = o.tenant_id
    WHERE p.id IS NULL
)
INSERT INTO payments (
    tenant_id, order_id, customer_id, payment_reference, amount, currency,
    payment_method, status, verification_status, claimed_by_customer, claimed_at,
    metadata, created_at
)
SELECT m.tenant_id,
       m.order_id,
       m.customer_id,
       m.payment_reference,
       CASE WHEN m.total_amount > 0 THEN m.total_amount ELSE 1 END,
       COALESCE(NULLIF(m.currency, ''), 'NGN'),
       'cash_on_delivery',
       'pending',
       'tenant_verified',
       false,
       m.created_at,
       '{}',
       now()
FROM missing m;

COMMIT;
`

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(backfillPaymentsSQL); err != nil {
		log.Fatalf("backfill payments failed: %v", err)
	}

	fmt.Println("Payment backfill completed successfully at", time.Now())
}
