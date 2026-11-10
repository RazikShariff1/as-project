package main

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/gateway-svc/accountclient"
	"github.com/zopping/gateway-svc/config"
	"github.com/zopping/gateway-svc/customerclient"
	"github.com/zopping/gateway-svc/gateway"
)

func main() {
	app := gofr.New()

	settings, err := config.Load(app.Config)
	if err != nil {
		app.Logger().Fatalf("invalid configuration: %v", err)
	}

	// The middleware chain is the application: it serves everything under /api/ and leaves GoFr's own routes
	// (/.well-known/health and the like) alone.
	gw := gateway.New(settings, accountclient.New(settings.Services[config.AccountService])).
		WithCustomers(customerclient.New(settings.Services[config.CustomerService])).
		WithLogger(app.Logger())

	app.UseMiddleware(gw.Middleware())

	// GoFr starts its HTTP server only once a route is registered, and the middleware above handles every /api/ request before
	// routing, so this root route exists just to switch the server on (along with GoFr's /.well-known health routes).
	app.GET("/", func(*gofr.Context) (any, error) { return map[string]string{"name": "gateway-svc"}, nil })

	app.Run()
}
