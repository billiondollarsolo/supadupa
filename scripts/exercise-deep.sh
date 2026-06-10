#!/usr/bin/env bash
# Exercise EVERY Supabase surface on one project and print a PASS/FAIL matrix.
set -uo pipefail
API=https://api.supadupa.brotechlabs.com
APPS=apps.supadupa.brotechlabs.com
ref="$1"
PH="$ref.$APPS"
EMAIL=admin@supadupa.brotechlabs.com
PASS="${SUPADUPA_BOOTSTRAP_PASSWORD:?set SUPADUPA_BOOTSTRAP_PASSWORD in the environment (do not hardcode credentials)}"
login(){ curl -s -X POST $API/v1/auth/login -H 'content-type: application/json' -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))'; }
TOK=$(login); A(){ curl -s -H "authorization: Bearer $TOK" "$@"; }
C(){ curl -s -k --resolve "$PH:443:127.0.0.1" "$@"; }   # hit project edge locally, ignore fresh-cert TLS
reveal(){ A "$API/v1/projects/$ref/secrets/$1/reveal" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("value",""))'; }
jget(){ python3 -c "import sys,json;d=json.load(sys.stdin);print($1)" 2>/dev/null; }
SR=$(reveal service_role); ANON=$(reveal anon_key)
declare -A R   # results matrix

echo "######## EXERCISING $ref ########"

