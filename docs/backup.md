# Backup & restore

Everything is in `DATA_DIR` (`./data` with the compose file):

```
data/
├── pocket-invoicing.db      # SQLite database (WAL mode: also -wal/-shm while running)
├── uploads/logo.png         # your logo
└── backups/                 # created by the backup command
```

## Taking a backup

Never copy the `.db` file while the app is running — WAL mode means the copy can be inconsistent. Use one of:

- **UI:** Settings → Backup → *Download database*
- **API:** `curl -H "Authorization: Bearer pi_…" -o backup.db https://…/api/v1/backup`
- **CLI inside the container:**
  ```bash
  docker compose exec app pocket-invoicing backup -o /data/backups/$(date +%F).db
  ```
- **Stopped container:** plain `cp` of the file is fine.

All of these use SQLite's `VACUUM INTO`, which produces a consistent single-file snapshot.

### Cron example (host)
```cron
0 3 * * * docker compose -f /opt/pocket-invoicing/docker-compose.yml exec -T app pocket-invoicing backup -o /data/backups/nightly-$(date +\%u).db
```
Then let restic/borg/Duplicati/rclone pick up `data/backups/` and `data/uploads/`.

## Restoring

```bash
docker compose stop app
cp backup.db data/pocket-invoicing.db
rm -f data/pocket-invoicing.db-wal data/pocket-invoicing.db-shm
docker compose start app
```

## Migrating between hosts / versions

Copy `data/` as a whole. Schema migrations run automatically on start and are forward-only; take a backup before upgrading major versions.

## JSON export

Settings → Backup → *Export JSON* (or `GET /api/v1/export.json`) dumps settings, clients, invoices (with items and payments), recurring profiles, time entries, expenses, catalog, templates, currencies and rates. The SMTP password is excluded. Useful for your own scripts or moving to another tool.
