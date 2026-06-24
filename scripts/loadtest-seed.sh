#!/usr/bin/env bash
# Full redeploy load test: create 4 projects, expose them, seed real data
# (buckets, tables+rows, users, storage objects), then run a per-project suite.
set -uo pipefail

API=https://api.supadupa.brotechlabs.com
APPS=apps.supadupa.brotechlabs.com
EMAIL=admin@supadupa.brotechlabs.com
PASS="${SUPADUPA_BOOTSTRAP_PASSWORD:?set SUPADUPA_BOOTSTRAP_PASSWORD in the environment (do not hardcode credentials)}"
HOST_ID=88eeeaeffd71ac8c4e624d63b36f792e

login() { curl -s -X POST "$API/v1/auth/login" -H 'content-type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))'; }

TOK=""
refresh() { TOK=$(login); }
A() { curl -s -H "authorization: Bearer $TOK" "$@"; }

# ref|name|ingress_mode|allowlist
PROJECTS=(
"shop-alpha|Shop Alpha|public|"
"blog-beta|Blog Beta|allowlisted|203.0.113.10/32,198.51.100.0/24"
"iot-gamma|IoT Gamma|public|"
"crm-delta|CRM Delta|allowlisted|192.0.2.0/24"
)

echo "############ PHASE 1: platform setup ############"
refresh; echo "login token len ${#TOK}"
DEF=$(A "$API/v1/settings/defaults")
# Strict decoder rejects read-only fields, so strip updated_at before PUT.
NEWDEF=$(echo "$DEF" | python3 -c 'import sys,json
d=json.load(sys.stdin); d.pop("updated_at",None)
d["feature_flags"]["database_external_access"]=True
print(json.dumps(d))')
A -X PUT "$API/v1/settings/defaults" -H 'content-type: application/json' -d "$NEWDEF" >/dev/null
echo "database_external_access -> $(A "$API/v1/settings/defaults" | python3 -c 'import sys,json;print(json.load(sys.stdin)["feature_flags"]["database_external_access"])')"
# Reuse an existing org if projects already exist (idempotent re-run), else create.
ORG=$(A "$API/v1/projects" | python3 -c 'import sys,json
d=json.load(sys.stdin); print(d[0]["org_id"] if d else "")' 2>/dev/null)
if [ -z "$ORG" ]; then
  ORG=$(A -X POST "$API/v1/orgs" -H 'content-type: application/json' -d '{"name":"Acme Corp"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
fi
echo "org id: $ORG"

echo "############ PHASE 2: create 4 projects (synchronous compose up) ############"
for e in "${PROJECTS[@]}"; do
  IFS='|' read -r ref name mode allow <<< "$e"
  refresh
  exists=$(A "$API/v1/projects/$ref" | python3 -c 'import sys,json
try: print(json.load(sys.stdin).get("ref",""))
except: print("")' 2>/dev/null)
  if [ "$exists" = "$ref" ]; then echo "  $ref already exists, skip create"; continue; fi
  body=$(python3 -c "import json;print(json.dumps({'ref':'$ref','name':'$name','host_id':'$HOST_ID','domain':'$APPS','profile':'full','stack_version':'latest'}))")
  code=$(curl -s -o /tmp/seed_create_$ref.json -w '%{http_code}' --max-time 900 -H "authorization: Bearer $TOK" \
    -X POST "$API/v1/orgs/$ORG/projects" -H 'content-type: application/json' -d "$body")
  echo "  create $ref -> HTTP $code ($(head -c 120 /tmp/seed_create_$ref.json))"
done

echo "############ PHASE 3: wait for readiness ############"
for e in "${PROJECTS[@]}"; do
  IFS='|' read -r ref name mode allow <<< "$e"
  refresh
  for i in $(seq 1 60); do
    phase=$(A "$API/v1/projects/$ref" | python3 -c 'import sys,json
d=json.load(sys.stdin); rs=d.get("runtime_status") or {}; print(rs.get("phase",""))' 2>/dev/null)
    [ "$phase" = "healthy" ] && { echo "  $ref healthy"; break; }
    sleep 5
    [ $((i%6)) -eq 0 ] && refresh
  done
  echo "  $ref final phase: ${phase:-unknown}"
done

echo "############ PHASE 4: expose (per-project DB ingress) ############"
for e in "${PROJECTS[@]}"; do
  IFS='|' read -r ref name mode allow <<< "$e"
  refresh
  ncfg=$(python3 -c "import json;print(json.dumps({'config':{'db_ingress_mode':'$mode','ip_allowlist':'$allow','ssl_enforced':'true'}}))")
  out=$(A -X PUT "$API/v1/projects/$ref/config/network" -H 'content-type: application/json' -d "$ncfg")
  echo "  $ref ingress=$mode allow='$allow' -> $(echo "$out" | python3 -c 'import sys,json
try:
 d=json.load(sys.stdin);print("ok",d.get("config",{}).get("db_ingress_mode"))
except: print("ERR")')"
  # mark two of them production to exercise the advisor gate
  case "$ref" in shop-alpha|crm-delta)
    A -X PUT "$API/v1/projects/$ref/config/general" -H 'content-type: application/json' -d '{"config":{"environment":"production"}}' >/dev/null
    echo "  $ref -> environment=production" ;;
  esac
done

echo "############ PHASE 5: seed data (buckets, tables, users, objects) ############"
SQL='create extension if not exists pgcrypto;
create table if not exists public.customers (id serial primary key, name text not null, email text unique, created_at timestamptz default now());
create table if not exists public.products (id serial primary key, sku text unique, title text, price_cents int);
create table if not exists public.orders (id serial primary key, customer_id int references public.customers(id), product_id int references public.products(id), qty int default 1, placed_at timestamptz default now());
insert into public.customers (name,email) values ('"'"'Ada Lovelace'"'"','"'"'ada@example.com'"'"'),('"'"'Alan Turing'"'"','"'"'alan@example.com'"'"'),('"'"'Grace Hopper'"'"','"'"'grace@example.com'"'"'),('"'"'Katherine Johnson'"'"','"'"'katherine@example.com'"'"') on conflict do nothing;
insert into public.products (sku,title,price_cents) values ('"'"'SKU-1'"'"','"'"'Widget'"'"',1999),('"'"'SKU-2'"'"','"'"'Gadget'"'"',4999),('"'"'SKU-3'"'"','"'"'Gizmo'"'"',999) on conflict do nothing;
insert into public.orders (customer_id,product_id,qty) values (1,1,2),(2,3,1),(3,2,5),(1,2,1) on conflict do nothing;
create or replace view public.order_summary as select o.id, c.name as customer, p.title as product, o.qty, p.price_cents*o.qty as total_cents from public.orders o join public.customers c on c.id=o.customer_id join public.products p on p.id=o.product_id;'

for e in "${PROJECTS[@]}"; do
  IFS='|' read -r ref name mode allow <<< "$e"
  refresh
  echo "--- seeding $ref ---"
  # buckets
  for b in 'avatars|true' 'documents|false' 'product-images|true' 'backups|false'; do
    IFS='|' read -r bn bp <<< "$b"
    A -X POST "$API/v1/projects/$ref/storage/buckets" -H 'content-type: application/json' \
      -d "{\"name\":\"$bn\",\"public\":$bp,\"file_size_limit\":52428800}" >/dev/null
  done
  echo "  buckets: $(A "$API/v1/projects/$ref/storage/buckets" | python3 -c 'import sys,json
try:print(len(json.load(sys.stdin)))
except:print("?")')"
  # tables + data
  payload=$(python3 -c "import json,sys;print(json.dumps({'name':'core-schema','version':'1','schema':'public','active':True,'sql':sys.stdin.read()}))" <<< "$SQL")
  rc=$(curl -s -o /tmp/seed_schema_$ref.json -w '%{http_code}' -H "authorization: Bearer $TOK" \
    -X POST "$API/v1/projects/$ref/database/schemas" -H 'content-type: application/json' -d "$payload")
  echo "  tables apply -> HTTP $rc"
  # users via project gotrue admin
  SR=$(A "$API/v1/projects/$ref/secrets/service_role/reveal" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("value",""))')
  PH="$ref.$APPS"
  ucount=0
  for u in ada alan grace katherine margaret; do
    hc=$(curl -s -k --resolve "$PH:443:127.0.0.1" -o /dev/null -w '%{http_code}' -X POST "https://$PH/auth/v1/admin/users" \
      -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: application/json' \
      -d "{\"email\":\"$u@$ref.example.com\",\"password\":\"L0adTest-$ref!\",\"email_confirm\":true}")
    [ "$hc" = "200" ] && ucount=$((ucount+1))
  done
  echo "  users created: $ucount"
  # storage objects: upload a couple files to a public bucket
  echo "hello from $ref $(date -u)" > /tmp/seed_obj_$ref.txt
  curl -s -k --resolve "$PH:443:127.0.0.1" -X POST "https://$PH/storage/v1/object/product-images/readme-$ref.txt" \
    -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: text/plain' --data-binary @/tmp/seed_obj_$ref.txt >/dev/null
  python3 -c "import json;open('/tmp/seed_obj2_$ref.json','w').write(json.dumps({'project':'$ref','seeded':True,'rows':8}))"
  curl -s -k --resolve "$PH:443:127.0.0.1" -X POST "https://$PH/storage/v1/object/avatars/meta-$ref.json" \
    -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: application/json' --data-binary @/tmp/seed_obj2_$ref.json >/dev/null
  echo "  objects uploaded to product-images + avatars"
done

echo "############ PHASE 6: per-project full suite test ############"
printf '%-12s %-9s %-7s %-7s %-9s %-8s %-7s\n' PROJECT PHASE TABLES ROWS BUCKETS USERS STUDIO
for e in "${PROJECTS[@]}"; do
  IFS='|' read -r ref name mode allow <<< "$e"
  refresh
  phase=$(A "$API/v1/projects/$ref" | python3 -c 'import sys,json;d=json.load(sys.stdin);print((d.get("runtime_status") or {}).get("phase","?"))')
  SR=$(A "$API/v1/projects/$ref/secrets/service_role/reveal" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("value",""))')
  PH="$ref.$APPS"
  tables=$(docker exec "$ref-db-1" sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -U supabase_admin -d postgres -tAc "select count(*) from information_schema.tables where table_schema='"'"'public'"'"';"' 2>/dev/null | tr -d ' ')
  rows=$(docker exec "$ref-db-1" sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -U supabase_admin -d postgres -tAc "select (select count(*) from public.customers)+(select count(*) from public.products)+(select count(*) from public.orders);"' 2>/dev/null | tr -d ' ')
  buckets=$(curl -s -k --resolve "$PH:443:127.0.0.1" "https://$PH/storage/v1/bucket" -H "apikey: $SR" -H "authorization: Bearer $SR" | python3 -c 'import sys,json
try:print(len(json.load(sys.stdin)))
except:print("?")')
  users=$(curl -s -k --resolve "$PH:443:127.0.0.1" "https://$PH/auth/v1/admin/users" -H "apikey: $SR" -H "authorization: Bearer $SR" | python3 -c 'import sys,json
try:
 d=json.load(sys.stdin);print(len(d.get("users",d if isinstance(d,list) else [])))
except:print("?")')
  rest=$(curl -s -k --resolve "$PH:443:127.0.0.1" "https://$PH/rest/v1/products?select=sku" -H "apikey: $SR" -H "authorization: Bearer $SR" | python3 -c 'import sys,json
try:print(len(json.load(sys.stdin)))
except:print("?")')
  studio=$(curl -s -k -o /dev/null -w '%{http_code}' --resolve "studio-$ref.$APPS:443:127.0.0.1" "https://studio-$ref.$APPS/")
  printf '%-12s %-9s %-7s %-7s %-9s %-8s %-7s (rest=%s)\n' "$ref" "$phase" "$tables" "$rows" "$buckets" "$users" "$studio" "$rest"
done

echo "############ DONE ############"
