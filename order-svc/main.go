package main

import (
	"strconv"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/clients/inventory"
	"github.com/zopping/order-svc/common/caller"
	orderHTTP "github.com/zopping/order-svc/http/order"
	"github.com/zopping/order-svc/migrations"
	orderService "github.com/zopping/order-svc/services/order"
	orderStore "github.com/zopping/order-svc/stores/order"
)

const inventoryServiceName = "inventory-svc"

func main() {
	app := gofr.New()

	app.Migrate(migrations.All())

	// The gateway forwards the verified user as headers; this makes them available to the services. See common/caller.
	app.UseMiddleware(caller.Middleware())

	// Orders read prices from inventory-svc and reserve, confirm and release stock through it.
	app.AddHTTPService(inventoryServiceName, app.Config.GetOrDefault("INVENTORY_SERVICE_URL", "http://localhost:8082"))

	ttl, err := strconv.Atoi(app.Config.GetOrDefault("RESERVATION_TTL_SECONDS", "86400"))
	if err != nil || !orderService.ValidTTL(ttl) {
		app.Logger().Fatal("RESERVATION_TTL_SECONDS must be between 1 and 86400")
	}

	// Stores -> services -> handlers.
	orders := orderHTTP.New(orderService.New(orderStore.New(), inventory.New(inventoryServiceName), ttl))

	// Authn/authz is enforced by the gateway; this service trusts its callers.
	app.POST("/order", orders.Create)
	app.GET("/order", orders.Index)
	app.GET("/order/{id}", orders.Read)
	app.GET("/order/{id}/log", orders.Logs)
	// Status changes are PUTs: GoFr answers a data-bearing POST with 201.
	app.PUT("/order/{id}/status", orders.UpdateStatus)
	app.PUT("/order/{id}/cancel", orders.Cancel)
	app.PUT("/order/{id}/payment-status", orders.PaymentStatus)

	app.Run()
}
