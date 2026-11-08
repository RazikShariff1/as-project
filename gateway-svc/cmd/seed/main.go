// Command seed registers the platform's endpoints and roles in account-svc. It is safe to run again.
//
//	ADMIN_EMAIL=admin@example.com ADMIN_PASSWORD=... ACCOUNT_SVC_URL=http://localhost:8083 go run ./cmd/seed
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/zopping/gateway-svc/seed"
)

func main() {
	cfg := seed.Config{
		AccountURL: env("ACCOUNT_SVC_URL", "http://localhost:8083"),
		Email:      os.Getenv("ADMIN_EMAIL"),
		Password:   os.Getenv("ADMIN_PASSWORD"),
	}

	if cfg.Email == "" || cfg.Password == "" {
		fmt.Fprintln(os.Stderr, "set ADMIN_EMAIL and ADMIN_PASSWORD to an account that may manage endpoints and roles")
		os.Exit(2)
	}

	sum, err := seed.Run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed failed:", err)
		os.Exit(1)
	}

	fmt.Printf("endpoints: %d registered, %d already there; roles: %d created, %d updated\n",
		sum.EndpointsCreated, sum.EndpointsExisting, sum.RolesCreated, sum.RolesUpdated)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}
