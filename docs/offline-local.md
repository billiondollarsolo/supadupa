# Offline Local Edge

Offline local edge mode runs Supadupa with local TLS and local DNS instead of public DNS and Let's Encrypt. Use it for demos, development, and single-node testing when the machine is not publicly reachable.

## What It Provides

Offline mode:

- Generates a local CA.
- Generates a server certificate for control-plane hosts and the wildcard apps domain.
- Writes a Traefik default certificate file-provider config.
- Disables the Traefik ACME cert resolver by setting `SUPADUPA_TLS_CERT_RESOLVER=`.
- Binds edge ports to loopback.
- Generates local DNS helper files.

It does not provide browser-trusted public TLS unless you trust the generated local CA on your workstation.

## Quick Start

```bash
scripts/setup-compose.sh --mode offline --bootstrap-password 'change-this-password'
scripts/setup-local-dns.sh --domain supadupa.test
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

Open:

```text
https://admin.supadupa.test
```

## Generated TLS Files

```text
runtime/certs/local/supadupa-local-ca.crt
runtime/certs/local/supadupa-local-ca.key
runtime/certs/local/supadupa-local.crt
runtime/certs/local/supadupa-local.key
runtime/routes/00-local-tls.yaml
```

The server certificate includes SANs for:

```text
admin.supadupa.test
api.supadupa.test
apps.supadupa.test
*.apps.supadupa.test
```

Trust `runtime/certs/local/supadupa-local-ca.crt` in your OS/browser if you want normal browser trust.

## Local DNS

Generate helper files:

```bash
scripts/setup-local-dns.sh --domain supadupa.test
```

This writes:

```text
runtime/local-dns/supadupa-dnsmasq.conf
runtime/local-dns/supadupa-hosts
```

The dnsmasq file supports wildcard project DNS:

```text
address=/admin.supadupa.test/127.0.0.1
address=/api.supadupa.test/127.0.0.1
address=/.apps.supadupa.test/127.0.0.1
```

Use it with dnsmasq, then configure your OS resolver to send `supadupa.test` queries to dnsmasq.

## Hosts File Fallback

`/etc/hosts` does not support wildcard DNS. Use explicit project refs:

```bash
scripts/setup-local-dns.sh --domain supadupa.test --refs smoke,alpha
```

That generates entries for:

```text
admin.supadupa.test
api.supadupa.test
smoke.apps.supadupa.test
studio-smoke.apps.supadupa.test
storage-smoke.apps.supadupa.test
db-smoke.apps.supadupa.test
pooler-smoke.apps.supadupa.test
```

Install manually, or run as root:

```bash
sudo scripts/setup-local-dns.sh --domain supadupa.test --refs smoke --install-hosts
```

## dnsmasq Install Helper

If dnsmasq is installed, this copies the generated config to `/etc/dnsmasq.d/supadupa.conf`:

```bash
sudo scripts/setup-local-dns.sh --domain supadupa.test --install-dnsmasq
```

You may still need to configure the OS resolver to use dnsmasq for `supadupa.test`.

## Limitations

- This is not hosted-grade TLS.
- The local CA must be trusted per workstation.
- Wildcard project DNS requires dnsmasq or another local DNS server.
- `/etc/hosts` works only for explicit project refs.
- Offline mode binds edge ports to `127.0.0.1` by default.
