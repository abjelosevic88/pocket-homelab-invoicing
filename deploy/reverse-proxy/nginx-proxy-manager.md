# Nginx Proxy Manager

1. **Proxy Hosts → Add Proxy Host**
2. Domain: `invoices.example.com`, Scheme `http`, Forward host: `pocket-invoicing` (container name) or the host IP, Port `8080`
3. Enable **Websockets Support** (harmless) and **Block Common Exploits**
4. SSL tab: request a Let's Encrypt certificate, Force SSL, HTTP/2
5. Set `BASE_URL=https://invoices.example.com` and `SECURE_COOKIES=true` on the container

That's it; the app trusts `X-Forwarded-*` headers by default (`TRUST_PROXY=true`).
