GOFR_ENV ?= local
MOCKGEN  = go run github.com/golang/mock/mockgen@v1.6.0

.PHONY: create-db run build test lint generate-mocks

# One-off: create this service's database in the shared local Postgres (container postgres-root).
create-db:
	docker exec postgres-root psql -U root -d root -c "CREATE DATABASE orders"

# Migrations run automatically on startup (GoFr app.Migrate).
run:
	GOFR_ENV=$(GOFR_ENV) go run main.go

build:
	go build -o bin/order-svc .

test:
	GOFR_ENV=test go test ./...

lint:
	golangci-lint run --timeout 10m

generate-mocks:
	$(MOCKGEN) -source=stores/interfaces.go -destination=stores/mock_interfaces.go -package=stores
	$(MOCKGEN) -source=services/interfaces.go -destination=services/mock_interfaces.go -package=services
	$(MOCKGEN) -source=clients/inventory/inventory.go -destination=clients/inventory/mock_inventory.go -package=inventory