# 1) DATABASE: extensions, tables, FK, view, trigger, RLS, grants, realtime publication
payload=$(python3 -c "import json;print(json.dumps({'name':'core','version':'1','schema':'public','active':True,'sql':open('/tmp/exercise_schema.sql').read()}))")
code=$(curl -s -o /tmp/ex_db_$ref.json -w '%{http_code}' -H "authorization: Bearer $TOK" -X POST "$API/v1/projects/$ref/database/schemas" -H 'content-type: application/json' -d "$payload")
exts=$(docker exec $ref-db-1 sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -U supabase_admin -d postgres -tAc "select count(*) from pg_extension where extname in ('"'"'pgcrypto'"'"','"'"'uuid-ossp'"'"','"'"'pg_trgm'"'"');"' 2>/dev/null|tr -d ' ')
rls=$(docker exec $ref-db-1 sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -U supabase_admin -d postgres -tAc "select relrowsecurity from pg_class where relname='"'"'notes'"'"';"' 2>/dev/null|tr -d ' ')
trg=$(docker exec $ref-db-1 sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -U supabase_admin -d postgres -tAc "select count(*) from pg_trigger where tgname='"'"'notes_touch'"'"';"' 2>/dev/null|tr -d ' ')
R[db_apply]="HTTP $code"; R[extensions]="$exts/3"; R[rls_enabled]="$rls"; R[trigger]="$trg"

# 2) AUTH: create two users, sign one in for a real JWT
mkuser(){ C -X POST "https://$PH/auth/v1/admin/users" -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: application/json' -d "{\"email\":\"$1\",\"password\":\"Forge-Pass-1!\",\"email_confirm\":true}" | jget 'd.get("id","")'; }
ALICE=$(mkuser "alice@$ref.test"); BOB=$(mkuser "bob@$ref.test")
signin(){ C -X POST "https://$PH/auth/v1/token?grant_type=password" -H "apikey: $ANON" -H 'content-type: application/json' -d "{\"email\":\"$1\",\"password\":\"Forge-Pass-1!\"}" | jget 'd.get("access_token","")'; }
ATOK=$(signin "alice@$ref.test"); BTOK=$(signin "bob@$ref.test")
ucount=$(C "https://$PH/auth/v1/admin/users" -H "apikey: $SR" -H "authorization: Bearer $SR" | jget 'len(d.get("users",[]))')
R[auth_users]="$ucount"; R[auth_signin]=$([ -n "$ATOK" ] && echo "JWT ok" || echo FAIL)

# 3) REST + RLS proof (anon blocked by RLS, owner sees own, service bypasses, other user isolated)
C -X POST "https://$PH/rest/v1/notes" -H "apikey: $ANON" -H "authorization: Bearer $ATOK" -H 'content-type: application/json' -H 'Prefer: return=representation' -d '{"title":"Alice secret","body":"top secret"}' >/tmp/ex_ins_$ref.json
ins=$(jget 'len(d) if isinstance(d,list) else d' </tmp/ex_ins_$ref.json)
restc(){ C "https://$PH/rest/v1/notes?select=id" -H "apikey: $2" -H "authorization: Bearer $3" | jget 'len(d) if isinstance(d,list) else "err"'; }
R[rest_alice]=$(restc x "$ANON" "$ATOK")     # owner -> 1
R[rest_anon]=$(restc x "$ANON" "$ANON")      # RLS -> 0
R[rest_service]=$(restc x "$SR" "$SR")       # bypass -> >=1
R[rest_bob]=$(restc x "$ANON" "$BTOK")       # other user -> 0
# FK: service inserts a tag referencing alice's note
NID=$(C "https://$PH/rest/v1/notes?select=id&limit=1" -H "apikey: $SR" -H "authorization: Bearer $SR" | jget 'd[0]["id"] if d else ""')
fk=$(C -o /dev/null -w '%{http_code}' -X POST "https://$PH/rest/v1/tags" -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: application/json' -d "{\"note_id\":\"$NID\",\"label\":\"urgent\"}")
R[rest_fk_insert]="HTTP $fk"

# 4) GraphQL (pg_graphql)
gq=$(C -X POST "https://$PH/graphql/v1" -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: application/json' -d '{"query":"{ notesCollection { edges { node { id title } } } }"}')
R[graphql]=$(echo "$gq" | jget '"data ok" if d.get("data") else ("err:"+str(d.get("errors"))[:40])')

# 5) STORAGE: buckets (public+private) + object upload/list
for b in 'avatars|true' 'documents|false'; do IFS='|' read -r bn bp <<<"$b"; A -X POST "$API/v1/projects/$ref/storage/buckets" -H 'content-type: application/json' -d "{\"name\":\"$bn\",\"public\":$bp,\"file_size_limit\":52428800}" >/dev/null; done
echo "hello from $ref" >/tmp/ex_obj_$ref.txt
upcode=$(C -o /dev/null -w '%{http_code}' -X POST "https://$PH/storage/v1/object/documents/readme.txt" -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: text/plain' --data-binary @/tmp/ex_obj_$ref.txt)
bk=$(C "https://$PH/storage/v1/bucket" -H "apikey: $SR" -H "authorization: Bearer $SR" | jget 'len(d)')
R[storage_buckets]="$bk"; R[storage_upload]="HTTP $upcode"

# 6) EDGE FUNCTION: deploy + invoke
src='Deno.serve((req) => new Response(JSON.stringify({ ok: true, fn: "hello", from: "'$ref'" }), { headers: { "content-type": "application/json" } }));'
fpayload=$(python3 -c "import json,sys;print(json.dumps({'name':'hello','entrypoint':'index.ts','verify_jwt':False,'source':sys.argv[1]}))" "$src")
dcode=$(curl -s -o /tmp/ex_fn_$ref.json -w '%{http_code}' -H "authorization: Bearer $TOK" -X POST "$API/v1/projects/$ref/functions" -H 'content-type: application/json' -d "$fpayload")
sleep 3
inv=$(C -X POST "https://$PH/functions/v1/hello" -H "apikey: $SR" -H "authorization: Bearer $SR" -H 'content-type: application/json' -d '{}')
R[fn_deploy]="HTTP $dcode"; R[fn_invoke]=$(echo "$inv" | jget '"ok" if d.get("ok") else "resp:"+str(d)[:30]' 2>/dev/null || echo "$inv" | head -c 30)

# 7) REALTIME: container up + table in publication
rt=$(docker ps --format '{{.Names}} {{.Status}}' | grep "$ref-realtime" | grep -c Up)
pub=$(docker exec $ref-db-1 sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -U supabase_admin -d postgres -tAc "select count(*) from pg_publication_tables where pubname='"'"'supabase_realtime'"'"' and tablename='"'"'notes'"'"';"' 2>/dev/null|tr -d ' ')
R[realtime_up]="$rt"; R[realtime_publication]="$pub"

# ---- matrix ----
echo "---- $ref surface matrix ----"
for k in db_apply extensions rls_enabled trigger auth_users auth_signin rest_alice rest_anon rest_service rest_bob rest_fk_insert graphql storage_buckets storage_upload fn_deploy fn_invoke realtime_up realtime_publication; do
  printf '  %-22s %s\n' "$k" "${R[$k]}"
done
