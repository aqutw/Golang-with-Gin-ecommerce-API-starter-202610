package checkout_test

import (
	"context"
	"errors"
	"example.com/ecommerce/commerce/adapters/memory"
	"example.com/ecommerce/commerce/application/checkout"
	"example.com/ecommerce/commerce/domain/catalog"
	"example.com/ecommerce/commerce/domain/fault"
	"example.com/ecommerce/commerce/domain/order"
	"example.com/ecommerce/commerce/ports"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixture() (checkout.Service, *memory.Store) {
	store := memory.New([]catalog.Product{
		{ID: "a", Name: "A", PriceMinor: 7500, Currency: "USD", Active: true},
		{ID: "b", Name: "B", PriceMinor: 2500, Currency: "USD", Active: true},
	}, map[string]int{"a": 1, "b": 0})
	var counter atomic.Int64
	return checkout.Service{Store: store, NewID: func() string { return fmt.Sprint(counter.Add(1)) }, Now: func() time.Time { return time.Unix(100, 0) }}, store
}

func TestIdempotencyAndOwnership(t *testing.T) {
	s, store := fixture()
	ctx := context.Background()
	cmd := checkout.Command{Items: []checkout.Item{{ProductID: "a", Quantity: 1}}, Coupon: "welcome10"}
	first, err := s.Place(ctx, "alice", "request-001", cmd)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Coupon = " WELCOME10 "
	replay, err := s.Place(ctx, "alice", "request-001", cmd)
	if err != nil || !replay.Replayed || first.Order.ID != replay.Order.ID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	cmd.Coupon = ""
	if _, err := s.Place(ctx, "alice", "request-001", cmd); !errors.Is(err, fault.IdempotencyConflict) {
		t.Fatal(err)
	}
	if _, err := s.GetOrder(ctx, "bob", first.Order.ID); !errors.Is(err, fault.NotFound) {
		t.Fatal(err)
	}
	first.Order.Lines[0].Name = "mutated"
	saved, err := s.GetOrder(ctx, "alice", first.Order.ID)
	if err != nil || saved.Lines[0].Name != "A" {
		t.Fatalf("snapshot mutated: %+v %v", saved, err)
	}
	stock, orders, events := store.Snapshot()
	if stock["a"] != 0 || orders != 1 || events != 1 {
		t.Fatalf("%v %d %d", stock, orders, events)
	}
}

func TestRollbackAfterPartialStockDeduction(t *testing.T) {
	s, store := fixture()
	cmd := checkout.Command{Items: []checkout.Item{{ProductID: "a", Quantity: 1}, {ProductID: "b", Quantity: 1}}}
	if _, err := s.Place(context.Background(), "alice", "request-001", cmd); !errors.Is(err, fault.OutOfStock) {
		t.Fatal(err)
	}
	stock, orders, events := store.Snapshot()
	if stock["a"] != 1 || orders != 0 || events != 0 {
		t.Fatalf("partial commit: %v %d %d", stock, orders, events)
	}
	err := store.View(context.Background(), func(r ports.Reader) error {
		_, ok, err := r.Receipt("alice", "request-001")
		if err != nil {
			return err
		}
		if ok {
			t.Fatal("failed checkout saved receipt")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentCheckoutDoesNotOversell(t *testing.T) {
	s, store := fixture()
	var wg sync.WaitGroup
	var successful atomic.Int64
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Place(context.Background(), "alice", fmt.Sprintf("request-%03d", i), checkout.Command{Items: []checkout.Item{{ProductID: "a", Quantity: 1}}})
			if err == nil {
				successful.Add(1)
			} else if !errors.Is(err, fault.OutOfStock) {
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}
	wg.Wait()
	stock, orders, events := store.Snapshot()
	if successful.Load() != 1 || stock["a"] != 0 || orders != 1 || events != 1 {
		t.Fatalf("oversold: %d %v %d %d", successful.Load(), stock, orders, events)
	}
}

func TestConcurrentSameKeyCreatesOneOrder(t *testing.T) {
	s, store := fixture()
	var wg sync.WaitGroup
	var created atomic.Int64
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Place(context.Background(), "alice", "same-request", checkout.Command{Items: []checkout.Item{{ProductID: "a", Quantity: 1}}})
			if err != nil {
				t.Error(err)
				return
			}
			if !r.Replayed {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	_, orders, events := store.Snapshot()
	if created.Load() != 1 || orders != 1 || events != 1 {
		t.Fatalf("duplicate: %d %d %d", created.Load(), orders, events)
	}
}

func TestCancellationRollsBackOrderAndOutbox(t *testing.T) {
	_, store := fixture()
	ctx, cancel := context.WithCancel(context.Background())
	err := store.Update(ctx, func(tx ports.Tx) error {
		if err := tx.DeductStock("a", 1); err != nil {
			return err
		}
		if err := tx.SaveOrder(order.Order{ID: "test"}); err != nil {
			return err
		}
		if err := tx.AppendEvent(order.PlacedEvent{ID: "event"}); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	stock, orders, events := store.Snapshot()
	if stock["a"] != 1 || orders != 0 || events != 0 {
		t.Fatal("canceled transaction committed")
	}
}

func TestDuplicateItemsAreAggregated(t *testing.T) {
	s, _ := fixture()
	quote, err := s.Quote(context.Background(), checkout.Command{Items: []checkout.Item{{ProductID: "a", Quantity: 1}, {ProductID: "a", Quantity: 1}}})
	if err != nil || quote.SubtotalMinor != 15000 || quote.ShippingMinor != 0 {
		t.Fatalf("%+v %v", quote, err)
	}
	_, err = s.Quote(context.Background(), checkout.Command{Items: []checkout.Item{{ProductID: "a", Quantity: 100}, {ProductID: "a", Quantity: 1}}})
	if !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
