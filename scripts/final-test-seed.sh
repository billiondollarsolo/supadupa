#!/usr/bin/env bash
# Final pre-release load test seeding.
# Usage: final-test-seed.sh <ref> <users> <extra_tables> <rows_each> <files>
# Exercises every Supabase surface AND loads the project heavily.
set -uo pipefail

API=https://api.supadupa.brotechlabs.com
APPS=apps.supadupa.brotechlabs.com
EMAIL=admin@supadupa.brotechlabs.com
PASS="${SUPADUPA_BOOTSTRAP_PASSWORD:?set SUPADUPA_BOOTSTRAP_PASSWORD in the environment (do not hardcode credentials)}"

ref="$1"; USERS="${2:-150}"; XTABLES="${3:-20}"; ROWS="${4:-2000}"; FILES="${5:-60}"
PH="$ref.$APPS"

RESAPI="--resolve api.supadupa.brotechlabs.com:443:127.0.0.1"
login(){ curl -s $RESAPI -X POST "$API/v1/auth/login" -H 'content-type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))'; }
TOK=$(login)
A(){ curl -s $RESAPI -H "authorization: Bearer $TOK" "$@"; }
C(){ curl -s -k --resolve "$PH:443:127.0.0.1" "$@"; }       # project edge, local, ignore cert
reveal(){ A "$API/v1/projects/$ref/secrets/$1/reveal" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("value",""))'; }
psqlc(){ docker exec "$ref-db-1" sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -U supabase_admin -d postgres -v ON_ERROR_STOP=1 -tAc "'"$1"'"' 2>&1; }
dbexec(){ docker exec -i "$ref-db-1" sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -U supabase_admin -d postgres -v ON_ERROR_STOP=1 -q' ; }

SR=$(reveal service_role); ANON=$(reveal anon_key)
echo "######## SEEDING $ref (users=$USERS extra_tables=$XTABLES rows/table=$ROWS files=$FILES) ########"
echo "  service_role len=${#SR} anon len=${#ANON}"

# ---------------------------------------------------------------------------
# 1) DATABASE: rich surface schema via the MANAGEMENT API (real code path)
# ---------------------------------------------------------------------------
SURF_SQL=$(cat <<'SQL'
create extension if not exists pgcrypto;
create extension if not exists "uuid-ossp";
create extension if not exists pg_trgm;
do $$
begin
  if not exists (select 1 from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname='auth' and p.proname='uid') then
    create schema if not exists auth;
    create function auth.uid() returns uuid language sql stable as $f$
      select coalesce(nullif(current_setting('request.jwt.claim.sub', true), ''),
        (nullif(current_setting('request.jwt.claims', true), '')::json ->> 'sub'))::uuid $f$;
  end if;
end $$;
create table if not exists public.notes (
  id uuid primary key default gen_random_uuid(),
  owner uuid not null default auth.uid(),
  title text not null, body text, updated_at timestamptz not null default now());
alter table public.notes enable row level security;
drop policy if exists notes_select_own on public.notes;
create policy notes_select_own on public.notes for select to authenticated using (auth.uid() = owner);
drop policy if exists notes_insert_own on public.notes;
create policy notes_insert_own on public.notes for insert to authenticated with check (auth.uid() = owner);
create table if not exists public.tags (id serial primary key, note_id uuid references public.notes(id) on delete cascade, label text not null);
create or replace view public.note_stats as select owner, count(*)::int as n from public.notes group by owner;
create or replace function public.touch_updated_at() returns trigger language plpgsql as $$ begin new.updated_at = now(); return new; end; $$;
drop trigger if exists notes_touch on public.notes;
create trigger notes_touch before update on public.notes for each row execute function public.touch_updated_at();
grant usage on schema public to anon, authenticated;
grant select on public.notes to anon, authenticated;
grant insert, update on public.notes to authenticated;
grant select on public.note_stats to anon, authenticated;
grant usage, select on all sequences in schema public to authenticated;
-- commerce tables
create table if not exists public.customers (id serial primary key, name text not null, email text unique, created_at timestamptz default now());
create table if not exists public.products (id serial primary key, sku text unique, title text, price_cents int);
create table if not exists public.orders (id serial primary key, customer_id int references public.customers(id), product_id int references public.products(id), qty int default 1, placed_at timestamptz default now());
create or replace view public.order_summary as select o.id, c.name as customer, p.title as product, o.qty, p.price_cents*o.qty as total_cents from public.orders o join public.customers c on c.id=o.customer_id join public.products p on p.id=o.product_id;
do $$ begin
  if exists (select 1 from pg_publication where pubname='supabase_realtime') then
    begin alter publication supabase_realtime add table public.notes; exception when others then null; end;
    begin alter publication supabase_realtime add table public.orders; exception when others then null; end;
  end if;
end $$;
SQL
)
payload=$(python3 -c "import json,sys;print(json.dumps({'name':'core-surface','version':'1','schema':'public','active':True,'sql':sys.stdin.read()}))" <<< "$SURF_SQL")
rc=$(curl -s $RESAPI -o /tmp/ft_surf_$ref.json -w '%{http_code}' -H "authorization: Bearer $TOK" \
  -X POST "$API/v1/projects/$ref/database/schemas" -H 'content-type: application/json' -d "$payload")
echo "  [db] surface schema apply -> HTTP $rc"

# ---------------------------------------------------------------------------
# 2) BULK DB LOAD via direct psql: many tables, thousands of rows, indexes,
#    a matview, a function, plus rows in the commerce tables.
# ---------------------------------------------------------------------------
dbexec <<SQL
\set ON_ERROR_STOP on
insert into public.customers(name,email)
  select 'Customer '||g, 'cust'||g||'@$ref.example.com' from generate_series(1,500) g
  on conflict do nothing;
insert into public.products(sku,title,price_cents)
  select 'SKU-'||g, 'Product '||g, (100+ (g*37) % 9000) from generate_series(1,300) g
  on conflict do nothing;
insert into public.orders(customer_id,product_id,qty,placed_at)
  select 1+(g % 500), 1+(g % 300), 1+(g % 5), now() - (g || ' minutes')::interval
  from generate_series(1,$ROWS) g;
do \$gen\$
declare i int;
begin
  for i in 1..$XTABLES loop
    execute format(\$f\$
      create table if not exists public.dataset_%1\$s (
        id bigserial primary key,
        uid uuid not null default gen_random_uuid(),
        label text not null,
        amount numeric(12,2) not null default 0,
        tags text[] default '{}',
        payload jsonb not null default '{}'::jsonb,
        is_active boolean default true,
        created_at timestamptz not null default now()
      )\$f\$, lpad(i::text,3,'0'));
    execute format(\$f\$
      insert into public.dataset_%1\$s(label,amount,tags,payload,is_active)
      select 'row-'||g, (random()*10000)::numeric(12,2),
             array['t'||(g%%7),'k'||(g%%3)],
             jsonb_build_object('n',g,'even',(g%%2=0),'grp',(g%%13)),
             (g%%5<>0)
      from generate_series(1,$ROWS) g \$f\$, lpad(i::text,3,'0'));
    execute format(\$f\$create index if not exists ix_dataset_%1\$s_label on public.dataset_%1\$s using gin (label gin_trgm_ops)\$f\$, lpad(i::text,3,'0'));
    execute format(\$f\$create index if not exists ix_dataset_%1\$s_payload on public.dataset_%1\$s using gin (payload)\$f\$, lpad(i::text,3,'0'));
  end loop;
end \$gen\$;
create materialized view if not exists public.dataset_rollup as
  select 'dataset_001'::text as src, count(*) n, sum(amount) total from public.dataset_001;
create or replace function public.order_total(cid int) returns bigint language sql stable as
  \$fn\$ select coalesce(sum(p.price_cents*o.qty),0) from public.orders o join public.products p on p.id=o.product_id where o.customer_id=cid \$fn\$;
analyze;
SQL
echo "  [db] bulk load done"
TABLES=$(psqlc "select count(*) from information_schema.tables where table_schema='public' and table_type='BASE TABLE'")
TOTROWS=$(psqlc "select sum(n_live_tup)::bigint from pg_stat_user_tables where schemaname='public'")
echo "  [db] public tables=$TABLES approx_rows=$TOTROWS"

# ---------------------------------------------------------------------------
# 3) AUTH: create many users in parallel via GoTrue admin API
# ---------------------------------------------------------------------------
echo "  [auth] creating $USERS users (parallel)..."
seq 1 "$USERS" | xargs -P 24 -I{} curl -s -k --resolve "$PH:443:127.0.0.1" -o /dev/null \
  -X POST "https://$PH/auth/v1/admin/users" -H "apikey: $SR" -H "authorization: Bearer $SR" \
  -H 'content-type: application/json' \
  -d "{\"email\":\"user{}@$ref.example.com\",\"password\":\"L0adTest-$ref-{}!\",\"email_confirm\":true}"
# one signed-in user for a real JWT + RLS proof
C -X POST "https://$PH/auth/v1/admin/users" -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: application/json' \
  -d "{\"email\":\"alice@$ref.test\",\"password\":\"Forge-Pass-1!\",\"email_confirm\":true}" >/dev/null
ATOK=$(C -X POST "https://$PH/auth/v1/token?grant_type=password" -H "apikey: $ANON" -H 'content-type: application/json' \
  -d "{\"email\":\"alice@$ref.test\",\"password\":\"Forge-Pass-1!\"}" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("access_token",""))')
# alice writes an RLS-owned note
C -X POST "https://$PH/rest/v1/notes" -H "apikey: $ANON" -H "authorization: Bearer $ATOK" \
  -H 'content-type: application/json' -H 'Prefer: return=representation' \
  -d '{"title":"Alice secret","body":"top secret"}' >/dev/null
UCOUNT=$(C "https://$PH/auth/v1/admin/users?per_page=1" -H "apikey: $SR" -H "authorization: Bearer $SR" \
  | python3 -c 'import sys,json
try:
 d=json.load(sys.stdin);print(len(d.get("users",d if isinstance(d,list) else [])))
except:print("?")')
AUDB=$(psqlc "select count(*) from auth.users")
echo "  [auth] users in auth.users=$AUDB  signin_jwt=$([ -n "$ATOK" ] && echo ok || echo FAIL)"

# ---------------------------------------------------------------------------
# 4) STORAGE: buckets (public+private) + many object uploads in parallel
# ---------------------------------------------------------------------------
for b in 'avatars|true' 'documents|false' 'product-images|true' 'backups|false' 'exports|false'; do
  IFS='|' read -r bn bp <<< "$b"
  A -X POST "$API/v1/projects/$ref/storage/buckets" -H 'content-type: application/json' \
    -d "{\"name\":\"$bn\",\"public\":$bp,\"file_size_limit\":52428800}" >/dev/null
done
echo "  [storage] uploading $FILES objects (parallel)..."
echo "payload for $ref" > /tmp/ft_obj_$ref.bin
seq 1 "$FILES" | xargs -P 16 -I{} curl -s -k --resolve "$PH:443:127.0.0.1" -o /dev/null \
  -X POST "https://$PH/storage/v1/object/product-images/file-{}.txt" \
  -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: text/plain' \
  --data-binary @/tmp/ft_obj_$ref.bin
BCOUNT=$(C "https://$PH/storage/v1/bucket" -H "apikey: $SR" -H "authorization: Bearer $SR" | python3 -c 'import sys,json
try:print(len(json.load(sys.stdin)))
except:print("?")')
OBJDB=$(psqlc "select count(*) from storage.objects")
echo "  [storage] buckets=$BCOUNT objects_in_db=$OBJDB"

# ---------------------------------------------------------------------------
# 5) EDGE FUNCTIONS: deploy several + invoke one
# ---------------------------------------------------------------------------
deploy_fn(){
  local name="$1" src="$2"
  local fp=$(python3 -c "import json,sys;print(json.dumps({'name':sys.argv[1],'entrypoint':'index.ts','verify_jwt':False,'source':sys.argv[2]}))" "$name" "$src")
  curl -s $RESAPI -o /dev/null -w '%{http_code}' -H "authorization: Bearer $TOK" \
    -X POST "$API/v1/projects/$ref/functions" -H 'content-type: application/json' -d "$fp"
}
f1=$(deploy_fn hello 'Deno.serve((req)=>new Response(JSON.stringify({ok:true,fn:"hello",from:"'$ref'"}),{headers:{"content-type":"application/json"}}));')
f2=$(deploy_fn echo  'Deno.serve(async (req)=>{const b=await req.text();return new Response(JSON.stringify({echo:b,len:b.length}),{headers:{"content-type":"application/json"}});});')
f3=$(deploy_fn now   'Deno.serve(()=>new Response(JSON.stringify({ts:Date.now()}),{headers:{"content-type":"application/json"}}));')
echo "  [functions] deploy hello=$f1 echo=$f2 now=$f3"
sleep 3
INV=$(C -X POST "https://$PH/functions/v1/hello" -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: application/json' -d '{}')
echo "  [functions] invoke hello -> $(echo "$INV" | head -c 80)"

# ---------------------------------------------------------------------------
# 6) GraphQL (pg_graphql)
# ---------------------------------------------------------------------------
GQ=$(C -X POST "https://$PH/graphql/v1" -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: application/json' \
  -d '{"query":"{ notesCollection { edges { node { id title } } } }"}')
echo "  [graphql] $(echo "$GQ" | python3 -c 'import sys,json
try:
 d=json.load(sys.stdin);print("data ok" if d.get("data") else "err:"+str(d.get("errors"))[:60])
except Exception as e:print("parse err")')"

echo "######## $ref SEED COMPLETE ########"
