#!/usr/bin/env bash
set -uo pipefail
API=https://api.supadupa.brotechlabs.com
APPS=apps.supadupa.brotechlabs.com
login(){ curl -s -X POST $API/v1/auth/login -H 'content-type: application/json' -d '{"email":"admin@supadupa.brotechlabs.com","password":"IQ8uKZdhWwOsqDpnBfjYo0cxcIcVJb9L"}' | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))'; }
TOK=$(login); A(){ curl -s -H "authorization: Bearer $TOK" "$@"; }
HOST=$(A $API/v1/hosts | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d[0]["id"] if d else "")')
ORG=$(A -X POST $API/v1/orgs -H 'content-type: application/json' -d '{"name":"Forge Org"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
echo "host=$HOST org=$ORG"
for ref in acme-prod acme-staging forge-alpha forge-beta; do
  TOK=$(login)
  body=$(python3 -c "import json;print(json.dumps({'ref':'$ref','name':'$ref','host_id':'$HOST','domain':'$APPS','profile':'full','resource_tier':'small','stack_version':'latest'}))")
  code=$(curl -s -o /tmp/ex_$ref.json -w '%{http_code}' --max-time 900 -H "authorization: Bearer $TOK" -X POST $API/v1/orgs/$ORG/projects -H 'content-type: application/json' -d "$body")
  echo "create $ref -> HTTP $code"
done
echo "ORG=$ORG" > /tmp/exercise_org.txt
echo "all created"
