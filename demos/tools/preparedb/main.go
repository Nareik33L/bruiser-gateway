package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	admin := os.Getenv("POSTGRES_ADMIN_URL")
	if admin == "" {
		admin = "postgres://bruiser:bruiser@127.0.0.1:5432/postgres?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "preparedb: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)
	for _, name := range []string{"bruiser", "harchester", "simtix"} {
		var exists int
		err := conn.QueryRow(ctx, `select 1 from pg_database where datname=$1`, name).Scan(&exists)
		if err == nil {
			continue
		}
		if _, err := conn.Exec(ctx, `create database `+name); err != nil {
			fmt.Fprintf(os.Stderr, "create %s: %v\n", name, err)
			os.Exit(1)
		}
		fmt.Println("created", name)
	}
}
