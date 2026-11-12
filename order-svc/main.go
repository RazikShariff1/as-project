package main

import (
	"strconv"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/clients/communication"
	"github.com/zopping/order-svc/clients/inventory"
	"github.com/zopping/order-svc/common/caller"
	orderHTTP "github.com/zopping/order-svc/http/order"
	orderLogHTTP "github.com/zopping/order-svc/http/orderlog"
	placeOrderHTTP "github.com/zopping/order-svc/http/placeorder"
	orderHandlers "github.com/zopping/order-svc/internal/orders/handlers"
	orderPatchService "github.com/zopping/order-svc/internal/orders/services"
	"github.com/zopping/order-svc/migrations"
	orderService "github.com/zopping/order-svc/services/order"
	orderLogService "github.com/zopping/order-svc/services/orderlog"
	placeOrderService "github.com/zopping/order-svc/services/placeorder"
	orderStore "github.com/zopping/order-svc/stores/order"
	orderLogStore "github.com/zopping/order-svc/stores/orderlog"
)

const (
	inventoryServiceName     = "inventory-svc"
	communicationServiceName = "communication-svc"
)

func main() {
	app := gofr.New()

	app.Migrate(migrations.All())

	// The gateway forwards the verified user as headers; this makes them available to the services. See common/caller.
	app.UseMiddleware(caller.Middleware())

	// Orders read prices from inventory-svc and reserve, confirm and release stock through it.
	app.AddHTTPService(inventoryServiceName, app.Config.GetOrDefault("INVENTORY_SERVICE_URL", "http://localhost:8082"))

	ttl, err := strconv.Atoi(app.Config.GetOrDefault("RESERVATION_TTL_SECONDS", "86400"))
	if err != nil || !placeOrderService.ValidTTL(ttl) {
		app.Logger().Fatal("RESERVATION_TTL_SECONDS must be between 1 and 86400")
	}

	// New orders are emailed to the owner (the admin) through communication-svc. Without both an address and the service URL, none are.
	var notifier communication.Notifier = communication.Nop{}

	if url, owner := app.Config.Get("COMMUNICATION_SERVICE_URL"), app.Config.Get("ADMIN_EMAIL"); url != "" && owner != "" {
		app.AddHTTPService(communicationServiceName, url)

		notifier = communication.New(communicationServiceName, owner)
	}

	// Stores -> services -> handlers.
	var (
		orderStr   = orderStore.New()
		inv        = inventory.New(inventoryServiceName)
		orderLogH  = orderLogHTTP.New(orderLogService.New(orderLogStore.New()))
		placeH     = placeOrderHTTP.New(placeOrderService.WithNotifier(placeOrderService.New(orderStr, inv, ttl), notifier))
		orderH     = orderHTTP.New(orderService.New(orderStr))
		orderPatch = orderHandlers.NewOrderHandler(orderPatchService.New(orderStr, inv, ttl))
	)

	// Authn/authz is enforced by the gateway; this service trusts its callers.
	app.POST("/order", placeH.Create)
	app.GET("/order", orderH.Index)
	app.GET("/order/{refNum}", orderH.Read)
	// One PATCH changes an order: status (cancel included), items, payment status, delivery details and metadata.
	app.PATCH("/order/{refNum}", orderPatch.Patch)
	// An order's history is a resource of its own, as in the reference: GET /order-log?orderReferenceNumber=.
	app.GET("/order-log", orderLogH.Index)
	app.POST("/order-log", orderLogH.Create)

	app.Run()
}
