#!/usr/bin/env python3
"""Import clients, invoices, payments and attached documents from Invoice Ninja v5.

Only the Python standard library is used. Export the data from the Invoice Ninja
MySQL database first (see docs/migrating-from-invoice-ninja.md for the two SQL
queries), copy the `documents/` folder from its storage, then run:

  ./scripts/import_invoiceninja.py --url https://invoices.example.com \
      --email you@example.com --password '...' \
      --invoices in_invoices.json --clients in_clients.json --docs ./documents \
      [--config extras.json] [--dry-run]

`extras.json` lets you set company settings, override clients (contact names,
rates, billing mode), map Invoice Ninja custom fields to Pocket Invoicing custom
fields, choose the line unit per client, and add unbilled time entries.
"""
import argparse
import datetime as dt
import json
import mimetypes
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
from http.cookiejar import CookieJar

COUNTRY = {442: "Luxembourg", 840: "United States", 70: "Bosnia and Herzegovina", 276: "Germany", 826: "United Kingdom", 40: "Austria", 756: "Switzerland", 528: "Netherlands", 56: "Belgium", 250: "France", 380: "Italy", 724: "Spain", 616: "Poland", 203: "Czechia", 191: "Croatia", 688: "Serbia", 705: "Slovenia", 752: "Sweden", 578: "Norway", 208: "Denmark", 246: "Finland", 372: "Ireland", 620: "Portugal", 36: "Australia", 124: "Canada"}


class Api:
    def __init__(self, url, dry=False):
        self.url = url.rstrip("/")
        self.dry = dry
        self.jar = CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))
        self.token = None

    def call(self, method, path, body=None, files=None, quiet=False):
        if self.dry and method != "GET":
            if not quiet:
                print(f"  [dry-run] {method} {path} {json.dumps(body, ensure_ascii=False)[:160] if body else ''}")
            return {"id": 0}
        headers = {}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        if files:
            boundary = uuid.uuid4().hex
            parts = []
            for field, (name, data, ct) in files:
                parts.append((f'--{boundary}\r\nContent-Disposition: form-data; name="{field}"; filename="{name}"\r\nContent-Type: {ct}\r\n\r\n').encode() + data + b"\r\n")
            data = b"".join(parts) + f"--{boundary}--\r\n".encode()
            headers["Content-Type"] = f"multipart/form-data; boundary={boundary}"
        elif body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        else:
            data = None
        req = urllib.request.Request(self.url + path, data=data, method=method, headers=headers)
        try:
            with self.opener.open(req, timeout=60) as resp:
                raw = resp.read()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as e:
            msg = e.read().decode(errors="replace")
            raise SystemExit(f"{method} {path} -> HTTP {e.code}: {msg[:400]}")


def parse_money(v):
    """'17.204,00' or '6.713,568' or '1,234.56' -> float"""
    if v is None:
        return None
    s = str(v).strip()
    if not s or s.lower() == "none":
        return None
    if "," in s and "." in s:
        if s.rfind(",") > s.rfind("."):
            s = s.replace(".", "").replace(",", ".")
        else:
            s = s.replace(",", "")
    elif "," in s:
        s = s.replace(",", ".")
    try:
        return float(s)
    except ValueError:
        return None


def fix_period(cv4, issue_date):
    """IN custom field like '2026-04-01 to 2026-04-30'. Fix obvious wrong years."""
    if not cv4:
        return None, None
    m = re.findall(r"(\d{4}-\d{2}-\d{2})", cv4)
    if len(m) < 2:
        return None, None
    a, b = m[0], m[1]
    year = issue_date[:4]
    def fix(d):
        y = int(d[:4])
        if abs(y - int(year)) > 1:
            return year + d[4:]
        return d
    return fix(a), fix(b)


_rate_cache = {}


