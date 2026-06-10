#!/usr/bin/env bash
# Seed the two DB-allowlisted projects. With the HTTP/DB allowlist split, their
# HTTP (Studio/API/Storage) is open by default, so seeding works over HTTPS
# while their DB ports stay restricted to example CIDRs.
set -uo pipefail
API=https://api.supadupa.brotechlabs.com
APPS=apps.supadupa.brotechlabs.com
EMAIL=admin@supadupa.brotechlabs.com
PASS="${SUPADUPA_BOOTSTRAP_PASSWORD:?set SUPADUPA_BOOTSTRAP_PASSWORD in the environment (do not hardcode credentials)}"
login(){ curl -s -X POST "$API/v1/auth/login" -H 'content-type: application/json' -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))'; }
TOK=$(login); A(){ curl -s -H "authorization: Bearer $TOK" "$@"; }

# ref|db_allowlist (documentation/example CIDRs only)
TARGETS=( "blog-beta|198.51.100.0/24" "crm-delta|192.0.2.0/24" )

for t in "${TARGETS[@]}"; do
  IFS='|' read -r ref dballow <<< "$t"
  TOK=$(login)
  echo "=== seeding $ref ==="
  for b in 'avatars|true' 'documents|false' 'product-images|true' 'backups|false'; do
    IFS='|' read -r bn bp <<< "$b"
    A -X POST "$API/v1/projects/$ref/storage/buckets" -H 'content-type: application/json' \
      -d "{\"name\":\"$bn\",\"public\":$bp,\"file_size_limit\":52428800}" >/dev/null
  done
  echo "  buckets: $(A "$API/v1/projects/$ref/storage/buckets" | python3 -c 'import sys,json
try:print(len(json.load(sys.stdin)))
except:print("?")')"
  SR=$(A "$API/v1/projects/$ref/secrets/service_role/reveal" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("value",""))')
  PH="$ref.$APPS"; uc=0
  for u in ada alan grace katherine margaret; do
    hc=$(curl -s -k --resolve "$PH:443:127.0.0.1" -o /dev/null -w '%{http_code}' -X POST "https://$PH/auth/v1/admin/users" \
      -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: application/json' \
      -d "{\"email\":\"$u@$ref.example.com\",\"password\":\"L0adTest-$ref!\",\"email_confirm\":true}")
    [ "$hc" = "200" ] && uc=$((uc+1))
  done
  echo "  users created: $uc"
  echo "hello from $ref" > /tmp/rs_$ref.txt
  curl -s -k --resolve "$PH:443:127.0.0.1" -X POST "https://$PH/storage/v1/object/product-images/readme-$ref.txt" \
    -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: text/plain' --data-binary @/tmp/rs_$ref.txt >/dev/null
  echo "  object uploaded"
  # DB allowlist restricted to an example CIDR; HTTP left open (empty http_allowlist).
  A -X PUT "$API/v1/projects/$ref/config/network" -H 'content-type: application/json' \
    -d "{\"config\":{\"db_ingress_mode\":\"allowlisted\",\"db_allowlist\":\"$dballow\",\"http_allowlist\":\"\",\"ssl_enforced\":\"true\"}}" >/dev/null
  echo "  network: HTTP open, DB allowlisted ($dballow)"
done
echo "=== reseed done ==="
