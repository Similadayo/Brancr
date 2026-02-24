-- Backfill payment references to new format: {TENANT}-{ITEM}-{DATE}-{SEQ}
-- Safe defaults: uses tenant name slug (A-Z0-9, max 10 chars), first item name slug (A-Z0-9, max 10 chars, fallback ORDER),
-- date from order created_at (YYYYMMDD), and a per-tenant sequence padded to 3 digits.
-- Run inside a transaction and take a backup/snapshot first.

BEGIN;

-- 1) Build slugs and new refs for orders still using the old BRNCR-* format
WITH tenant_slugs AS (
    SELECT id AS tenant_id,
           left(regexp_replace(upper(COALESCE(NULLIF(name, ''), 'TENANT' || id)), '[^A-Z0-9]+', '', 'g'), 10) AS tenant_slug
    FROM tenants
),
order_refs AS (
    SELECT o.id,
           ts.tenant_slug,
           left(regexp_replace(upper(COALESCE(NULLIF((jsonb_path_query_first(o.items::jsonb, '$[0].name')::text)::text, ''), 'ORDER')), '[^A-Z0-9]+', '', 'g'), 10) AS item_slug,
           to_char(o.created_at, 'YYYYMMDD') AS dt,
           row_number() OVER (PARTITION BY o.tenant_id ORDER BY o.id) AS seq
    FROM orders o
    JOIN tenant_slugs ts ON ts.tenant_id = o.tenant_id
    WHERE o.payment_reference LIKE 'BRNCR-%'
),
updated_orders AS (
    UPDATE orders o
    SET payment_reference = concat_ws('-', tenant_slug, item_slug, dt, lpad(seq::text, 3, '0'))
    FROM order_refs r
    WHERE o.id = r.id
    RETURNING o.id, o.payment_reference
)
UPDATE payments p
SET payment_reference = u.payment_reference
FROM updated_orders u
WHERE p.order_id = u.id;

COMMIT;

-- Verification: should return zero rows
-- SELECT o.id FROM orders o JOIN payments p ON p.order_id = o.id WHERE o.payment_reference <> p.payment_reference;
