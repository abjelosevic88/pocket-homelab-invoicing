# Reverse proxy & SSO

Set on the app:

```env
BASE_URL=https://invoices.example.com
SECURE_COOKIES=true
TRUST_PROXY=true     # default
```

Ready-made snippets live in `deploy/reverse-proxy/`:

- `Caddyfile` — Caddy with automatic HTTPS (and an Authelia `forward_auth` variant)
- `traefik-labels.yml` — labels for the compose service
- `nginx.conf`
- `nginx-proxy-manager.md`

## SSO / forward auth (Authelia, Authentik, oauth2-proxy, Pocket ID…)

1. Protect the host with your provider's middleware.
2. Make sure the proxy forwards the identity headers (`Remote-User`, `Remote-Email` by default; change with `AUTH_PROXY_HEADER` / `AUTH_PROXY_EMAIL_HEADER`).
3. Set `AUTH_MODE=proxy`. Users are created automatically on first visit; the login page is bypassed.
4. **Exclude** these paths from the auth middleware so clients can open their invoice links and your monitoring can hit health checks:
   - `/i/*` and `/api/public/*` — public invoice links
   - `/healthz`, `/readyz`, `/metrics` (optional)

Authentik example (Proxy Provider → *Unauthenticated paths*):
```
^/i/.*
^/api/public/.*
^/healthz
```

## No reverse proxy (LAN only)

Works out of the box over plain HTTP. Keep `AUTH_MODE=local` (the built-in login) unless the network is fully trusted; `AUTH_MODE=none` disables authentication entirely.

## Tailscale / VPN

Expose port 8080 on the tailnet; with `tailscale serve` you get HTTPS automatically:
```bash
tailscale serve --bg 8080
```
Set `BASE_URL` to the `https://<machine>.<tailnet>.ts.net` address so emailed links work. Note that clients outside the tailnet won't be able to open public links — email the PDF instead (it's attached anyway).
