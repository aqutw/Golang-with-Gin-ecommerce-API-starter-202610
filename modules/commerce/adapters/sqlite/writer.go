package sqlite

import (
	"encoding/json"
	"time"

	"example.com/ecommerce/commerce/domain/fault"
	"example.com/ecommerce/commerce/domain/order"
	"example.com/ecommerce/commerce/ports"
)

type writer struct{ reader }

func (w *writer) DeductStock(id string, quantity int) error {
	if quantity < 1 {
		return fault.Invalid
	}
	result, err := w.tx.ExecContext(w.ctx, `UPDATE inventory SET available = available - ? WHERE product_id = ? AND available >= ?`, quantity, id, quantity)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 1 {
		return nil
	}
	if _, err := w.Product(id); err != nil {
		return err
	}
	return fault.OutOfStock
}

func (w *writer) SaveOrder(o order.Order) error {
	var snapshot string
	if o.Discount != nil {
		encoded, err := json.Marshal(o.Discount)
		if err != nil {
			return err
		}
		snapshot = string(encoded)
	}
	_, err := w.tx.ExecContext(w.ctx, `INSERT INTO orders (id, customer_id, currency, subtotal_minor, discount_minor, shipping_minor, total_minor, status, created_at, discount_snapshot) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.ID, o.CustomerID, o.Currency, o.SubtotalMinor, o.DiscountMinor, o.ShippingMinor, o.TotalMinor, o.Status, o.CreatedAt.UTC().Format(time.RFC3339Nano), snapshot)
	if err != nil {
		return err
	}
	for i, line := range o.Lines {
		_, err := w.tx.ExecContext(w.ctx, `INSERT INTO order_lines (order_id, line_number, product_id, name, quantity, unit_price_minor) VALUES (?, ?, ?, ?, ?, ?)`, o.ID, i, line.ProductID, line.Name, line.Quantity, line.UnitPriceMinor)
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) SaveReceipt(customer, key string, receipt ports.Receipt) error {
	_, err := w.tx.ExecContext(w.ctx, `INSERT INTO checkout_receipts (customer_id, idempotency_key, fingerprint, order_id) VALUES (?, ?, ?, ?)`, customer, key, receipt.Fingerprint, receipt.OrderID)
	return err
}

func (w *writer) AppendEvent(event order.PlacedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = w.tx.ExecContext(w.ctx, `INSERT INTO outbox (id, event_type, order_id, occurred_at, payload) VALUES (?, ?, ?, ?, ?)`, event.ID, event.Type, event.OrderID, event.OccurredAt.UTC().Format(time.RFC3339Nano), string(payload))
	return err
}
