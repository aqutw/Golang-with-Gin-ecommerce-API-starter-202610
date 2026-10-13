package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/ecommerce/commerce/application/checkout"
	"example.com/ecommerce/commerce/domain/fault"
	"example.com/ecommerce/commerce/domain/order"
	"example.com/ecommerce/commerce/ports"
)

func initialized(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func service(s *Store, counter *atomic.Int64) checkout.Service {
	return checkout.Service{Store: s, NewID: func() string { return fmt.Sprint(counter.Add(1)) }, Now: func() time.Time { return time.Unix(100, 123).UTC() }}
}

func scalar(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSeedAndRestartPreserveProductsOrdersAndReceipts(t *testing.T) {
	// Exercise URI escaping as well as nested directory creation.
	path := filepath.Join(t.TempDir(), "nested", "shop?#.db")
	s := initialized(t, path)
	var ids atomic.Int64
	app := service(s, &ids)
	cmd := checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 1}}, Coupon: "WELCOME10"}
	first, err := app.Place(context.Background(), "alice", "request-001", cmd)
	if err != nil {
		t.Fatal(err)
	}
	if first.Order.TotalMinor != 7250 || first.Order.Lines[0].UnitPriceMinor != 7500 {
		t.Fatalf("%+v", first)
	}
	if _, err := s.db.Exec("UPDATE products SET price_minor = 9900, name = 'Updated name' WHERE id = 'keyboard'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = initialized(t, path) // Migrate + seed run again, just as API startup does.
	if err := s.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scalar(t, s, "SELECT count(*) FROM products") != 2 || scalar(t, s, "SELECT count(*) FROM schema_migrations") != 2 {
		t.Fatal("seed or migrations duplicated rows")
	}
	if scalar(t, s, "SELECT price_minor FROM products WHERE id = 'keyboard'") != 9900 || scalar(t, s, "SELECT available FROM inventory WHERE product_id = 'keyboard'") != 9 {
		t.Fatal("seed reset changed prices or stock")
	}
	app = service(s, &ids)
	replay, err := app.Place(context.Background(), "alice", "request-001", cmd)
	if err != nil || !replay.Replayed || replay.Order.ID != first.Order.ID {
		t.Fatalf("%+v %v", replay, err)
	}
	if replay.Order.TotalMinor != 7250 || replay.Order.Lines[0].Name != "Mechanical keyboard" || !replay.Order.CreatedAt.Equal(first.Order.CreatedAt) {
		t.Fatal("order snapshot changed after restart")
	}
	if _, err := app.GetOrder(context.Background(), "bob", first.Order.ID); !errors.Is(err, fault.NotFound) {
		t.Fatal(err)
	}
	cmd.Coupon = ""
	if _, err := app.Place(context.Background(), "alice", "request-001", cmd); !errors.Is(err, fault.IdempotencyConflict) {
		t.Fatal(err)
	}
	if scalar(t, s, "SELECT count(*) FROM orders") != 1 || scalar(t, s, "SELECT count(*) FROM outbox") != 1 || scalar(t, s, "SELECT count(*) FROM checkout_receipts") != 1 {
		t.Fatal("replay produced duplicate effects")
	}
}

func TestSQLRollbackAfterPartialStockDeduction(t *testing.T) {
	s := initialized(t, filepath.Join(t.TempDir(), "shop.db"))
	if _, err := s.db.Exec("UPDATE inventory SET available = 0 WHERE product_id = 'mouse'"); err != nil {
		t.Fatal(err)
	}
	var ids atomic.Int64
	app := service(s, &ids)
	_, err := app.Place(context.Background(), "alice", "request-001", checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 1}, {ProductID: "mouse", Quantity: 1}}})
	if !errors.Is(err, fault.OutOfStock) {
		t.Fatal(err)
	}
	if scalar(t, s, "SELECT available FROM inventory WHERE product_id = 'keyboard'") != 10 || scalar(t, s, "SELECT count(*) FROM orders") != 0 || scalar(t, s, "SELECT count(*) FROM checkout_receipts") != 0 || scalar(t, s, "SELECT count(*) FROM outbox") != 0 {
		t.Fatal("partial transaction committed")
	}
}

