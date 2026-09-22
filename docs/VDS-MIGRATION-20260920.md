# VDS migration — 2026-09-20

Game services and PostgreSQL now run on 157.228.155.109 (vm4634613.firstbyte.club).
- Main: TCP 9609; battle: UDP 9610.
- systemd: openfreaks-main, openfreaks-battle.
- Binary/content: /opt/openfreaks/current.
- Config and protected environment: /etc/openfreaks.
- Database: openfreaks_live_20260920; migrated 50 existing accounts before smoke tests.
- Public WSS remains on serbiansubtitles.online/freaks/ws/{main,battle}; nginx on the old web host forwards to new ports 9729/9730. Firewall restricts those upstream ports to 85.137.89.21.
- Web build and HTTPS remain on the old web host. Existing native clients using the old TCP address are forwarded by freaks-main-forward.socket/service.
- Old main/battle are stopped and disabled. Old database retained for rollback; never start the old main concurrently with the new writer.

Validation: full Go tests passed; public WSS registration/match/snapshot/exit smoke passed; 20 concurrent external web test clients passed (35.9 seconds). This is a short concurrency smoke, not a long-duration capacity benchmark.

Fixes: football kills no longer count as goals; endless arena does not retain replay snapshots; main/battle connection closure reasons logged. Client exposes its existing authored exit button in all modes and leaves voluntarily without waiting for match results. Keepalive uses unscaled time.
