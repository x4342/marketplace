\set ON_ERROR_STOP on
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM journal_entries e
        LEFT JOIN journal_lines l ON l.entry_id = e.id
        LEFT JOIN ledger_accounts a ON a.id = l.account_id
        GROUP BY e.id
        HAVING NOT e.sealed OR count(l.entry_id) < 2 OR sum(l.units) <> 0
            OR NOT bool_and(a.denomination = e.denomination)
    ) THEN
        RAISE EXCEPTION 'Unsealed, unbalanced, or mixed-denomination journal';
    END IF;

    IF EXISTS (
        SELECT 1 FROM ledger_accounts a JOIN journal_lines l ON l.account_id = a.id
        WHERE a.owner_id IS NOT NULL GROUP BY a.id HAVING sum(l.units) < 0
    ) THEN
        RAISE EXCEPTION 'Negative seller balance';
    END IF;

    IF EXISTS (
        SELECT 1 FROM seller_payouts p
        LEFT JOIN reconciliation_items r ON r.payout_id = p.id
        LEFT JOIN LATERAL (
            SELECT coalesce(sum(l.units) FILTER (WHERE a.bucket = 'reserved'), 0) AS reserved,
                   coalesce(sum(l.units) FILTER (WHERE a.bucket = 'withdrawn'), 0) AS withdrawn,
                   coalesce(sum(l.units) FILTER (WHERE a.bucket = 'held'), 0) AS held
            FROM journal_entries e JOIN journal_lines l ON l.entry_id = e.id
            JOIN ledger_accounts a ON a.id = l.account_id
            WHERE e.subject_id = p.id AND a.owner_id = p.seller_id AND a.denomination = p.currency
        ) amounts ON true
        WHERE amounts.reserved <> CASE
                  WHEN p.state IN ('denied', 'confirmed', 'reconciled') THEN 0 ELSE p.amount_minor END
           OR amounts.withdrawn <> CASE
                  WHEN p.state IN ('confirmed', 'reconciled') THEN p.amount_minor ELSE 0 END
           OR amounts.held <> CASE
                  WHEN p.state = 'denied' AND p.hold_active THEN p.amount_minor ELSE 0 END
           OR (p.state IN ('confirmed', 'reconciled') AND r.settled_units IS DISTINCT FROM p.test_units)
           OR (p.state NOT IN ('confirmed', 'reconciled') AND r.payout_id IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'Payout reservation, hold, or settlement mismatch';
    END IF;

    IF EXISTS (
        SELECT 1 FROM journal_entries e
        WHERE e.event_key = 'payment-test:' || e.subject_id::text
          AND NOT EXISTS (SELECT 1 FROM conversation_system_events c WHERE c.order_id = e.subject_id)
    ) OR EXISTS (
        SELECT 1 FROM conversation_system_events c
        WHERE NOT EXISTS (SELECT 1 FROM journal_entries e WHERE e.event_key = 'payment-test:' || c.order_id::text)
    ) THEN
        RAISE EXCEPTION 'Verified payment and private purchase event mismatch';
    END IF;
END;
$$;

SELECT (SELECT count(*) FROM journal_entries) AS journals,
       (SELECT count(*) FROM seller_payouts) AS payouts,
       (SELECT count(*) FROM reconciliation_items) AS reconciliations,
       (SELECT count(*) FROM orders WHERE state = 'refunded') AS refunds;
COMMIT;
