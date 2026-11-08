package main

import (
	"strconv"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/inventory-svc/common/caller"
	reservationHTTP "github.com/zopping/inventory-svc/http/reservation"
	stockHTTP "github.com/zopping/inventory-svc/http/stock"
	"github.com/zopping/inventory-svc/migrations"
	reservationService "github.com/zopping/inventory-svc/services/reservation"
	stockService "github.com/zopping/inventory-svc/services/stock"
	reservationStore "github.com/zopping/inventory-svc/stores/reservation"
	stockStore "github.com/zopping/inventory-svc/stores/stock"
)

func main() {
	app := gofr.New()

	app.Migrate(migrations.All())

	// The gateway forwards the verified seller as a header; this makes it available to the handlers. See common/caller.
	app.UseMiddleware(caller.Middleware())

	// HOUSE_SELLER_ID is the seller that wins a price tie, so our own offer is shown first when prices match (0 = none).
	houseSellerID, err := strconv.ParseInt(app.Config.GetOrDefault("HOUSE_SELLER_ID", "0"), 10, 64)
	if err != nil || houseSellerID < 0 {
		app.Logger().Fatal("HOUSE_SELLER_ID must be a seller id, or 0 for none")
	}

	// Stores -> services -> handlers.
	stockHandler := stockHTTP.New(stockService.New(stockStore.New(), houseSellerID))
	reservationHandler := reservationHTTP.New(reservationService.New(reservationStore.New()))

	// Authn/authz is enforced by the gateway; this service trusts its callers.
	// {id} is the catalogue variant id and {sellerId} the seller. Static paths are registered before the {id} ones.
	app.GET("/stock", stockHandler.Index)
	app.GET("/stock/availability", stockHandler.Availability)
	app.GET("/stock/{id}/seller/{sellerId}", stockHandler.Read)
	app.PUT("/stock/{id}/seller/{sellerId}", stockHandler.Put)
	app.DELETE("/stock/{id}/seller/{sellerId}", stockHandler.Withdraw)
	app.PUT("/stock/{id}/seller/{sellerId}/offer", stockHandler.UpsertOffer)
	app.PUT("/stock/{id}/seller/{sellerId}/adjust", stockHandler.Adjust)
	app.PUT("/stock/{id}/seller/{sellerId}/price", stockHandler.SetPrice)
	app.GET("/stock/{id}/seller/{sellerId}/movement", stockHandler.Movements)
	app.GET("/stock/{id}/seller/{sellerId}/price-log", stockHandler.PriceLog)

	// Confirm, release and release-expired are PUTs: GoFr answers a data-bearing POST with 201.
	app.POST("/reservation", reservationHandler.Create)
	app.PUT("/reservation/release-expired", reservationHandler.ReleaseExpired)
	app.GET("/reservation/{orderRef}", reservationHandler.Read)
	app.PUT("/reservation/{orderRef}/confirm", reservationHandler.Confirm)
	app.PUT("/reservation/{orderRef}/release", reservationHandler.Release)

	app.Run()
}
