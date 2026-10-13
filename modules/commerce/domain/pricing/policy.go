package pricing

import (
	"example.com/ecommerce/commerce/domain/discount"
	"example.com/ecommerce/commerce/domain/fault"
	"example.com/ecommerce/commerce/domain/order"
	"math"
)

type Totals struct {
	Currency      string         `json:"currency"`
	SubtotalMinor int64          `json:"subtotal_minor"`
	DiscountMinor int64          `json:"discount_minor"`
	Discount      *discount.Rule `json:"discount,omitempty"`
	ShippingMinor int64          `json:"shipping_minor"`
	TotalMinor    int64          `json:"total_minor"`
}

// Policy is ecommerce-specific, not a generic utility. Thresholds use USD cents.
// Tax is intentionally absent; add a jurisdiction policy before real selling.
type Policy struct{ Discounts discount.Engine }

func (p Policy) Calculate(lines []order.Line, currency string, rule *discount.Rule) (Totals, error) {
	if len(lines) == 0 || currency != "USD" {
		return Totals{}, fault.Invalid
	}
	t := Totals{Currency: currency}
	for _, line := range lines {
		if line.Quantity < 1 || line.Quantity > 100 || line.UnitPriceMinor < 0 || line.UnitPriceMinor > math.MaxInt64/int64(line.Quantity) {
			return Totals{}, fault.Invalid
		}
		amount := line.UnitPriceMinor * int64(line.Quantity)
		if t.SubtotalMinor > math.MaxInt64-amount {
			return Totals{}, fault.Invalid
		}
		t.SubtotalMinor += amount
	}
	amount, err := p.Discounts.Calculate(t.SubtotalMinor, currency, rule)
	if err != nil {
		return Totals{}, err
	}
	t.DiscountMinor = amount
	if rule != nil {
		snapshot := *rule
		t.Discount = &snapshot
	}
	net := t.SubtotalMinor - t.DiscountMinor
	if net < 10000 {
		t.ShippingMinor = 500
	}
	if net > math.MaxInt64-t.ShippingMinor {
		return Totals{}, fault.Invalid
	}
	t.TotalMinor = net + t.ShippingMinor
	return t, nil
}
