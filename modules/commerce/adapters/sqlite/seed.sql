-- Insert missing demo rows only. Never reset stock, product prices, or orders.
INSERT INTO products (id, name, price_minor, currency, active) VALUES
    ('keyboard', 'Mechanical keyboard', 7500, 'USD', 1),
    ('mouse', 'Wireless mouse', 2500, 'USD', 1)
ON CONFLICT(id) DO NOTHING;

INSERT INTO inventory (product_id, available) VALUES
    ('keyboard', 10),
    ('mouse', 20)
ON CONFLICT(product_id) DO NOTHING;

-- All business-configurable discount numbers live in this table.
-- 1000 BPS = 10%; amounts / thresholds / caps are USD cents, 0 cap = uncapped.
INSERT INTO discount_rules (code, strategy, rate_bps, amount_minor, min_subtotal_minor, max_discount_minor, currency, active) VALUES
    ('WELCOME10', 'percentage', 1000, 0, 0, 0, 'USD', 1),
    ('SAVE500', 'fixed_amount', 0, 500, 5000, 0, 'USD', 1),
    ('BIGSALE20', 'percentage', 2000, 0, 10000, 3000, 'USD', 1)
ON CONFLICT(code) DO NOTHING;
