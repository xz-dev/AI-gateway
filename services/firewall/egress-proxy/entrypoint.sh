#!/bin/sh
set -eu

ssl_db=${SQUID_SSL_DB:-/var/lib/squid/ssl_db/db}
if [ ! -f "$ssl_db/index.txt" ]; then
  /usr/lib/squid/security_file_certgen -c -s "$ssl_db" -M 16MB
fi

/usr/sbin/unbound -d -c /etc/unbound/ai-gateway.conf &
unbound_pid=$!
"$@" &
squid_pid=$!

# shellcheck disable=SC2317,SC2329 # invoked by the signal trap below
stop() {
  trap - HUP INT TERM
  kill -TERM "$squid_pid" "$unbound_pid" 2>/dev/null || true
  wait "$squid_pid" 2>/dev/null || true
  wait "$unbound_pid" 2>/dev/null || true
  exit 0
}
trap stop HUP INT TERM

while kill -0 "$unbound_pid" 2>/dev/null && kill -0 "$squid_pid" 2>/dev/null; do
  sleep 1
done
kill -TERM "$squid_pid" "$unbound_pid" 2>/dev/null || true
wait "$squid_pid" 2>/dev/null || true
wait "$unbound_pid" 2>/dev/null || true
exit 1
