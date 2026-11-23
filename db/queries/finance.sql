-- name: PostJournal :one
SELECT finance_post(sqlc.arg(event_key)::text,sqlc.arg(subject)::uuid,sqlc.narg(owner)::uuid,
    sqlc.arg(code)::text,sqlc.arg(buckets)::text[],sqlc.arg(amounts)::bigint[])::boolean;

-- name: RecordLedgerSale :exec
SELECT finance_sale(sqlc.arg(subject)::uuid,sqlc.arg(seller)::uuid,sqlc.arg(code)::text,
    sqlc.arg(gross)::bigint,sqlc.arg(test_units)::bigint);

-- name: LockSellerFunds :exec
SELECT finance_lock(sqlc.arg(seller)::uuid);

-- name: SellerBalances :many
SELECT a.denomination,a.bucket,sum(l.units)::bigint AS balance
FROM ledger_accounts a JOIN journal_lines l ON l.account_id=a.id
JOIN journal_entries e ON e.id=l.entry_id AND e.sealed
WHERE a.owner_id=$1 GROUP BY a.denomination,a.bucket ORDER BY a.denomination,a.bucket;

-- name: PayoutByKey :one
SELECT * FROM seller_payouts WHERE seller_id=$1 AND request_key=$2;

-- name: InsertPayout :one
INSERT INTO seller_payouts(seller_id,request_key,currency,amount_minor,test_units,state,scenario)
VALUES($1,$2,$3,$4,$5,'reserved',$6) RETURNING id;

-- name: LockPayout :one
SELECT * FROM seller_payouts WHERE id=$1 FOR UPDATE;

-- name: SellerPayout :one
SELECT * FROM seller_payouts WHERE id=$1 AND seller_id=$2;

-- name: SellerPayouts :many
SELECT * FROM seller_payouts WHERE seller_id=sqlc.arg(seller_id)
ORDER BY created_at DESC,id LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: FinancePayoutQueue :many
SELECT p.id,p.currency,p.amount_minor,p.state,p.test_units,u.username
FROM seller_payouts p JOIN users u ON u.id=p.seller_id
WHERE (sqlc.arg(state)::text='' AND (p.state NOT IN ('denied','reconciled') OR p.hold_active))
    OR p.state=sqlc.arg(state)::text
ORDER BY p.created_at,p.id LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: FinancePayout :one
SELECT p.id,p.seller_id,p.currency,p.amount_minor,p.test_units,p.state,p.attempts,p.hold_active,u.username,
       finance_balance(p.seller_id,p.currency,'available')::bigint AS available,
       finance_balance(p.seller_id,p.currency,'held')::bigint AS held
FROM seller_payouts p JOIN users u ON u.id=p.seller_id WHERE p.id=$1;

-- name: SetPayoutState :exec
UPDATE seller_payouts SET state=sqlc.arg(state),attempts=sqlc.arg(attempts)
WHERE id=sqlc.arg(id);

-- name: AuditPayout :exec
INSERT INTO payout_reviews(payout_id,actor_id,action,reason) VALUES($1,$2,$3,$4);

-- name: PayoutActivity :many
SELECT action,reason,created_at FROM payout_reviews WHERE payout_id=$1 ORDER BY id;

-- name: RecordReconciliation :exec
INSERT INTO reconciliation_items(payout_id,provider_ref,settled_units) VALUES($1,$2,$3);

-- name: PayoutReconciliation :one
SELECT * FROM reconciliation_items WHERE payout_id=$1;

-- name: UserPayoutNotices :many
SELECT p.id,a.action,a.created_at
FROM seller_payouts p JOIN payout_reviews a ON a.payout_id=p.id
WHERE p.seller_id=sqlc.arg(seller_id) AND a.action IN ('approved','denied','confirmed','failed','stalled','reconciled','hold_set','hold_lifted')
ORDER BY a.id DESC LIMIT 30 OFFSET sqlc.arg(page_offset)::integer;

-- name: FirstOrderObservation :one
SELECT units FROM payment_observations WHERE order_id=$1 ORDER BY created_at,event_key LIMIT 1;

-- name: FinanceOrderQueue :many
SELECT o.id,o.state,i.title,i.currency,i.price_minor
FROM orders o JOIN order_items i ON i.order_id=o.id
WHERE o.state IN ('fulfillment_pending','fulfilled','completed','fulfillment_failed','disputed','under_review')
AND NOT EXISTS(SELECT 1 FROM disputes d WHERE d.order_id=o.id)
AND EXISTS (SELECT 1 FROM payment_observations p WHERE p.order_id=o.id)
ORDER BY o.created_at DESC,o.id LIMIT 31 OFFSET sqlc.arg(page_offset)::integer;

-- name: SetPayoutHold :exec
UPDATE seller_payouts SET hold_active=$2 WHERE id=$1;
