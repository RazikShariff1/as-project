# One image, four services. Build context is the folder holding the four service directories.
FROM golang:1.25-alpine AS build
# go.mod asks for a newer Go than this image; let the toolchain fetch it.
ENV GOTOOLCHAIN=auto CGO_ENABLED=0
WORKDIR /src
COPY . .
RUN for s in gateway-svc account-svc catalogue-svc inventory-svc; do \
      (cd "$s" && go build -trimpath -ldflags="-s -w" -o "/out/$s/$s" .) || exit 1; \
      cp -r "$s/configs" "/out/$s/configs"; \
    done

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/ /app/
COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh && chown -R app /app
# Non-secret defaults, so the image works even if the platform only supplies secrets. Dashboard values override these.
ENV GOFR_ENV=production \
    TRUST_FORWARDED_FOR=true \
    SELLER_CHANGE_DELIVERY=direct \
    ACCOUNT_DB_NAME=neondb \
    CATALOGUE_DB_NAME=neondb \
    INVENTORY_DB_NAME=neondb
USER app
EXPOSE 10000
CMD ["/app/start.sh"]
