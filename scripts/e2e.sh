#!/usr/bin/env bash
# End-to-end check of the deployed platform through the gateway.
#
#   ADMIN_EMAIL=you@example.com ADMIN_PASSWORD='...' ./scripts/e2e.sh
#   BASE=https://other.onrender.com ... ./scripts/e2e.sh        # default BASE is the Render deploy
#   SKIP_SEED=1 ...                                               # roles were already seeded
#
# It creates test data (a seller, catalogue entries, stock, two customers, orders) with a unique suffix, so it can be re-run.
# Needs: bash, curl, jq, and Go (only for the one-time role seeding). Credentials come from the environment, never from this file.
set -uo pipefail

BASE="${BASE:-https://as-project-j88g.onrender.com}"
: "${ADMIN_EMAIL:?set ADMIN_EMAIL}"
: "${ADMIN_PASSWORD:?set ADMIN_PASSWORD}"
API="$BASE/api"
RUN="$(date +%s)"
FAILS=0
BODY=""

# call <expected-status> <METHOD> <path> [json] [token]   -> sets $BODY, prints PASS/FAIL
call() {
  local want="$1" method="$2" path="$3" data="${4:-}" token="${5:-}" code
  local args=(-s -m 90 -o /tmp/e2e.body -w '%{http_code}' -X "$method" -H 'Content-Type: application/json')
  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$data" ] && args+=(-d "$data")
  code=$(curl "${args[@]}" "$API$path")
  BODY="$(cat /tmp/e2e.body)"
  if [ "$code" = "$want" ]; then printf 'PASS %-6s %-58s %s\n' "$method" "$path" "$code"
  else printf 'FAIL %-6s %-58s got %s want %s\n     %s\n' "$method" "$path" "$code" "$want" "$(echo "$BODY" | head -c 300)"; FAILS=$((FAILS+1)); fi
}
j() { echo "$BODY" | jq -r "$1"; }
step() { printf '\n== %s\n' "$1"; }

step "Wake the services and log in as admin"
curl -s -m 120 -o /dev/null "$BASE/.well-known/alive"
call 200 POST /account-svc/auth/login "$(jq -n --arg e "$ADMIN_EMAIL" --arg p "$ADMIN_PASSWORD" '{email:$e,password:$p}')"
ADMIN=$(j .data.token)
[ "$ADMIN" != null ] && [ -n "$ADMIN" ] || { echo "cannot continue without an admin token"; exit 1; }
call 200 GET /account-svc/auth/me "" "$ADMIN"

if [ -z "${SKIP_SEED:-}" ]; then
  step "Seed endpoints and roles (one time; safe to repeat)"
  ( cd "$(dirname "$0")/../gateway-svc" && ACCOUNT_SVC_URL="$API/account-svc" ADMIN_EMAIL="$ADMIN_EMAIL" ADMIN_PASSWORD="$ADMIN_PASSWORD" go run ./cmd/seed ) \
    || { echo "FAIL seeding"; FAILS=$((FAILS+1)); }
fi
call 200 GET '/account-svc/role?limit=100' "" "$ADMIN"
CUSTOMER_ROLE=$(j '.data.roles[] | select(.name=="customer") | .id')
[ -n "$CUSTOMER_ROLE" ] || { echo "no 'customer' role: seeding did not run, or the gateway build is old"; exit 1; }

step "Catalogue: seller, brand, category, product, variant"
call 201 POST /catalogue-svc/seller "$(jq -n --arg n "E2E Seller $RUN" '{name:$n,status:"active"}')" "$ADMIN";           SELLER=$(j .data.id)
call 201 POST /catalogue-svc/brand "$(jq -n --arg n "E2E Brand $RUN" '{name:$n,status:"ENABLED"}')" "$ADMIN";           BRAND=$(j .data.id)
call 201 POST /catalogue-svc/category "$(jq -n --arg n "E2E Category $RUN" '{name:$n,status:"ENABLED"}')" "$ADMIN";    CAT=$(j .data.id)
call 201 POST /catalogue-svc/product "$(jq -n --arg n "E2E Rice $RUN" --argjson c "$CAT" --argjson b "$BRAND" \
  '{name:$n,primaryCategoryId:$c,categoryIds:[$c],brandId:$b,status:"ENABLED"}')" "$ADMIN";                            PRODUCT=$(j .data.id)
call 201 POST /catalogue-svc/variant "$(jq -n --argjson p "$PRODUCT" --arg n "1 kg $RUN" '{productId:$p,name:$n,status:"ENABLED"}')" "$ADMIN"; VARIANT=$(j .data.id)
call 200 GET "/catalogue-svc/product/$PRODUCT"
call 200 GET "/catalogue-svc/variant/$VARIANT"

