// Command db applies schema and demo seed without starting HTTP or requiring auth.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"example.com/ecommerce/commerce/adapters/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	defaultPath := os.Getenv("DB_PATH")
	if defaultPath == "" {
		defaultPath = "./data/ecommerce.db"
	}
	path := flag.String("db", defaultPath, "SQLite database file")
	flag.Parse()
	if flag.NArg() != 1 || (flag.Arg(0) != "migrate" && flag.Arg(0) != "seed") {
		return fmt.Errorf("usage: db [-db path] migrate|seed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := sqlite.Open(ctx, *path)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return err
	}
	if flag.Arg(0) == "seed" {
		if err := store.Seed(ctx); err != nil {
			return err
		}
	}
	fmt.Printf("SQLite %s completed: %s\n", flag.Arg(0), *path)
	return nil
}
