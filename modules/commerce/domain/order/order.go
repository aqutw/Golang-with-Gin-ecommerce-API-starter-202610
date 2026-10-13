package order

import (
	"example.com/ecommerce/commerce/domain/discount"
	"time"
)

type Line struct {
	ProductID      string `json:"product_id"`
	Name           string `json:"name"`
	Quantity       int    `json:"quantity"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
}

type Order struct {
	ID            string         `json:"id"`
	CustomerID    string         `json:"customer_id"`
	Lines         []Line         `json:"lines"`
	Currency      string         `json:"currency"`
	SubtotalMinor int64          `json:"subtotal_minor"`
	DiscountMinor int64          `json:"discount_minor"`
	Discount      *discount.Rule `json:"discount,omitempty"`
	ShippingMinor int64          `json:"shipping_minor"`
	TotalMinor    int64          `json:"total_minor"`
	Status        string         `json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
}

// Copy protects committed snapshots from a caller mutating a returned slice.
func (o Order) Copy() Order {
	o.Lines = append([]Line(nil), o.Lines...)
	if o.Discount != nil {
		snapshot := *o.Discount
		o.Discount = &snapshot
	}
	return o
}

type PlacedEvent struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	OrderID    string    `json:"order_id"`
	OccurredAt time.Time `json:"occurred_at"`
}
