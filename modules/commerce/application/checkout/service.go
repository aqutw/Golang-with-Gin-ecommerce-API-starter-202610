package checkout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"example.com/ecommerce/commerce/domain/discount"
	"example.com/ecommerce/commerce/domain/fault"
	"example.com/ecommerce/commerce/domain/order"
	"example.com/ecommerce/commerce/domain/pricing"
	"example.com/ecommerce/commerce/ports"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Item struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}
type Command struct {
	Items  []Item `json:"items"`
	Coupon string `json:"coupon"`
}
type Result struct {
	Order    order.Order `json:"order"`
	Replayed bool        `json:"replayed"`
}
type Service struct {
	Store   ports.Store
	Pricing pricing.Policy
	NewID   func() string
	Now     func() time.Time
}

func normalize(cmd Command) (Command, error) {
	if len(cmd.Items) == 0 || len(cmd.Items) > 50 {
		return Command{}, fault.Invalid
	}
	counts := map[string]int{}
	for _, item := range cmd.Items {
		if item.ProductID == "" || len(item.ProductID) > 64 || item.Quantity < 1 || item.Quantity > 100 {
			return Command{}, fault.Invalid
		}
		counts[item.ProductID] += item.Quantity
		if counts[item.ProductID] > 100 {
			return Command{}, fault.Invalid
		}
	}
	cmd.Items = make([]Item, 0, len(counts))
	for id, quantity := range counts {
		cmd.Items = append(cmd.Items, Item{id, quantity})
	}
	sort.Slice(cmd.Items, func(i, j int) bool { return cmd.Items[i].ProductID < cmd.Items[j].ProductID })
	cmd.Coupon = strings.ToUpper(strings.TrimSpace(cmd.Coupon))
	if len(cmd.Coupon) > 64 {
		return Command{}, fault.Invalid
	}
	return cmd, nil
}

func (s Service) quote(r ports.Reader, cmd Command) ([]order.Line, pricing.Totals, error) {
	lines := make([]order.Line, 0, len(cmd.Items))
	for _, item := range cmd.Items {
		p, err := r.Product(item.ProductID)
		if err != nil {
			return nil, pricing.Totals{}, err
		}
		if !p.Active || p.Currency != "USD" {
			return nil, pricing.Totals{}, fault.Invalid
		}
		lines = append(lines, order.Line{ProductID: p.ID, Name: p.Name, Quantity: item.Quantity, UnitPriceMinor: p.PriceMinor})
	}
	var rule *discount.Rule
	if cmd.Coupon != "" {
		stored, err := r.Discount(cmd.Coupon)
		if errors.Is(err, fault.NotFound) {
			return nil, pricing.Totals{}, fault.Invalid
		}
		if err != nil {
			return nil, pricing.Totals{}, err
		}
		rule = &stored
	}
	totals, err := s.Pricing.Calculate(lines, "USD", rule)
	return lines, totals, err
}

func (s Service) Quote(ctx context.Context, cmd Command) (pricing.Totals, error) {
	cmd, err := normalize(cmd)
	if err != nil {
		return pricing.Totals{}, err
	}
	var totals pricing.Totals
	err = s.Store.View(ctx, func(r ports.Reader) error {
		_, t, err := s.quote(r, cmd)
		totals = t
		return err
	})
	return totals, err
}

func (s Service) Place(ctx context.Context, customer, key string, cmd Command) (Result, error) {
	if customer == "" || len(key) < 8 || len(key) > 128 || strings.IndexFunc(key, unicode.IsSpace) >= 0 {
		return Result{}, fault.Invalid
	}
	cmd, err := normalize(cmd)
	if err != nil {
		return Result{}, err
	}
	encoded, err := json.Marshal(cmd)
	if err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(sum[:])
	var result Result
	err = s.Store.Update(ctx, func(tx ports.Tx) error {
		receipt, ok, err := tx.Receipt(customer, key)
		if err != nil {
			return err
		}
		if ok {
			if receipt.Fingerprint != fingerprint {
				return fault.IdempotencyConflict
			}
			o, err := tx.Order(receipt.OrderID)
			result = Result{Order: o, Replayed: true}
			return err
		}
		lines, totals, err := s.quote(tx, cmd)
		if err != nil {
			return err
		}
		for _, item := range cmd.Items {
			if err := tx.DeductStock(item.ProductID, item.Quantity); err != nil {
				return err
			}
		}
		o := order.Order{ID: s.NewID(), CustomerID: customer, Lines: lines, Currency: totals.Currency,
			SubtotalMinor: totals.SubtotalMinor, DiscountMinor: totals.DiscountMinor, Discount: totals.Discount, ShippingMinor: totals.ShippingMinor,
			TotalMinor: totals.TotalMinor, Status: "pending_payment", CreatedAt: s.Now().UTC()}
		if err := tx.SaveOrder(o); err != nil {
			return err
		}
		if err := tx.SaveReceipt(customer, key, ports.Receipt{Fingerprint: fingerprint, OrderID: o.ID}); err != nil {
			return err
		}
		if err := tx.AppendEvent(order.PlacedEvent{ID: s.NewID(), Type: "order.placed.v1", OrderID: o.ID, OccurredAt: o.CreatedAt}); err != nil {
			return err
		}
		result = Result{Order: o}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func (s Service) GetOrder(ctx context.Context, customer, id string) (order.Order, error) {
	var result order.Order
	err := s.Store.View(ctx, func(r ports.Reader) error {
		o, err := r.Order(id)
		if err != nil {
			return err
		}
		if o.CustomerID != customer {
			return fault.NotFound
		}
		result = o
		return nil
	})
	return result, err
}
