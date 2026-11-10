package main

import (
	"strconv"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/common/authctx"
	"github.com/zopping/customer-svc/common/password"
	addressHTTP "github.com/zopping/customer-svc/http/address"
	authHTTP "github.com/zopping/customer-svc/http/auth"
	customerHTTP "github.com/zopping/customer-svc/http/customer"
	"github.com/zopping/customer-svc/migrations"
	addressService "github.com/zopping/customer-svc/services/address"
	authService "github.com/zopping/customer-svc/services/auth"
	customerService "github.com/zopping/customer-svc/services/customer"
	addressStore "github.com/zopping/customer-svc/stores/address"
	customerStore "github.com/zopping/customer-svc/stores/customer"
	sessionStore "github.com/zopping/customer-svc/stores/session"
)

const minutesPerSecond = 60

func main() {
	app := gofr.New()

	app.Migrate(migrations.All())
	app.UseMiddleware(authctx.Middleware())

	minutes, err := strconv.Atoi(app.Config.GetOrDefault("SESSION_TTL_MINUTES", "10080"))
	if err != nil || minutes < 1 {
		app.Logger().Fatal("SESSION_TTL_MINUTES must be a positive number of minutes")
	}

	// Stores -> services -> handlers.
	customers, sessions, addresses := customerStore.New(), sessionStore.New(), addressStore.New()
	hasher := password.New(password.DefaultCost)

	auth, err := authService.New(customers, sessions, hasher, minutes*minutesPerSecond)
	if err != nil {
		app.Logger().Fatalf("cannot start auth service: %v", err)
	}

	authHandler := authHTTP.New(auth)
	customerHandler := customerHTTP.New(auth, customerService.New(customers, hasher))
	addressHandler := addressHTTP.New(auth, addressService.New(addresses))

	// Authenticated by the bearer token itself, which the gateway forwards for these routes only. They take no
	// customer id, so a caller can only ever reach their own data. Register is a POST, so GoFr answers it 201.
	app.POST("/auth/register", authHandler.Register)
	app.POST("/auth/login", authHandler.Login)
	app.GET("/auth/me", authHandler.Me)
	app.DELETE("/auth/session", authHandler.Logout)

	// /customer/me must be registered before /customer/{id} so "me" is not read as an id.
	app.GET("/customer/me", customerHandler.Me)
	app.PUT("/customer/me", customerHandler.UpdateMe)
	app.PUT("/customer/me/password", customerHandler.ChangeMyPassword)
	app.GET("/customer/me/address", addressHandler.Index)
	app.POST("/customer/me/address", addressHandler.Create)
	app.GET("/customer/me/address/{id}", addressHandler.Read)
	app.PUT("/customer/me/address/{id}", addressHandler.Update)
	app.DELETE("/customer/me/address/{id}", addressHandler.Delete)

	// Staff routes: reached through the team gateway, which authorises them by role. Never expose them to customers.
	app.GET("/customer", customerHandler.Index)
	app.GET("/customer/{id}", customerHandler.Read)
	app.PUT("/customer/{id}/status", customerHandler.UpdateStatus)

	app.Run()
}
