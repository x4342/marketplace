-- +goose Up
CREATE TABLE ledger_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid REFERENCES users(id),
    denomination text NOT NULL CHECK (denomination IN ('USD','EUR','TEST')),
    bucket text NOT NULL,
    UNIQUE NULLS NOT DISTINCT(owner_id, denomination, bucket),
    CHECK (
        (owner_id IS NOT NULL AND denomination IN ('USD','EUR') AND bucket IN ('pending','available','held','reserved','withdrawn'))
        OR (owner_id IS NULL AND denomination IN ('USD','EUR') AND bucket IN ('sale_clearing','commission'))
        OR (owner_id IS NULL AND denomination='TEST' AND bucket IN ('test_treasury','test_provider','test_conversion','test_external'))
    )
);
CREATE TABLE journal_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_key text NOT NULL UNIQUE,
    subject_id uuid NOT NULL,
    denomination text NOT NULL CHECK (denomination IN ('USD','EUR','TEST')),
    sealed boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE journal_lines (
    entry_id uuid NOT NULL REFERENCES journal_entries(id),
    account_id uuid NOT NULL REFERENCES ledger_accounts(id),
    units bigint NOT NULL CHECK (units <> 0),
    PRIMARY KEY (entry_id,account_id)
);
CREATE INDEX journal_lines_account_idx ON journal_lines(account_id,entry_id);
CREATE INDEX journal_subject_idx ON journal_entries(subject_id,event_key);

-- +goose StatementBegin
CREATE FUNCTION guard_journal_entry() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' AND NOT NEW.sealed THEN RETURN NEW; END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NOT OLD.sealed AND NEW.sealed
           AND (to_jsonb(NEW)-'sealed') = (to_jsonb(OLD)-'sealed')
           AND (SELECT count(*) >= 2 AND sum(l.units) = 0 AND bool_and(a.denomination=NEW.denomination)
                FROM journal_lines l JOIN ledger_accounts a ON a.id=l.account_id WHERE l.entry_id=NEW.id)
        THEN RETURN NEW; END IF;
    END IF;
    RAISE EXCEPTION 'journal entry must be balanced, single-denomination and immutable once posted';
END;
$$;
CREATE FUNCTION guard_journal_line() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM journal_entries e WHERE e.id=NEW.entry_id AND NOT e.sealed FOR UPDATE)
    THEN RAISE EXCEPTION 'cannot append to a posted journal'; END IF;
    RETURN NEW;
END;
$$;
CREATE FUNCTION require_sealed_journal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM journal_entries e WHERE e.id=NEW.id AND e.sealed)
    THEN RAISE EXCEPTION 'journal must be sealed before commit'; END IF;
    RETURN NULL;
END;
$$;
CREATE FUNCTION finance_lock(owner uuid) RETURNS void LANGUAGE sql AS $$
    SELECT pg_advisory_xact_lock(hashtextextended(owner::text, 88703));
$$;
CREATE FUNCTION finance_balance(owner uuid, code text, account_bucket text) RETURNS bigint LANGUAGE sql STABLE AS $$
    SELECT coalesce(sum(l.units),0)::bigint FROM ledger_accounts a
    JOIN journal_lines l ON l.account_id=a.id JOIN journal_entries e ON e.id=l.entry_id AND e.sealed
    WHERE a.owner_id=owner AND a.denomination=code AND a.bucket=account_bucket;
$$;
CREATE FUNCTION finance_post(event text, subject uuid, owner uuid, code text, buckets text[], amounts bigint[]) RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE entry uuid;
DECLARE account uuid;
DECLARE holder uuid;
DECLARE pos integer;
BEGIN
    IF owner IS NOT NULL THEN PERFORM finance_lock(owner); END IF;
    INSERT INTO journal_entries(event_key,subject_id,denomination)
    VALUES(event,subject,code) ON CONFLICT(event_key) DO NOTHING RETURNING id INTO entry;
    IF entry IS NULL THEN RETURN false; END IF;
    IF cardinality(buckets) IS DISTINCT FROM cardinality(amounts) OR cardinality(buckets)<2
    THEN RAISE EXCEPTION 'invalid journal lines'; END IF;
    FOR pos IN 1..cardinality(buckets) LOOP
        IF amounts[pos]=0 THEN CONTINUE; END IF;
        holder := CASE WHEN buckets[pos] IN ('pending','available','held','reserved','withdrawn') THEN owner ELSE NULL END;
        IF amounts[pos]<0 AND holder IS NOT NULL AND finance_balance(holder,code,buckets[pos]) < -amounts[pos]
        THEN RAISE EXCEPTION 'insufficient seller funds' USING ERRCODE='P7001'; END IF;
        INSERT INTO ledger_accounts(owner_id,denomination,bucket) VALUES(holder,code,buckets[pos])
        ON CONFLICT DO NOTHING;
        SELECT id INTO STRICT account FROM ledger_accounts
        WHERE owner_id IS NOT DISTINCT FROM holder AND denomination=code AND bucket=buckets[pos];
        INSERT INTO journal_lines(entry_id,account_id,units) VALUES(entry,account,amounts[pos]);
    END LOOP;
    UPDATE journal_entries SET sealed=true WHERE id=entry;
    RETURN true;
