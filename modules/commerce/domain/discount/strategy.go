// Package discount owns ecommerce discount algorithms. Rule values are supplied
// by the application from storage, never fetched by a domain strategy.
package discount

import (
	"strings"

	"example.com/ecommerce/commerce/domain/fault"
)

type Kind string

const (
	Percentage  Kind = "percentage"
	FixedAmount Kind = "fixed_amount"
)

// A basis point is 0.01%; 1000 BPS means 10% off. Money uses minor units.
// MaxDiscountMinor == 0 means uncapped. A request can apply only one rule.
type Rule struct {
	Code             string `json:"code"`
	Strategy         Kind   `json:"strategy"`
	RateBPS          int64  `json:"rate_bps"`
	AmountMinor      int64  `json:"amount_minor"`
	MinSubtotalMinor int64  `json:"min_subtotal_minor"`
	MaxDiscountMinor int64  `json:"max_discount_minor"`
	Currency         string `json:"currency"`
	Active           bool   `json:"active"`
}

type Strategy interface {
	Amount(subtotalMinor int64, rule Rule) (int64, error)
}

type PercentageStrategy struct{}

func (PercentageStrategy) Amount(subtotal int64, rule Rule) (int64, error) {
	if subtotal < 0 || rule.RateBPS < 1 || rule.RateBPS > 10000 || rule.AmountMinor != 0 {
		return 0, fault.Invalid
	}
	// Decompose before multiplication so even MaxInt64 at 100% cannot overflow.
	// This is floor(subtotal * rate / 10000), with no floating-point rounding.
	return (subtotal/10000)*rule.RateBPS + ((subtotal%10000)*rule.RateBPS)/10000, nil
}

type FixedAmountStrategy struct{}

func (FixedAmountStrategy) Amount(subtotal int64, rule Rule) (int64, error) {
	if subtotal < 0 || rule.AmountMinor < 1 || rule.RateBPS != 0 || rule.MaxDiscountMinor != 0 {
		return 0, fault.Invalid
	}
	return min(subtotal, rule.AmountMinor), nil
}

// Engine dispatches by persisted strategy kind, not by coupon name. Construct
// the immutable registry once before serving; custom registries are copied.
type Engine struct{ strategies map[Kind]Strategy }

func NewEngine(strategies map[Kind]Strategy) Engine {
	if strategies == nil {
		strategies = map[Kind]Strategy{Percentage: PercentageStrategy{}, FixedAmount: FixedAmountStrategy{}}
	}
	registry := make(map[Kind]Strategy, len(strategies))
	for kind, strategy := range strategies {
		registry[kind] = strategy
	}
	return Engine{strategies: registry}
}

func (e Engine) Calculate(subtotal int64, currency string, rule *Rule) (int64, error) {
	if subtotal < 0 {
		return 0, fault.Invalid
	}
	if rule == nil {
		return 0, nil
	}
	if !rule.Active || rule.Code == "" || rule.Code != strings.ToUpper(strings.TrimSpace(rule.Code)) ||
		rule.Currency != currency || rule.MinSubtotalMinor < 0 || rule.MaxDiscountMinor < 0 || subtotal < rule.MinSubtotalMinor {
		return 0, fault.Invalid
	}
	if e.strategies == nil {
		e = NewEngine(nil)
	}
	strategy, ok := e.strategies[rule.Strategy]
	if !ok || strategy == nil {
		return 0, fault.Invalid
	}
	amount, err := strategy.Amount(subtotal, *rule)
	if err != nil {
		return 0, err
	}
	if rule.MaxDiscountMinor > 0 {
		amount = min(amount, rule.MaxDiscountMinor)
	}
	if amount < 0 || amount > subtotal {
		return 0, fault.Invalid
	}
	return amount, nil
}
