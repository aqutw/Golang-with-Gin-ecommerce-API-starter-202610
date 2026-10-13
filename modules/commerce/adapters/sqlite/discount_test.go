package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"example.com/ecommerce/commerce/application/checkout"
	"example.com/ecommerce/commerce/domain/fault"
)

func TestDiscountValuesComeFromSQLiteAndOrdersKeepSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discount.db")
	s := initialized(t, path)
	var ids atomic.Int64
	app := service(s, &ids)
	ctx := context.Background()
	cmd := checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 1}}, Coupon: "WELCOME10"}
	first, err := app.Place(ctx, "alice", "request-001", cmd)
	if err != nil || first.Order.DiscountMinor != 750 || first.Order.Discount == nil || first.Order.Discount.RateBPS != 1000 {
		t.Fatalf("%+v %v", first, err)
	}
	if _, err := s.db.Exec("UPDATE discount_rules SET rate_bps = 2500 WHERE code = 'WELCOME10'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := app.Quote(ctx, cmd)
	if err != nil || quote.DiscountMinor != 1875 || quote.Discount.RateBPS != 2500 {
		t.Fatalf("%+v %v", quote, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = initialized(t, path)
	app = service(s, &ids)
	if scalar(t, s, "SELECT rate_bps FROM discount_rules WHERE code = 'WELCOME10'") != 2500 || scalar(t, s, "SELECT count(*) FROM discount_rules") != 3 {
		t.Fatal("seed reset edited rule or created duplicate")
	}
	replay, err := app.Place(ctx, "alice", "request-001", cmd)
	if err != nil || !replay.Replayed || replay.Order.DiscountMinor != 750 || replay.Order.Discount.RateBPS != 1000 {
		t.Fatalf("%+v %v", replay, err)
	}
	second, err := app.Place(ctx, "alice", "request-002", cmd)
	if err != nil || second.Order.DiscountMinor != 1875 || second.Order.Discount.RateBPS != 2500 {
		t.Fatalf("%+v %v", second, err)
	}
}

func TestStoredFixedDiscountThresholdCapAndDisabledRules(t *testing.T) {
	s := initialized(t, filepath.Join(t.TempDir(), "discount.db"))
	var ids atomic.Int64
	app := service(s, &ids)
	ctx := context.Background()
	quote, err := app.Quote(ctx, checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 1}}, Coupon: "SAVE500"})
	if err != nil || quote.DiscountMinor != 500 {
		t.Fatalf("%+v %v", quote, err)
	}
	if _, err := s.db.Exec("UPDATE discount_rules SET amount_minor = 700, min_subtotal_minor = 7500 WHERE code = 'SAVE500'"); err != nil {
		t.Fatal(err)
	}
	quote, err = app.Quote(ctx, checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 1}}, Coupon: "SAVE500"})
	if err != nil || quote.DiscountMinor != 700 {
		t.Fatalf("%+v %v", quote, err)
	}
	quote, err = app.Quote(ctx, checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 3}}, Coupon: "BIGSALE20"})
	if err != nil || quote.DiscountMinor != 3000 {
		t.Fatalf("%+v %v", quote, err)
	}
	if _, err := s.db.Exec("UPDATE discount_rules SET max_discount_minor = 1000 WHERE code = 'BIGSALE20'"); err != nil {
		t.Fatal(err)
	}
	quote, err = app.Quote(ctx, checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 3}}, Coupon: "BIGSALE20"})
	if err != nil || quote.DiscountMinor != 1000 {
		t.Fatalf("%+v %v", quote, err)
	}
	for _, cmd := range []checkout.Command{
		{Items: []checkout.Item{{ProductID: "mouse", Quantity: 1}}, Coupon: "SAVE500"},
		{Items: []checkout.Item{{ProductID: "mouse", Quantity: 1}}, Coupon: "UNKNOWN"},
	} {
		if _, err := app.Quote(ctx, cmd); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("UPDATE discount_rules SET active = 0 WHERE code = 'SAVE500'"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Place(ctx, "alice", "request-001", checkout.Command{Items: []checkout.Item{{ProductID: "keyboard", Quantity: 1}}, Coupon: "SAVE500"}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if scalar(t, s, "SELECT available FROM inventory WHERE product_id='keyboard'") != 10 || scalar(t, s, "SELECT count(*) FROM orders") != 0 {
		t.Fatal("invalid discount consumed stock")
	}
	if _, err := s.db.Exec("UPDATE discount_rules SET rate_bps = 10001 WHERE code = 'WELCOME10'"); err == nil {
		t.Fatal("SQL accepted invalid percentage")
	}
}

func TestDiscountMigrationKeepsLegacyOrders(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	initial, err := files.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
		INSERT INTO schema_migrations VALUES (1, '2026-10-13T00:00:00Z');
		INSERT INTO orders (id, customer_id, currency, subtotal_minor, discount_minor, shipping_minor, total_minor, status, created_at)
		VALUES ('legacy-order', 'alice', 'USD', 10000, 1000, 500, 9500, 'pending_payment', '2026-10-13T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	var ids atomic.Int64
	o, err := service(s, &ids).GetOrder(context.Background(), "alice", "legacy-order")
	if err != nil || o.DiscountMinor != 1000 || o.TotalMinor != 9500 || o.Discount != nil {
		t.Fatalf("legacy order changed: %+v %v", o, err)
	}
	if err := s.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scalar(t, s, "SELECT count(*) FROM discount_rules") != 3 {
		t.Fatal("new rules not seeded")
	}
	if _, err := s.db.Exec("UPDATE discount_rules SET rate_bps = 1000.5 WHERE code = 'WELCOME10'"); err == nil {
		t.Fatal("non-integer BPS accepted")
	}
}