step "Seller sells it: permit, request, approve (offer reaches inventory-svc)"
call 200 PUT "/catalogue-svc/product/$PRODUCT/permissible-seller" "$(jq -n --argjson s "$SELLER" '{sellerIds:[$s]}')" "$ADMIN"
call 201 POST /catalogue-svc/seller-product-request "$(jq -n --argjson s "$SELLER" --argjson p "$PRODUCT" --argjson v "$VARIANT" \
  '{sellerId:$s,productId:$p,status:"PENDING",items:[{variantId:$v,mrp:120,discount:20}]}')" "$ADMIN";                 REQ=$(j .data.id)
call 200 PUT "/catalogue-svc/seller-product-request/$REQ/approval" '{"status":"APPROVED"}' "$ADMIN"
call 200 GET "/inventory-svc/stock/$VARIANT/seller/$SELLER" "" "$ADMIN"
echo "   offer: status=$(j .data.status) sellingPrice=$(j .data.sellingPrice)  (expect ENABLED / 100)"

step "Inventory: stock the offer"
call 200 PUT "/inventory-svc/stock/$VARIANT/seller/$SELLER" '{"quantity":10,"unit":1,"notes":"e2e"}' "$ADMIN"
call 200 GET "/inventory-svc/stock/availability?variantIds=$VARIANT"
echo "   available=$(j '.data[0].bestOffer.available') (expect 10)"

step "Two customers"
for n in 1 2; do
  call 201 POST /account-svc/user "$(jq -n --arg e "e2e-$RUN-$n@example.com" --argjson r "$CUSTOMER_ROLE" \
    '{name:"E2E Customer",email:$e,phone:"9999999999",password:"E2e-Passw0rd!",status:"active",attributes:{},roleIds:[$r]}')" "$ADMIN"
  call 200 POST /account-svc/auth/login "$(jq -n --arg e "e2e-$RUN-$n@example.com" '{email:$e,password:"E2e-Passw0rd!"}')"
  eval "C$n=$(j .data.token)"
done

ADDR='{"name":"Asha","phone":"9999999999","line1":"1 Main St","city":"Pune","pincode":"411001"}'
ORDER_BODY=$(jq -n --argjson v "$VARIANT" --argjson s "$SELLER" --argjson a "$ADDR" '{items:[{variantId:$v,sellerId:$s,quantity:2}],deliveryAddress:$a}')

step "Orders: place, read, list (customer 1)"
call 201 POST /order-svc/order "$ORDER_BODY" "$C1"; O1=$(j .data.id)
echo "   total=$(j .data.total) status=$(j .data.status) payment=$(j .data.paymentStatus)  (expect 200 / PLACED / PENDING)"
call 200 GET "/order-svc/order/$O1" "" "$C1"
call 200 GET /order-svc/order "" "$C1"
call 200 GET "/order-svc/order/$O1/log" "" "$C1"
call 200 GET "/inventory-svc/stock/availability?variantIds=$VARIANT"
echo "   available=$(j '.data[0].bestOffer.available') (expect 8: two units are held)"

step "Isolation and rules"
call 404 GET "/order-svc/order/$O1" "" "$C2"
call 403 PUT "/order-svc/order/$O1/status" '{"status":"CONFIRMED"}' "$C1"
call 409 POST /order-svc/order "$(echo "$ORDER_BODY" | jq '.items[0].quantity=999')" "$C1"
call 400 POST /order-svc/order '{"items":[]}' "$C1"
call 401 GET /order-svc/order

step "Cancel releases the stock (customer 1)"
call 200 PUT "/order-svc/order/$O1/cancel" '{"reason":"changed my mind"}' "$C1"
call 200 GET "/inventory-svc/stock/availability?variantIds=$VARIANT"
echo "   available=$(j '.data[0].bestOffer.available') (expect 10 again)"
call 409 PUT "/order-svc/order/$O1/cancel" '{}' "$C1" || true

step "Full lifecycle (customer 2, admin moves it)"
call 201 POST /order-svc/order "$ORDER_BODY" "$C2"; O2=$(j .data.id); C2ID=$(j .data.customerId)
for s in CONFIRMED PACKED OUT_FOR_DELIVERY DELIVERED; do
  call 200 PUT "/order-svc/order/$O2/status" "{\"status\":\"$s\"}" "$ADMIN"
done
echo "   payment=$(j .data.paymentStatus) (expect PAID: cash on delivery)"
call 200 GET "/inventory-svc/stock/$VARIANT/seller/$SELLER" "" "$ADMIN"
echo "   quantity=$(j .data.quantity) reserved=$(j .data.reserved) (expect 8 / 0: two units sold)"
call 200 GET "/order-svc/order?customerId=$C2ID" "" "$ADMIN"
call 409 PUT "/order-svc/order/$O2/cancel" '{}' "$C2"
call 200 GET "/order-svc/order/$O2/log" "" "$C2"
echo "   history entries=$(j '.data | length') (expect 6: placed, 4 statuses, 1 payment)"

printf '\n%s\n' "$([ "$FAILS" = 0 ] && echo 'ALL CHECKS PASSED' || echo "$FAILS CHECK(S) FAILED")"
exit "$FAILS"