def historical_rate(cur, base, date, peg):
    """Return how many `base` units one `cur` is worth on `date`, via frankfurter (ECB).
    `peg` = base units per 1 EUR when the base itself is not on the ECB list (e.g. BAM 1.95583)."""
    key = (cur, base, date)
    if key in _rate_cache:
        return _rate_cache[key]
    try:
        if peg:
            q = f"https://api.frankfurter.app/{date}?from={cur}&to=EUR" if cur != "EUR" else None
            eur = 1.0 if q is None else json.load(urllib.request.urlopen(urllib.request.Request(q, headers={"User-Agent": "pocket-invoicing-import"}), timeout=20))["rates"]["EUR"]
            r = round(eur * peg, 6)
        else:
            q = f"https://api.frankfurter.app/{date}?from={cur}&to={base}"
            r = json.load(urllib.request.urlopen(urllib.request.Request(q, headers={"User-Agent": "pocket-invoicing-import"}), timeout=20))["rates"][base]
    except Exception as e:  # noqa: BLE001
        print(f"  ! no historical rate for {cur}->{base} on {date}: {e}")
        r = 0
    _rate_cache[key] = r
    return r


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", required=True)
    ap.add_argument("--email")
    ap.add_argument("--password")
    ap.add_argument("--token")
    ap.add_argument("--invoices", required=True)
    ap.add_argument("--clients", required=True)
    ap.add_argument("--docs", help="folder with the IN documents (files named NUMBER__original.pdf or a map.tsv)")
    ap.add_argument("--config", help="extras JSON")
    ap.add_argument("--dry-run", action="store_true")
    a = ap.parse_args()

    cfg = json.load(open(a.config)) if a.config else {}
    api = Api(a.url, a.dry_run)
    if a.token:
        api.token = a.token
    else:
        api.call("POST", "/api/v1/auth/login", {"email": a.email, "password": a.password}, quiet=True) if not a.dry_run else None
    me = api.call("GET", "/api/v1/auth/me")
    print(f"Connected as {me.get('email')}")

    # 1. settings
    settings = api.call("GET", "/api/v1/settings")
    settings.update(cfg.get("settings", {}))
    api.call("PUT", "/api/v1/settings", settings)
    base = settings["base_currency"]
    peg = cfg.get("base_eur_peg")  # e.g. 1.95583 for BAM
    print(f"Settings saved (base currency {base})")

    # 2. currencies & manual rates
    for code in cfg.get("enable_currencies", []):
        api.call("PUT", f"/api/v1/currencies/{code}", {"enabled": True})
    for quote, rate in cfg.get("manual_rates", {}).items():
        api.call("PUT", "/api/v1/rates", {"base": base, "quote": quote, "rate": rate})
    try:
        r = api.call("POST", "/api/v1/rates/refresh")
        print(f"Rates refreshed: {r}")
    except SystemExit as e:
        print(f"  ! rate refresh skipped: {e}")

    # 3. clients
    in_clients = json.load(open(a.clients))
    overrides = {int(k): v for k, v in cfg.get("clients", {}).items()}
    existing = {c["name"]: c for c in api.call("GET", "/api/v1/clients?archived=1")}
    client_map = {}
    for c in in_clients:
        ov = overrides.get(c["id"], {})
        contact = (c.get("contacts") or [{}])[0]
        body = {
            "name": ov.get("name", c["name"]), "contact_name": ov.get("contact_name", ""), "email": ov.get("email", contact.get("email") or ""),
            "phone": c.get("phone") or "", "address1": c.get("address1") or "", "address2": c.get("address2") or "", "city": c.get("city") or "",
            "state": c.get("state") or "", "postal_code": c.get("postal_code") or "", "country": ov.get("country", COUNTRY.get(c.get("country_id"), "")),
            "tax_id": c.get("vat_number") or "", "website": c.get("website") or "", "currency": c.get("currency") or base,
            "billing_mode": ov.get("billing_mode", "hourly"), "default_rate": ov.get("default_rate", 0), "payment_terms_days": ov.get("payment_terms_days", 0), "notes": ov.get("notes", ""),
        }
        if body["name"] in existing:
            cl = api.call("PUT", f"/api/v1/clients/{existing[body['name']]['id']}", body)
            print(f"Client updated: {body['name']}")
        else:
            cl = api.call("POST", "/api/v1/clients", body)
            print(f"Client created: {body['name']}")
        client_map[c["id"]] = cl["id"]

    # 4. invoices
    docs = {}
    if a.docs and os.path.isdir(a.docs):
        for fn in os.listdir(a.docs):
            if "__" in fn:
                docs.setdefault(fn.split("__", 1)[0], []).append(os.path.join(a.docs, fn))
    cf_map = cfg.get("custom_field_map", {})  # {"cv1": "pfr_broj_racuna"}
    units = cfg.get("unit_by_client", {})
    have = {i["number"] for i in api.call("GET", "/api/v1/invoices?limit=1000")["items"]}
    in_invoices = sorted(json.load(open(a.invoices)), key=lambda r: (r["date"], r["id"]))
    created = skipped = 0
    for r in in_invoices:
        if r.get("deleted"):
            continue
        if r["number"] in have:
            skipped += 1
            continue
        cid = client_map.get(r["client_id"])
        if not cid:
            print(f"  ! {r['number']}: unknown client {r['client_id']}, skipped")
            continue
        unit = units.get(str(r["client_id"]), "unit")
        currency = next((c.get("currency") for c in in_clients if c["id"] == r["client_id"]), base)
        items = []
        for it in r.get("items") or []:
            qty = float(it.get("quantity") or 0)
            cost = float(it.get("cost") or 0)
            if qty == 0 and cost == 0:
                continue
            unit_code = str(it.get("unit_code") or "").lower()
            line_unit = unit_code if unit_code in ("hour", "day", "month", "unit", "fixed") else unit
            items.append({"description": (it.get("notes") or it.get("product_key") or "").strip(), "unit": line_unit, "quantity": qty, "unit_price": cost, "tax_rate": float(it.get("tax_rate1") or 0), "discount": float(it.get("discount") or 0) if not it.get("is_amount_discount") else 0})
        ps, pe = fix_period(r.get("cv4"), r["date"])
        custom = {}
        for src, key in cf_map.items():
            v = r.get(src)
            if v and str(v).strip() and str(v).strip().lower() != "none":
                custom[key] = str(v).strip()
        # exchange rate to base: prefer the "total in base" custom field, else ECB history
        xr = 0
        tb_field = cfg.get("total_in_base_field")
        tb = parse_money(r.get(tb_field)) if tb_field else None
        if tb and float(r["amount"]):
            xr = round(tb / float(r["amount"]), 6)
        elif currency != base:
            xr = historical_rate(currency, base, r["date"], peg) or 0
        body = {
            "number": r["number"], "client_id": cid, "status": "sent" if int(r["status"]) >= 2 else "draft", "issue_date": r["date"], "due_date": str(r.get("due") or r["date"])[:10],
            "currency": currency, "exchange_rate": xr, "billing_mode": {"day": "daily", "hour": "hourly", "month": "monthly"}.get(unit, "fixed"),
            "period_start": ps, "period_end": pe, "po_number": r.get("po") or "", "discount_type": "none", "discount_value": 0,
            "notes": cfg.get("keep_public_notes") and (r.get("notes") or "") or "", "terms": "", "footer": settings.get("default_footer", ""),
            "items": items, "custom_fields": custom,
        }
        inv = api.call("POST", "/api/v1/invoices", body)
        paid = float(r.get("paid") or 0)
        if paid > 0 and inv.get("id"):
            api.call("POST", "/api/v1/payments", {"invoice_id": inv["id"], "amount": paid, "currency": currency, "date": r["date"], "method": "bank_transfer", "reference": "Imported from Invoice Ninja"})
        for path in docs.get(r["number"], []):
            data = open(path, "rb").read()
            name = os.path.basename(path).split("__", 1)[1]
            if inv.get("id"):
                api.call("POST", f"/api/v1/invoices/{inv['id']}/attachments", files=[("file", (name, data, mimetypes.guess_type(name)[0] or "application/pdf"))])
        created += 1
        print(f"Invoice {r['number']}: {len(items)} lines, {currency} {r['amount']}, paid {paid}, xr {xr}, docs {len(docs.get(r['number'], []))}")
    print(f"Invoices: {created} created, {skipped} already present")

    # 5. extras: products, time entries, recurring
    for p in cfg.get("products", []):
        api.call("POST", "/api/v1/products", p)
    for t in cfg.get("time_entries", []):
        t = dict(t)
        t["client_id"] = client_map.get(t.pop("in_client_id"))
        api.call("POST", "/api/v1/time", t)
        print(f"Time entry: {t.get('description', '')[:50]} ({t.get('duration_minutes')} min)")
    for rp in cfg.get("recurring", []):
        rp = dict(rp)
        rp["client_id"] = client_map.get(rp.pop("in_client_id"))
        api.call("POST", "/api/v1/recurring", rp)
        print(f"Recurring profile: {rp['name']}")
    if "settings_after" in cfg:
        settings = api.call("GET", "/api/v1/settings")
        settings.update(cfg["settings_after"])
        api.call("PUT", "/api/v1/settings", settings)
    print("Done.")


if __name__ == "__main__":
    main()
