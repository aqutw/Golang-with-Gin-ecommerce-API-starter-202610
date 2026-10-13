-- Insert missing demo rows only. Never reset stock, product prices, or orders.
INSERT INTO products (id, name, price_minor, currency, active) VALUES
    ('keyboard', 'Mechanical keyboard', 7500, 'USD', 1),
    ('mouse', 'Wireless mouse', 2500, 'USD', 1)
ON CONFLICT(id) DO NOTHING;

INSERT INTO inventory (product_id, available) VALUES
    ('keyboard', 10),
    ('mouse', 20)
ON CONFLICT(product_id) DO NOTHING;
