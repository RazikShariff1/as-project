#!/usr/bin/env bash
# End-to-end check of the deployed platform through the gateway.
#
#   ADMIN_EMAIL=you@example.com ADMIN_PASSWORD='...' ./scripts/e2e.sh
#   BASE=https://other.onrender.com ... ./scripts/e2e.sh        # default BASE is the Render deploy
#   SKIP_SEED=1 ...                                               # roles were already seeded
#
# It creates test data (a seller, catalogue entries, stock, two shoppers, orders) with a unique suffix, so it can be re-run.
# Team calls go through /api (admin token); shopper calls go through /store (token from customer-svc).
# Needs: bash, curl, jq, and Go (only for the one-time role seeding). Credentials come from the environment, never from this file.
set -uo pipefail

BASE="${BASE:-https://as-project-j88g.onrender.com}"
: "${ADMIN_EMAIL:?set ADMIN_EMAIL}"
: "${ADMIN_PASSWORD:?set ADMIN_PASSWORD}"
API="$BASE/api"
STORE="$BASE/store"
RUN="$(date +%s)"
FAILS=0
BODY=""

# req <root> <expected-status> <METHOD> <path> [json] [token]   -> sets $BODY, prints PASS/FAIL
req() {
  local root="$1" want="$2" method="$3" path="$4" data="${5:-}" token="${6:-}" code
  local args=(-s -m 90 -o /tmp/e2e.body -w '%{http_code}' -X "$method" -H 'Content-Type: application/json')
  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$data" ] && args+=(-d "$data")
  code=$(curl "${args[@]}" "$root$path")
  BODY="$(cat /tmp/e2e.body)"
  if [ "$code" = "$want" ]; then printf 'PASS %-6s %-58s %s\n' "$method" "$path" "$code"
  else printf 'FAIL %-6s %-58s got %s want %s\n     %s\n' "$method" "$path" "$code" "$want" "$(echo "$BODY" | head -c 300)"; FAILS=$((FAILS+1)); fi
}
call()  { req "$API" "$@"; }     # the team's realm
scall() { req "$STORE" "$@"; }   # the shoppers' realm
j() { echo "$BODY" | jq -r "$1"; }
step() { printf '\n== %s\n' "$1"; }

step "Wake the services and log in as admin"
curl -s -m 120 -o /dev/null "$BASE/.well-known/alive"
call 201 POST /account-svc/auth/login "$(jq -n --arg e "$ADMIN_EMAIL" --arg p "$ADMIN_PASSWORD" '{email:$e,password:$p}')"
ADMIN=$(j .data.token)
[ "$ADMIN" != null ] && [ -n "$ADMIN" ] || { echo "cannot continue without an admin token"; exit 1; }
call 200 GET /account-svc/auth/me "" "$ADMIN"

if [ -z "${SKIP_SEED:-}" ]; then
  step "Seed endpoints and roles (one time; safe to repeat)"
  ( cd "$(dirname "$0")/../gateway-svc" && ACCOUNT_SVC_URL="$API/account-svc" ADMIN_EMAIL="$ADMIN_EMAIL" ADMIN_PASSWORD="$ADMIN_PASSWORD" go run ./cmd/seed ) \
    || { echo "FAIL seeding"; FAILS=$((FAILS+1)); }
fi
call 200 GET '/account-svc/role?limit=100' "" "$ADMIN"
[ -n "$(j '.data.roles[] | select(.name=="customer-support") | .id')" ] || { echo "no 'customer-support' role: seeding did not run, or the gateway build is old"; exit 1; }