END;
$$;
CREATE FUNCTION finance_sale(subject uuid, seller uuid, code text, gross bigint, test_units bigint) RETURNS void LANGUAGE plpgsql AS $$
DECLARE fee bigint := (gross*5+50)/100;
BEGIN
    PERFORM finance_post('sale:'||subject,subject,seller,code,
        ARRAY['sale_clearing','pending','commission'],ARRAY[-gross,gross-fee,fee]);
    PERFORM finance_post('payment-test:'||subject,subject,NULL,'TEST',
        ARRAY['test_treasury','test_provider'],ARRAY[test_units,-test_units]);
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER journal_entry_guard BEFORE INSERT OR UPDATE OR DELETE ON journal_entries
    FOR EACH ROW EXECUTE FUNCTION guard_journal_entry();
CREATE CONSTRAINT TRIGGER journal_entry_sealed AFTER INSERT ON journal_entries
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_sealed_journal();
CREATE TRIGGER journal_line_insertion BEFORE INSERT ON journal_lines
    FOR EACH ROW EXECUTE FUNCTION guard_journal_line();
CREATE TRIGGER journal_line_immutable BEFORE UPDATE OR DELETE ON journal_lines
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();
CREATE TRIGGER ledger_account_immutable BEFORE UPDATE OR DELETE ON ledger_accounts
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();

CREATE TABLE seller_payouts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seller_id uuid NOT NULL REFERENCES users(id),
    request_key bytea NOT NULL CHECK (octet_length(request_key)=32),
    currency text NOT NULL CHECK (currency IN ('USD','EUR')),
    amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 100000000000),
    test_units bigint NOT NULL CHECK (test_units > 0),
    state text NOT NULL CHECK (state IN ('reserved','under_review','approved','denied','processing','confirmed','reconciled','failed','stalled')),
    scenario text NOT NULL CHECK (scenario IN ('success','fail_once','stall_once')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(seller_id,request_key)
);
CREATE INDEX payout_seller_idx ON seller_payouts(seller_id,created_at DESC,id);
CREATE INDEX payout_queue_idx ON seller_payouts(state,created_at,id);
CREATE TABLE payout_reviews (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payout_id uuid NOT NULL REFERENCES seller_payouts(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    action text NOT NULL,
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER payout_reviews_immutable BEFORE UPDATE OR DELETE ON payout_reviews
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();
CREATE TABLE reconciliation_items (
    payout_id uuid PRIMARY KEY REFERENCES seller_payouts(id),
    provider_ref text NOT NULL UNIQUE,
    settled_units bigint NOT NULL CHECK (settled_units>0),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER reconciliation_items_immutable BEFORE UPDATE OR DELETE ON reconciliation_items
    FOR EACH ROW EXECUTE FUNCTION reject_order_history_change();

-- Replay previously verified M5 orders into the same journal contract.
-- +goose StatementBegin
DO $$
DECLARE sale record;
DECLARE fee bigint;
DECLARE net bigint;
BEGIN
    FOR sale IN
        SELECT o.id,o.seller_id,o.state,i.currency,i.price_minor,p.units
        FROM orders o JOIN order_items i ON i.order_id=o.id
        JOIN LATERAL (SELECT units FROM payment_observations WHERE order_id=o.id ORDER BY created_at,event_key LIMIT 1) p ON true
        WHERE o.state IN ('paid','fulfillment_pending','fulfilled','completed','fulfillment_failed','disputed','under_review','refunded')
        ORDER BY o.created_at,o.id
    LOOP
        PERFORM finance_sale(sale.id,sale.seller_id,sale.currency,sale.price_minor,sale.units);
        fee := (sale.price_minor*5+50)/100;
        net := sale.price_minor-fee;
        IF sale.state='completed' THEN
            PERFORM finance_post('release:'||sale.id,sale.id,sale.seller_id,sale.currency,
                ARRAY['pending','available'],ARRAY[-net,net]);
        ELSIF sale.state IN ('fulfillment_failed','disputed','under_review') THEN
            PERFORM finance_post('hold:'||sale.id,sale.id,sale.seller_id,sale.currency,
                ARRAY['pending','held'],ARRAY[-net,net]);
        ELSIF sale.state='refunded' THEN
            PERFORM finance_post('refund:'||sale.id,sale.id,sale.seller_id,sale.currency,
                ARRAY['sale_clearing','pending','commission'],ARRAY[sale.price_minor,-net,-fee]);
            PERFORM finance_post('refund-test:'||sale.id,sale.id,NULL,'TEST',
                ARRAY['test_treasury','test_provider'],ARRAY[-sale.units,sale.units]);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM journal_entries) OR EXISTS(SELECT 1 FROM seller_payouts)
    THEN RAISE EXCEPTION 'cannot roll back populated financial history'; END IF;
END;
$$;
-- +goose StatementEnd
DROP TABLE reconciliation_items;
DROP TABLE payout_reviews;
DROP TABLE seller_payouts;
DROP TABLE journal_lines;
DROP TABLE journal_entries;
DROP TABLE ledger_accounts;
DROP FUNCTION finance_sale(uuid,uuid,text,bigint,bigint);
DROP FUNCTION finance_post(text,uuid,uuid,text,text[],bigint[]);
DROP FUNCTION finance_balance(uuid,text,text);
DROP FUNCTION finance_lock(uuid);
DROP FUNCTION require_sealed_journal();
DROP FUNCTION guard_journal_line();
DROP FUNCTION guard_journal_entry();
