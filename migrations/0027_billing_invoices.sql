CREATE TABLE billing_invoices (
    id UUID PRIMARY KEY,
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    usage_snapshot_id UUID NOT NULL REFERENCES usage_snapshots(id) ON DELETE RESTRICT,
    number TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    currency TEXT NOT NULL DEFAULT 'USD',
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    due_at TIMESTAMPTZ NOT NULL,
    subtotal_cents BIGINT NOT NULL DEFAULT 0,
    total_cents BIGINT NOT NULL DEFAULT 0,
    line_items JSONB NOT NULL DEFAULT '[]'::jsonb,
    metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT billing_invoices_amounts_check CHECK (subtotal_cents >= 0 AND total_cents >= 0),
    CONSTRAINT billing_invoices_status_check CHECK (status IN ('draft', 'open', 'void', 'paid')),
    CONSTRAINT billing_invoices_currency_check CHECK (char_length(currency) = 3),
    UNIQUE (org_id, number)
);

CREATE INDEX billing_invoices_org_id_created_at_idx ON billing_invoices(org_id, created_at DESC);
CREATE INDEX billing_invoices_usage_snapshot_id_idx ON billing_invoices(usage_snapshot_id);
