#!/bin/sh
set -eu

# Squid sends the actual ClientHello SNI and current request host. Requiring an
# exact pair rejects missing SNI, domain fronting, and a forged decrypted Host.
while IFS=' ' read -r sni host _rest; do
  sni=$(printf '%s' "${sni:--}" | tr '[:upper:]' '[:lower:]')
  host=$(printf '%s' "${host:--}" | tr '[:upper:]' '[:lower:]')
  sni=${sni%.}
  host=${host%.}
  if [ "$sni" != "-" ] && [ "$host" != "-" ] && [ "$sni" = "$host" ]; then
    printf 'OK\n'
  else
    printf 'ERR message=name-mismatch\n'
  fi
done
