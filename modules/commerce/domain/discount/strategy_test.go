package discount_test

import (
	"math"
	"math/big"
	"testing"

	"example.com/ecommerce/commerce/domain/discount"
)

func TestDiscountStrategies(t *testing.T) {
	percentage := discount.Rule{Code: "PERCENT", Strategy: discount.Percentage, RateBPS: 1000, Currency: "USD", Active: true}
	fixed := discount.Rule{Code: "FIXED", Strategy: discount.FixedAmount, AmountMinor: 500, Currency: "USD", Active: true}
	for _, tc := range []struct {
		name     string
		subtotal int64
		rule     discount.Rule
		want     int64
		invalid  bool
	}{
		{"percent", 7500, percentage, 750, false},
		{"round-down", 101, percentage, 10, false},
		{"fixed", 7500, fixed, 500, false},
		{"fixed-cannot-exceed-subtotal", 200, fixed, 200, false},
		{"zero-subtotal", 0, percentage, 0, false},
		{"threshold-met", 5000, discount.Rule{Code: "MIN", Strategy: discount.FixedAmount, AmountMinor: 500, MinSubtotalMinor: 5000, Currency: "USD", Active: true}, 500, false},
		{"below-threshold", 4999, discount.Rule{Code: "MIN", Strategy: discount.FixedAmount, AmountMinor: 500, MinSubtotalMinor: 5000, Currency: "USD", Active: true}, 0, true},
		{"percentage-cap", 25000, discount.Rule{Code: "CAP", Strategy: discount.Percentage, RateBPS: 2000, MaxDiscountMinor: 3000, Currency: "USD", Active: true}, 3000, false},
		{"inactive", 1000, discount.Rule{Code: "OFF", Strategy: discount.Percentage, RateBPS: 1000, Currency: "USD"}, 0, true},
		{"wrong-currency", 1000, discount.Rule{Code: "EUR", Strategy: discount.Percentage, RateBPS: 1000, Currency: "EUR", Active: true}, 0, true},
		{"unknown-strategy", 1000, discount.Rule{Code: "UNKNOWN", Strategy: "unknown", Currency: "USD", Active: true}, 0, true},
		{"excessive-rate", 1000, discount.Rule{Code: "BAD", Strategy: discount.Percentage, RateBPS: 10001, Currency: "USD", Active: true}, 0, true},
		{"mixed-parameters", 1000, discount.Rule{Code: "BAD", Strategy: discount.Percentage, RateBPS: 1000, AmountMinor: 500, Currency: "USD", Active: true}, 0, true},
		{"negative-fixed", 1000, discount.Rule{Code: "BAD", Strategy: discount.FixedAmount, AmountMinor: -1, Currency: "USD", Active: true}, 0, true},
		{"negative-subtotal", -1, percentage, 0, true},
		{"negative-cap", 1000, discount.Rule{Code: "BAD", Strategy: discount.Percentage, RateBPS: 1000, MaxDiscountMinor: -1, Currency: "USD", Active: true}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			amount, err := discount.NewEngine(nil).Calculate(tc.subtotal, "USD", &tc.rule)
			if (err != nil) != tc.invalid || (!tc.invalid && amount != tc.want) {
				t.Fatalf("amount=%d want=%d err=%v", amount, tc.want, err)
			}
		})
	}
}

func TestPercentageAvoidsOverflow(t *testing.T) {
	for _, subtotal := range []int64{9999, 10000, 10001, math.MaxInt64} {
		for _, rate := range []int64{1, 999, 3333, 9999, 10000} {
			rule := discount.Rule{Code: "LARGE", Strategy: discount.Percentage, RateBPS: rate, Currency: "USD", Active: true}
			amount, err := discount.NewEngine(nil).Calculate(subtotal, "USD", &rule)
			if err != nil {
				t.Fatal(err)
			}
			// Use an independent arbitrary-precision oracle for rounding/overflow.
			want := new(big.Int).Mul(big.NewInt(subtotal), big.NewInt(rate))
			want.Quo(want, big.NewInt(10000))
			if amount != want.Int64() {
				t.Fatalf("subtotal=%d rate=%d got=%d want=%s", subtotal, rate, amount, want)
			}
		}
	}
}

type customStrategy struct{}

func (customStrategy) Amount(subtotal int64, rule discount.Rule) (int64, error) {
	return rule.AmountMinor, nil
}

func TestRegistryIsCopiedAndExtensible(t *testing.T) {
	registry := map[discount.Kind]discount.Strategy{"custom": customStrategy{}}
	engine := discount.NewEngine(registry)
	delete(registry, "custom")
	rule := discount.Rule{Code: "CUSTOM", Strategy: "custom", AmountMinor: 123, Currency: "USD", Active: true}
	amount, err := engine.Calculate(1000, "USD", &rule)
	if err != nil || amount != 123 {
		t.Fatalf("%d %v", amount, err)
	}
	rule.AmountMinor = 1001
	if _, err := engine.Calculate(1000, "USD", &rule); err == nil {
		t.Fatal("custom strategy exceeded subtotal")
	}
}
