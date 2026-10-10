-- Exchange rates are only meaningful against the current base currency. Rows fetched
-- while a different base was active (for example EUR-based ECB rows left over from
-- before switching the base to BAM) were invisible on the Currencies page and could
-- feed wrong cross rates. Drop them; the next refresh fills in the current base.
DELETE FROM exchange_rates
WHERE base != COALESCE((SELECT json_extract(value, '$.base_currency') FROM settings WHERE key = 'app'), base);
