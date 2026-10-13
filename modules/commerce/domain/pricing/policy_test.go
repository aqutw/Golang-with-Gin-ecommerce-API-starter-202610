package pricing_test

import (
	"example.com/ecommerce/commerce/domain/discount"
	"example.com/ecommerce/commerce/domain/order"
	"example.com/ecommerce/commerce/domain/pricing"
	"math"
	"testing"
)

func TestPricingBoundaries(t *testing.T) {
	percentage := &discount.Rule{Code: "WELCOME10", Strategy: discount.Percentage, RateBPS: 1000, Currency: "USD", Active: true}
	unknown := &discount.Rule{Code: "BAD", Strategy: "unknown", Currency: "USD", Active: true}
	for _, tc := range []struct {
		name            string
		price           int64
		quantity        int
		rule            *discount.Rule
		total, shipping int64
		invalid         bool
	}{
		{"below-free-shipping", 9999, 1, nil, 10499, 500, false},
		{"free-shipping", 10000, 1, nil, 10000, 0, false},
		{"shipping-after-discount", 10000, 1, percentage, 9500, 500, false},
		{"fraction-rounds-down", 101, 1, percentage, 591, 500, false},
		{"unknown-strategy", 100, 1, unknown, 0, 0, true},
		{"overflow", math.MaxInt64, 2, nil, 0, 0, true},
		{"negative", -1, 1, nil, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := (pricing.Policy{}).Calculate([]order.Line{{Quantity: tc.quantity, UnitPriceMinor: tc.price}}, "USD", tc.rule)
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.invalid && (v.TotalMinor != tc.total || v.ShippingMinor != tc.shipping) {
				t.Fatalf("%+v", v)
			}
		})
	}
}
