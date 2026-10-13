CREATE TABLE products (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    price_minor INTEGER NOT NULL CHECK (price_minor >= 0),
    currency TEXT NOT NULL CHECK (length(currency) = 3),
    active INTEGER NOT NULL CHECK (active IN (0, 1))
);

CREATE TABLE inventory (
    product_id TEXT PRIMARY KEY REFERENCES products(id),
    available INTEGER NOT NULL CHECK (available >= 0)
);

CREATE TABLE orders (
    id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL,
    currency TEXT NOT NULL,
    subtotal_minor INTEGER NOT NULL CHECK (subtotal_minor >= 0),
    discount_minor INTEGER NOT NULL CHECK (discount_minor >= 0 AND discount_minor <= subtotal_minor),
    shipping_minor INTEGER NOT NULL CHECK (shipping_minor >= 0),
    total_minor INTEGER NOT NULL CHECK (total_minor >= 0 AND total_minor = subtotal_minor - discount_minor + shipping_minor),
    status TEXT NOT NULL CHECK (status IN ('pending_payment', 'paid', 'cancelled', 'expired')),
    created_at TEXT NOT NULL
);
CREATE INDEX orders_customer_id ON orders(customer_id);

CREATE TABLE order_lines (
    order_id TEXT NOT NULL REFERENCES orders(id),
    line_number INTEGER NOT NULL CHECK (line_number >= 0),
    product_id TEXT NOT NULL REFERENCES products(id),
    name TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity BETWEEN 1 AND 100),
    unit_price_minor INTEGER NOT NULL CHECK (unit_price_minor >= 0),
    PRIMARY KEY (order_id, line_number)
);

CREATE TABLE checkout_receipts (
    customer_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    order_id TEXT NOT NULL REFERENCES orders(id),
    PRIMARY KEY (customer_id, idempotency_key)
);

CREATE TABLE outbox (
    id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    order_id TEXT NOT NULL REFERENCES orders(id),
    occurred_at TEXT NOT NULL,
    payload TEXT NOT NULL,
    published_at TEXT
);
CREATE INDEX outbox_pending ON outbox(occurred_at) WHERE published_at IS NULL;
