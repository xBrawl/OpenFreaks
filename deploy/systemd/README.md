# Native Linux deployment (not installed on VDS yet)

Build `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o openfreaks ./cmd/openfreaks`.
Release directory: `/opt/openfreaks/releases/<version>` with binary and `content` copied from `deploy/content-real`; `current` symlink points at that release.

Use existing unprivileged user `freaks`. Create `/var/lib/openfreaks` owned by freaks. Copy config.systemd.json to `/etc/openfreaks/config.json`. Store environment in `/etc/openfreaks/server.env` (root-owned,0600): PostgreSQL URL, XBRAWL_TOKEN_SECRET, XBRAWL_INTERNAL_SECRET, XBRAWL_BATTLE_ADVERTISED_HOST and XBRAWL_BATTLE_ADVERTISED_PORT=9610. Keep existing token secret when migrating accounts. Do not paste credentials into logs.

Before replacing the C# services: take a database backup; restore it into an isolated PostgreSQL database; test migration and login on separate TCP/UDP/internal ports. Validate 20 concurrent clients on the target host. Only then stop old Freaks services, take a final backup, start openfreaks-battle and openfreaks-main using these units, and check localhost:9710/health and localhost:9709/health plus a live client battle. Never run both main implementations on one database. Other VDS services must remain untouched.

Copy units to /etc/systemd/system, daemon-reload and enable/start only as part of the verified cutover. Rollback after Go accepts writes requires data conversion or restoring the pre-cutover backup, losing later writes. Old C# battle replays cannot be reconstructed by Go.
