package main

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/clients/inventory"
	"github.com/zopping/catalogue-svc/common/caller"
	"github.com/zopping/catalogue-svc/common/events"
	sellerChangeSubscriber "github.com/zopping/catalogue-svc/eventhandler/sellerproductchange"
	permissibleHTTP "github.com/zopping/catalogue-svc/http/permissible"
	sellerChangeHTTP "github.com/zopping/catalogue-svc/http/sellerchange"
	sellerRequestHTTP "github.com/zopping/catalogue-svc/http/sellerrequest"
	permissibleService "github.com/zopping/catalogue-svc/services/permissible"
	sellerChangeService "github.com/zopping/catalogue-svc/services/sellerchange"
	sellerRequestService "github.com/zopping/catalogue-svc/services/sellerrequest"
	permissibleStore "github.com/zopping/catalogue-svc/stores/permissible"
	sellerRequestStore "github.com/zopping/catalogue-svc/stores/sellerrequest"

	brandHTTP "github.com/zopping/catalogue-svc/http/brand"
	categoryHTTP "github.com/zopping/catalogue-svc/http/category"
	productHTTP "github.com/zopping/catalogue-svc/http/product"
	sellerHTTP "github.com/zopping/catalogue-svc/http/seller"
	variantHTTP "github.com/zopping/catalogue-svc/http/variant"
	"github.com/zopping/catalogue-svc/migrations"
	brandService "github.com/zopping/catalogue-svc/services/brand"
	categoryService "github.com/zopping/catalogue-svc/services/category"
	productService "github.com/zopping/catalogue-svc/services/product"
	sellerService "github.com/zopping/catalogue-svc/services/seller"
	variantService "github.com/zopping/catalogue-svc/services/variant"
	brandStore "github.com/zopping/catalogue-svc/stores/brand"
	categoryStore "github.com/zopping/catalogue-svc/stores/category"
	productStore "github.com/zopping/catalogue-svc/stores/product"
	sellerStore "github.com/zopping/catalogue-svc/stores/seller"
	variantStore "github.com/zopping/catalogue-svc/stores/variant"
)

const inventoryServiceName = "inventory-svc"

func main() {
	app := gofr.New()

	app.Migrate(migrations.All())

	// The gateway forwards the verified seller as a header; this makes it available to the services. See common/caller.
	app.UseMiddleware(caller.Middleware())

	// inventory-svc is called when a seller product request is approved. The change event carries only request ids.
	app.AddHTTPService(inventoryServiceName, app.Config.GetOrDefault("INVENTORY_SERVICE_URL", "http://localhost:8082"))

	changeTopic := app.Config.GetOrDefault("SELLER_PRODUCT_CHANGE_TOPIC", "seller-product-change")

	// Stores -> services -> handlers.
	sellers := sellerStore.New()
	brands := brandStore.New()
	categories := categoryStore.New()
	products := productStore.New()
	variants := variantStore.New()

	sellerHandler := sellerHTTP.New(sellerService.New(sellers))
	brandHandler := brandHTTP.New(brandService.New(brands))
	categoryHandler := categoryHTTP.New(categoryService.New(categories))
	permissibleStr, requestStr := permissibleStore.New(), sellerRequestStore.New()
	permissibleHandler := permissibleHTTP.New(permissibleService.New(permissibleStr, products))
	changeService := sellerChangeService.New(requestStr, inventory.New(inventoryServiceName))

	// SELLER_CHANGE_DELIVERY=direct applies approvals in-process, with no broker (e.g. a single-container deploy).
	// Anything else (the default) publishes to Pub/Sub, which this service also subscribes to.
	direct := app.Config.GetOrDefault("SELLER_CHANGE_DELIVERY", "pubsub") == "direct"

	var publisher events.Publisher = events.GoFr{}
	if direct {
		publisher = events.NewDirect(changeService.Process)
	}

	sellerRequestHandler := sellerRequestHTTP.New(sellerRequestService.New(
		requestStr, permissibleStr, sellers, products, variants, publisher, changeTopic))
	sellerChange := sellerChangeSubscriber.New(changeService)
	reconcileHandler := sellerChangeHTTP.New(changeService)

	productHandler := productHTTP.New(productService.New(products, variants))
	variantHandler := variantHTTP.New(variantService.New(variants))

	// Authn/authz is enforced by the gateway; this service trusts its callers.
	app.GET("/seller", sellerHandler.Index)
	app.GET("/seller/{id}", sellerHandler.Read)
	app.POST("/seller", sellerHandler.Create)
	app.PUT("/seller/{id}", sellerHandler.Update)

	app.GET("/brand", brandHandler.Index)
	app.GET("/brand/{id}", brandHandler.Read)
	app.POST("/brand", brandHandler.Create)
	app.PUT("/brand/{id}", brandHandler.Update)
	app.DELETE("/brand/{id}", brandHandler.Delete)

	app.GET("/category", categoryHandler.Index)
	app.GET("/category/{id}", categoryHandler.Read)
	app.POST("/category", categoryHandler.Create)
	app.PUT("/category/{id}", categoryHandler.Update)
	app.DELETE("/category/{id}", categoryHandler.Delete)

	app.GET("/product", productHandler.Index)
	app.GET("/product/{id}", productHandler.Read)
	app.POST("/product", productHandler.Create)
	app.PUT("/product/{id}", productHandler.Update)
	app.DELETE("/product/{id}", productHandler.Delete)

	app.GET("/variant", variantHandler.Index)
	app.GET("/variant/{id}", variantHandler.Read)
	app.POST("/variant", variantHandler.Create)
	app.PUT("/variant/{id}", variantHandler.Update)
	app.DELETE("/variant/{id}", variantHandler.Delete)

	// Which sellers may sell which product (admin), and the request workflow: file, approve or reject, show or hide.
	app.PUT("/product/{id}/permissible-seller", permissibleHandler.Replace)
	app.GET("/permissible-seller", permissibleHandler.Index)

	// Static before {id}: a scheduler calls this to apply approvals that never reached inventory-svc.
	app.PUT("/seller-product-request/reconcile", reconcileHandler.Reconcile)

	app.GET("/seller-product-request", sellerRequestHandler.Index)
	app.POST("/seller-product-request", sellerRequestHandler.Create)
	app.GET("/seller-product-request/{id}", sellerRequestHandler.Read)
	app.PUT("/seller-product-request/{id}", sellerRequestHandler.Update)
	app.DELETE("/seller-product-request/{id}", sellerRequestHandler.Delete)
	app.PUT("/seller-product-request/{id}/approval", sellerRequestHandler.Approval)
	app.PUT("/seller-product-request/{id}/visibility", sellerRequestHandler.Visibility)

	// Approved requests are pushed to inventory-svc by this subscriber, as catalogue-service does.
	if !direct {
		app.Subscribe(changeTopic, sellerChange.Handle)
	}

	app.Run()
}