func TestOutboxFailureRollsBackOrderReceiptAndStock(t *testing.T) {
	s := initialized(t, filepath.Join(t.TempDir(), "shop.db"))
	var ids atomic.Int64
	app := service(s, &ids)
	cmd := checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 1}}}
	if _, err := app.Place(context.Background(), "alice", "request-001", cmd); err != nil {
		t.Fatal(err)
	}
	// IDs 1/2 were the first order/event. Force the next event to collide with 2,
	// after the new order and receipt have already been inserted.
	call := 0
	app.NewID = func() string {
		call++
		if call == 1 {
			return "new-order"
		}
		return "2"
	}
	if _, err := app.Place(context.Background(), "alice", "request-002", cmd); err == nil {
		t.Fatal("expected unique event ID failure")
	}
	if scalar(t, s, "SELECT available FROM inventory WHERE product_id = 'keyboard'") != 9 || scalar(t, s, "SELECT count(*) FROM orders") != 1 || scalar(t, s, "SELECT count(*) FROM checkout_receipts") != 1 || scalar(t, s, "SELECT count(*) FROM outbox") != 1 {
		t.Fatal("outbox failure partially committed")
	}
}

func TestConcurrentStoresDoNotOversell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shop.db")
	a := initialized(t, path)
	b := initialized(t, path)
	if _, err := a.db.Exec("UPDATE inventory SET available = 1 WHERE product_id = 'keyboard'"); err != nil {
		t.Fatal(err)
	}
	var ids atomic.Int64
	apps := []checkout.Service{service(a, &ids), service(b, &ids)}
	var wg sync.WaitGroup
	var created atomic.Int64
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := apps[i%2].Place(context.Background(), "alice", fmt.Sprintf("request-%03d", i), checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 1}}})
			if err == nil {
				created.Add(1)
			} else if !errors.Is(err, fault.OutOfStock) {
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if created.Load() != 1 || scalar(t, a, "SELECT available FROM inventory WHERE product_id = 'keyboard'") != 0 || scalar(t, a, "SELECT count(*) FROM orders") != 1 {
		t.Fatal("oversold across connections")
	}
}

func TestConcurrentSameKeyAcrossStoresCreatesOneOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shop.db")
	a := initialized(t, path)
	b := initialized(t, path)
	var ids atomic.Int64
	apps := []checkout.Service{service(a, &ids), service(b, &ids)}
	var wg sync.WaitGroup
	var created atomic.Int64
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := apps[i%2].Place(context.Background(), "alice", "same-request", checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 1}}})
			if err != nil {
				t.Error(err)
				return
			}
			if !r.Replayed {
				created.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if created.Load() != 1 || scalar(t, a, "SELECT available FROM inventory WHERE product_id = 'keyboard'") != 9 || scalar(t, a, "SELECT count(*) FROM checkout_receipts") != 1 || scalar(t, a, "SELECT count(*) FROM outbox") != 1 {
		t.Fatal("duplicate effects across connections")
	}
}

func TestCancellationAndPanicRollback(t *testing.T) {
	s := initialized(t, filepath.Join(t.TempDir(), "shop.db"))
	ctx, cancel := context.WithCancel(context.Background())
	err := s.Update(ctx, func(tx ports.Tx) error {
		if err := tx.DeductStock("keyboard", 1); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		_ = s.Update(context.Background(), func(tx ports.Tx) error {
			if err := tx.DeductStock("keyboard", 1); err != nil {
				t.Fatal(err)
			}
			panic("test")
		})
	}()
	if scalar(t, s, "SELECT available FROM inventory WHERE product_id = 'keyboard'") != 10 {
		t.Fatal("cancellation or panic committed stock")
	}
}

func TestForeignKeysAndReadErrors(t *testing.T) {
	s := initialized(t, filepath.Join(t.TempDir(), "shop.db"))
	if scalar(t, s, "PRAGMA foreign_keys") != 1 {
		t.Fatal("foreign keys disabled")
	}
	err := s.Update(context.Background(), func(tx ports.Tx) error {
		return tx.AppendEvent(order.PlacedEvent{ID: "test", OrderID: "missing", Type: "order.placed.v1", OccurredAt: time.Now()})
	})
	if err == nil {
		t.Fatal("foreign key violation accepted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.View(context.Background(), func(r ports.Reader) error { _, err := r.Products(); return err }); err == nil {
		t.Fatal("database failure hidden as empty catalog")
	}
}
