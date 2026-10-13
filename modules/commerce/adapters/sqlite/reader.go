package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"example.com/ecommerce/commerce/domain/catalog"
	"example.com/ecommerce/commerce/domain/fault"
	"example.com/ecommerce/commerce/domain/order"
	"example.com/ecommerce/commerce/ports"
)

// View exposes only reader; callers cannot accidentally acquire write methods.
type reader struct {
	ctx context.Context
	tx  *sql.Tx
}

func (r *reader) Products() ([]catalog.Product, error) {
	rows, err := r.tx.QueryContext(r.ctx, "SELECT id, name, price_minor, currency, active FROM products ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	products := make([]catalog.Product, 0)
	for rows.Next() {
		var p catalog.Product
		if err := rows.Scan(&p.ID, &p.Name, &p.PriceMinor, &p.Currency, &p.Active); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

func (r *reader) Product(id string) (catalog.Product, error) {
	var p catalog.Product
	err := r.tx.QueryRowContext(r.ctx, "SELECT id, name, price_minor, currency, active FROM products WHERE id = ?", id).Scan(&p.ID, &p.Name, &p.PriceMinor, &p.Currency, &p.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return catalog.Product{}, fault.NotFound
	}
	return p, err
}

func (r *reader) Order(id string) (order.Order, error) {
	var o order.Order
	var createdAt string
	err := r.tx.QueryRowContext(r.ctx, `SELECT id, customer_id, currency, subtotal_minor, discount_minor, shipping_minor, total_minor, status, created_at FROM orders WHERE id = ?`, id).
		Scan(&o.ID, &o.CustomerID, &o.Currency, &o.SubtotalMinor, &o.DiscountMinor, &o.ShippingMinor, &o.TotalMinor, &o.Status, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return order.Order{}, fault.NotFound
	}
	if err != nil {
		return order.Order{}, err
	}
	o.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return order.Order{}, err
	}
	rows, err := r.tx.QueryContext(r.ctx, `SELECT product_id, name, quantity, unit_price_minor FROM order_lines WHERE order_id = ? ORDER BY line_number`, id)
	if err != nil {
		return order.Order{}, err
	}
	defer rows.Close()
	o.Lines = make([]order.Line, 0)
	for rows.Next() {
		var line order.Line
		if err := rows.Scan(&line.ProductID, &line.Name, &line.Quantity, &line.UnitPriceMinor); err != nil {
			return order.Order{}, err
		}
		o.Lines = append(o.Lines, line)
	}
	return o, rows.Err()
}

func (r *reader) Receipt(customer, key string) (ports.Receipt, bool, error) {
	var receipt ports.Receipt
	err := r.tx.QueryRowContext(r.ctx, "SELECT fingerprint, order_id FROM checkout_receipts WHERE customer_id = ? AND idempotency_key = ?", customer, key).Scan(&receipt.Fingerprint, &receipt.OrderID)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.Receipt{}, false, nil
	}
	return receipt, err == nil, err
}
