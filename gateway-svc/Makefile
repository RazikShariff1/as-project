GOFR_ENV ?= local

.PHONY: run build test seed lint

run:
	GOFR_ENV=$(GOFR_ENV) go run main.go

build:
	go build -o bin/gateway-svc .

test:
	go test ./...

# Registers the endpoint catalogue and the admin/seller/catalogue-manager roles in account-svc. Safe to repeat: it
# replaces each role's grants. Needs ADMIN_EMAIL, ADMIN_PASSWORD and ACCOUNT_SVC_URL (default http://localhost:8083).
seed:
	GOFR_ENV=$(GOFR_ENV) go run ./cmd/seed

lint:
	golangci-lint run --timeout 10m
