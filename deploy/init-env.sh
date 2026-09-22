#!/bin/sh
set -eu
host=${1:-127.0.0.1}
case "$host" in *[!a-zA-Z0-9.-]*|'') echo 'Specify a hostname or IPv4 address.' >&2; exit 1;; esac
if [ -e .env ]; then echo '.env already exists; preserve its secrets.' >&2; exit 1; fi
umask 077
{
  printf 'POSTGRES_DB=xbrawl\nPOSTGRES_USER=xbrawl\n'
  printf 'POSTGRES_PASSWORD=%s\n' "$(openssl rand -hex 32)"
  printf 'XBRAWL_TOKEN_SECRET=%s\n' "$(openssl rand -hex 32)"
  printf 'XBRAWL_INTERNAL_SECRET=%s\n' "$(openssl rand -hex 32)"
  printf 'XBRAWL_BATTLE_ADVERTISED_HOST=%s\n' "$host"
  printf 'XBRAWL_BATTLE_ADVERTISED_PORT=9610\nMAIN_TCP_PORT=9609\nBATTLE_UDP_PORT=9610\nBIND_HOST=0.0.0.0\n'
} > .env
echo 'Created .env. Keep this file private and include it in your backup.'
