package ports

import (
	"context"
	"example.com/ecommerce/commerce/domain/catalog"
	"example.com/ecommerce/commerce/domain/order"
)

type Receipt struct {
	Fingerprint string
	OrderID     string
}

type Reader interface {
	Products() ([]catalog.Product, error)
	Product(id string) (catalog.Product, error)
	Order(id string) (order.Order, error)
	Receipt(customer, key string) (Receipt, bool, error)
}

type Tx interface {
	Reader
	DeductStock(productID string, quantity int) error
	SaveOrder(order.Order) error
	SaveReceipt(customer, key string, receipt Receipt) error
	AppendEvent(order.PlacedEvent) error
}

// Update must commit stock, order, receipt, and outbox atomically, roll back on
// error, and serialize conflicting stock / idempotency writes. Callbacks must
// not perform network IO or retain the supplied Reader/Tx after returning.
type Store interface {
	View(context.Context, func(Reader) error) error
	Update(context.Context, func(Tx) error) error
}