step "Catalogue: seller, brand, category, product, variant"
call 201 POST /catalogue-svc/seller "$(jq -n --arg n "E2E Seller $RUN" '{name:$n,status:"active"}')" "$ADMIN";           SELLER=$(j .data.id)
call 201 POST /catalogue-svc/brand "$(jq -n --arg n "E2E Brand $RUN" '{name:$n,status:"ENABLED"}')" "$ADMIN";           BRAND=$(j .data.id)
call 201 POST /catalogue-svc/category "$(jq -n --arg n "E2E Category $RUN" '{name:$n,status:"ENABLED"}')" "$ADMIN";    CAT=$(j .data.id)
call 201 POST /catalogue-svc/product "$(jq -n --arg n "E2E Rice $RUN" --argjson c "$CAT" --argjson b "$BRAND" \
  '{name:$n,primaryCategoryId:$c,categoryIds:[$c],brandId:$b,status:"ENABLED"}')" "$ADMIN";                            PRODUCT=$(j .data.id)
call 201 POST /catalogue-svc/variant "$(jq -n --argjson p "$PRODUCT" --arg n "1 kg $RUN" --arg c "E2E-$RUN" \
  '{productId:$p,name:$n,clientItemId:$c,status:"ENABLED"}')" "$ADMIN"; VARIANT=$(j .data.id)
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

step "Two shoppers sign up through /store (customer-svc)"
for n in 1 2; do
  scall 201 POST /customer-svc/auth/register "$(jq -n --arg e "e2e-$RUN-$n@example.com" \
    '{name:"E2E Shopper",email:$e,phone:"9999999999",password:"E2e-Passw0rd!"}')"
  eval "C$n=$(j .data.token)"
done
scall 409 POST /customer-svc/auth/register "$(jq -n --arg e "e2e-$RUN-1@example.com" '{name:"Again",email:$e,password:"E2e-Passw0rd!"}')"
scall 201 POST /customer-svc/auth/login "$(jq -n --arg e "e2e-$RUN-1@example.com" '{email:$e,password:"E2e-Passw0rd!"}')"
scall 401 POST /customer-svc/auth/login "$(jq -n --arg e "e2e-$RUN-1@example.com" '{email:$e,password:"wrong-password"}')"
scall 200 GET /customer-svc/customer/me "" "$C1"
C1ID=$(j .data.id)
scall 200 PUT /customer-svc/customer/me '{"name":"E2E Shopper One","phone":"8888888888"}' "$C1"
scall 201 POST /customer-svc/customer/me/address '{"label":"Home","recipient":"Asha","line1":"1 Main St","city":"Pune","postalCode":"411001"}' "$C1"
A1=$(j .data.id)
echo "   first address is default: $(j .data.isDefault) (expect true)"
scall 200 GET /customer-svc/customer/me/address "" "$C1"
scall 404 GET "/customer-svc/customer/me/address/$A1" "" "$C2"
echo "   (another shopper cannot read the address)"
scall 200 GET /customer-svc/customer/me/address "" "$C2"
echo "   shopper 2 sees $(j '.data | length') addresses (expect 0)"

step "The two realms stay apart"
call 401 GET /order-svc/order "" "$C1"
scall 401 GET /order-svc/order "" "$ADMIN"
scall 404 GET /account-svc/user "" "$C1"
scall 404 GET /customer-svc/customer "" "$C1"
call 200 GET "/customer-svc/customer?limit=5" "" "$ADMIN"
call 200 GET "/customer-svc/customer/$C1ID" "" "$ADMIN"
echo "   staff see customer: $(j .data.email)"

ADDR='{"name":"Asha","phone":"9999999999","line1":"1 Main St","city":"Pune","pincode":"411001"}'
ORDER_BODY=$(jq -n --argjson v "$VARIANT" --argjson s "$SELLER" --argjson a "$ADDR" '{items:[{variantId:$v,sellerId:$s,quantity:2}],deliveryAddress:$a}')

