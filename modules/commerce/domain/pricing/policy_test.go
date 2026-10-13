package pricing_test

import (
	"example.com/ecommerce/commerce/domain/order"
	"example.com/ecommerce/commerce/domain/pricing"
	"math"
	"testing"
)

func TestPricingBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name            string
		price           int64
		quantity        int
		coupon          string
		total, shipping int64
		invalid         bool
	}{
		{"below-free-shipping", 9999, 1, "", 10499, 500, false},
		{"free-shipping", 10000, 1, "", 10000, 0, false},
		{"shipping-after-discount", 10000, 1, "WELCOME10", 9500, 500, false},
		{"fraction-rounds-down", 101, 1, "WELCOME10", 591, 500, false},
		{"unknown-coupon", 100, 1, "BAD", 0, 0, true},
		{"overflow", math.MaxInt64, 2, "", 0, 0, true},
		{"negative", -1, 1, "", 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := (pricing.Policy{}).Calculate([]order.Line{{Quantity: tc.quantity, UnitPriceMinor: tc.price}}, "USD", tc.coupon)
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.invalid && (v.TotalMinor != tc.total || v.ShippingMinor != tc.shipping) {
				t.Fatalf("%+v", v)
			}
		})
	}
}
