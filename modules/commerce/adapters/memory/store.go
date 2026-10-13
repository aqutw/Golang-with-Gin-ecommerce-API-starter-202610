// Package memory is a single-process demo adapter, not durable persistence.
package memory

import (
	"context"
	"example.com/ecommerce/commerce/domain/catalog"
	"example.com/ecommerce/commerce/domain/fault"
	"example.com/ecommerce/commerce/domain/inventory"
	"example.com/ecommerce/commerce/domain/order"
	"example.com/ecommerce/commerce/ports"
	"fmt"
	"sort"
	"sync"
)

type receiptKey struct{ customer, key string }
type state struct {
	products map[string]catalog.Product
	stock    map[string]int
	orders   map[string]order.Order
	receipts map[receiptKey]ports.Receipt
	events   []order.PlacedEvent
}
type Store struct {
	mu   sync.RWMutex
	data *state
}

var _ ports.Store = (*Store)(nil)

func New(products []catalog.Product, stock map[string]int) *Store {
	d := &state{products: map[string]catalog.Product{}, stock: map[string]int{}, orders: map[string]order.Order{}, receipts: map[receiptKey]ports.Receipt{}}
	for _, p := range products {
		d.products[p.ID] = p
	}
	for id, amount := range stock {
		d.stock[id] = amount
	}
	return &Store{data: d}
}

func (d *state) clone() *state {
	products, _ := d.Products() // The memory implementation cannot fail.
	c := New(products, d.stock).data
	for id, o := range d.orders {
		c.orders[id] = o.Copy()
	}
	for key, receipt := range d.receipts {
		c.receipts[key] = receipt
	}
	c.events = append([]order.PlacedEvent(nil), d.events...)
	return c
}

func (s *Store) View(ctx context.Context, fn func(ports.Reader) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(s.data)
}

func (s *Store) Update(ctx context.Context, fn func(ports.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	next := s.data.clone()
	if err := fn(next); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (d *state) Products() ([]catalog.Product, error) {
	products := make([]catalog.Product, 0, len(d.products))
	for _, p := range d.products {
		products = append(products, p)
	}
	sort.Slice(products, func(i, j int) bool { return products[i].ID < products[j].ID })
	return products, nil
}
func (d *state) Product(id string) (catalog.Product, error) {
	p, ok := d.products[id]
	if !ok {
		return catalog.Product{}, fault.NotFound
	}
	return p, nil
}
func (d *state) Order(id string) (order.Order, error) {
	o, ok := d.orders[id]
	if !ok {
		return order.Order{}, fault.NotFound
	}
	return o.Copy(), nil
}
func (d *state) Receipt(customer, key string) (ports.Receipt, bool, error) {
	r, ok := d.receipts[receiptKey{customer, key}]
	return r, ok, nil
}
func (d *state) DeductStock(id string, quantity int) error {
	stock, ok := d.stock[id]
	if !ok {
		return fault.NotFound
	}
	next, err := inventory.Deduct(stock, quantity)
	if err != nil {
		return err
	}
	d.stock[id] = next
	return nil
}
func (d *state) SaveOrder(o order.Order) error {
	if _, exists := d.orders[o.ID]; exists {
		return fmt.Errorf("duplicate order ID")
	}
	d.orders[o.ID] = o.Copy()
	return nil
}
func (d *state) SaveReceipt(customer, key string, receipt ports.Receipt) error {
	d.receipts[receiptKey{customer, key}] = receipt
	return nil
}
func (d *state) AppendEvent(event order.PlacedEvent) error {
	d.events = append(d.events, event)
	return nil
}

// Snapshot is for tests / local diagnostics, not part of the application port.
func (s *Store) Snapshot() (map[string]int, int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stock := map[string]int{}
	for id, n := range s.data.stock {
		stock[id] = n
	}
	return stock, len(s.data.orders), len(s.data.events)
}