step "Orders: place, read, list (customer 1)"
scall 201 POST /order-svc/order "$ORDER_BODY" "$C1"; O1=$(j .data.order.referenceNumber)
echo "   total=$(j .data.order.total) status=$(j .data.order.status) payment=$(j .data.order.paymentStatus)  (expect 200 / PENDING / PENDING)"
scall 200 GET "/order-svc/order/$O1" "" "$C1"
scall 200 GET /order-svc/order "" "$C1"
scall 200 GET "/order-svc/order-log?orderReferenceNumber=$O1" "" "$C1"
scall 200 GET "/order-svc/order-log?orderReferenceNumber=$O1" "" "$C2"
echo "   other customer sees $(j .data.count) history entries (expect 0)"
scall 400 GET "/order-svc/order-log" "" "$C1"
call 200 GET "/inventory-svc/stock/availability?variantIds=$VARIANT"
echo "   available=$(j '.data[0].bestOffer.available') (expect 8: two units are held)"

step "Isolation and rules"
scall 404 GET "/order-svc/order/$O1" "" "$C2"
scall 403 PATCH "/order-svc/order/$O1" '{"status":"PACKED"}' "$C1"
scall 404 PATCH "/order-svc/order/$O1" '{"status":"CANCELLED"}' "$C2"
scall 409 POST /order-svc/order "$(echo "$ORDER_BODY" | jq '.items[0].quantity=999')" "$C1"
scall 400 POST /order-svc/order '{"items":[]}' "$C1"
scall 401 GET /order-svc/order
scall 403 GET "/order-svc/order?customerId=999999" "" "$C1"
scall 403 POST /order-svc/order "$(echo "$ORDER_BODY" | jq '.customerId=999999')" "$C1"

step "Cancel releases the stock (customer 1)"
scall 200 PATCH "/order-svc/order/$O1" '{"status":"CANCELLED","cancellationReason":"changed my mind"}' "$C1"
call 200 GET "/inventory-svc/stock/availability?variantIds=$VARIANT"
echo "   available=$(j '.data[0].bestOffer.available') (expect 10 again)"
scall 200 PATCH "/order-svc/order/$O1" '{"status":"CANCELLED"}' "$C1"  # repeating only retries the stock release

step "Full lifecycle (customer 2, admin moves it)"
scall 201 POST /order-svc/order "$ORDER_BODY" "$C2"; O2=$(j .data.order.referenceNumber); C2ID=$(j .data.order.customerId)
echo "   order belongs to shopper $C2ID, not an account-svc user"
call 200 PATCH "/order-svc/order/$O2" '{"notes":"leave at the gate","metaData":{"source":"e2e"}}' "$ADMIN"
call 409 PATCH "/order-svc/order/$O2" '{"status":"COMPLETED"}' "$ADMIN"
for s in PACKED DISPATCHED COMPLETED; do
  call 200 PATCH "/order-svc/order/$O2" "{\"status\":\"$s\"}" "$ADMIN"
done
echo "   payment=$(j .data.order.paymentStatus) (expect PAID: cash on delivery)"
call 200 GET "/inventory-svc/stock/$VARIANT/seller/$SELLER" "" "$ADMIN"
echo "   quantity=$(j .data.quantity) reserved=$(j .data.reserved) (expect 8 / 0: two units sold)"
call 200 GET "/order-svc/order?customerId=$C2ID" "" "$ADMIN"
scall 409 PATCH "/order-svc/order/$O2" '{"status":"CANCELLED"}' "$C2"
scall 404 POST /order-svc/order-log "{\"referenceNumber\":$O2,\"action\":\"NOTE\",\"details\":{\"note\":\"x\"}}" "$C2"   # staff-only: not on /store at all
call 201 POST /order-svc/order-log "{\"referenceNumber\":$O2,\"action\":\"NOTE\",\"details\":{\"note\":\"called the customer\"}}" "$ADMIN"
scall 200 GET "/order-svc/order-log?orderReferenceNumber=$O2" "" "$C2"
echo "   history entries=$(j '.data.count') (expect 7: pending, 3 statuses, 1 payment, 1 update, 1 note)"

printf '\n%s\n' "$([ "$FAILS" = 0 ] && echo 'ALL CHECKS PASSED' || echo "$FAILS CHECK(S) FAILED")"
exit "$FAILS"
