CREATE TABLE discount_rules (
    code TEXT PRIMARY KEY CHECK (length(code) BETWEEN 1 AND 64 AND code = upper(trim(code))),
    strategy TEXT NOT NULL CHECK (strategy IN ('percentage', 'fixed_amount')),
    rate_bps INTEGER NOT NULL DEFAULT 0 CHECK (rate_bps BETWEEN 0 AND 10000),
    amount_minor INTEGER NOT NULL DEFAULT 0 CHECK (amount_minor >= 0),
    min_subtotal_minor INTEGER NOT NULL DEFAULT 0 CHECK (min_subtotal_minor >= 0),
    max_discount_minor INTEGER NOT NULL DEFAULT 0 CHECK (max_discount_minor >= 0),
    currency TEXT NOT NULL CHECK (length(currency) = 3),
    active INTEGER NOT NULL CHECK (active IN (0, 1)),
    CHECK ((strategy = 'percentage' AND rate_bps > 0 AND amount_minor = 0)
        OR (strategy = 'fixed_amount' AND amount_minor > 0 AND rate_bps = 0 AND max_discount_minor = 0))
) STRICT;

-- Existing orders keep their original totals, with no invented rule snapshot.
ALTER TABLE orders ADD COLUMN discount_snapshot TEXT NOT NULL DEFAULT '';
