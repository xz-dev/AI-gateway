#!/usr/bin/env python3
import hashlib
import ipaddress
import json
import re
import sys
from pathlib import Path

SERVICE_RE = re.compile(r"[a-z][a-z0-9-]{0,31}$")
DOMAIN_RE = re.compile(r"(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")
METHOD_RE = re.compile(r"[A-Z]{3,12}$")
BLOCKED_IPV4_CIDRS = (
    "0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
    "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
    "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24",
    "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
)
# Squid internally represents IPv4 as mapped IPv6, so broad IPv6 complement
# ranges would also deny ordinary IPv4. Keep special-use IPv6 ranges explicit;
# mapped IPv4 answers are normalized and checked by BLOCKED_IPV4_CIDRS.
BLOCKED_IPV6_CIDRS = (
    "::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64",
    "2001::/23", "2001:db8::/32", "2002::/16", "3ffe::/16", "3fff::/20", "5f00::/16",
    "fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
)


def die(message: str) -> None:
    raise SystemExit(message)


def domain(value: object, where: str) -> str:
    if not isinstance(value, str):
        die(f"{where}: domain must be a string")
    value = value.strip().lower().rstrip(".")
    base = value[1:] if value.startswith(".") else value
    if not DOMAIN_RE.fullmatch(base):
        die(f"{where}: invalid domain {value!r}")
    try:
        ipaddress.ip_address(base)
    except ValueError:
        return value
    die(f"{where}: IP literals are forbidden")


def write_lines(path: Path, values: list[str] | tuple[str, ...]) -> None:
    path.write_text("".join(f"{value}\n" for value in values), encoding="utf-8")
    path.chmod(0o644)


def main() -> None:
    if len(sys.argv) != 3:
        die(f"usage: {sys.argv[0]} POLICY.json OUTPUT_DIR")
    policy_path = Path(sys.argv[1])
    output = Path(sys.argv[2])
    raw = policy_path.read_bytes()
    try:
        policy = json.loads(raw)
    except json.JSONDecodeError as error:
        die(f"{policy_path}: {error}")
    services = policy.get("services") if isinstance(policy, dict) else None
    if not isinstance(services, dict) or not services:
        die("policy.services must be a non-empty object")

    parsed = []
    listeners = set()
    for index, (name, spec) in enumerate(services.items()):
        where = f"services.{name}"
        if not isinstance(name, str) or not SERVICE_RE.fullmatch(name):
            die(f"invalid service name {name!r}")
        if not isinstance(spec, dict):
            die(f"{where} must be an object")
        listen = spec.get("listen")
        if not isinstance(listen, str) or listen.count(":") != 1:
            die(f"{where}.listen must be private IPv4:port")
        listen_ip_text, port_text = listen.split(":")
        try:
            listen_ip = ipaddress.ip_address(listen_ip_text)
            port = int(port_text)
        except ValueError:
            die(f"{where}.listen must be private IPv4:port")
        if listen_ip.version != 4 or not listen_ip.is_private or not 1024 <= port <= 65535:
            die(f"{where}.listen must be private IPv4 and an unprivileged port")
        if listen in listeners:
            die(f"duplicate listener {listen}")
        listeners.add(listen)
        try:
            client = ipaddress.ip_network(spec.get("client"), strict=True)
        except (TypeError, ValueError):
            die(f"{where}.client must be a private IPv4 /32")
        if client.version != 4 or client.prefixlen != 32 or not client.network_address.is_private:
            die(f"{where}.client must be a private IPv4 /32")
        destinations = spec.get("destinations")
        if not isinstance(destinations, list):
            die(f"{where}.destinations must be an array")
        seen = {}
        entries = []
        for destination_index, item in enumerate(destinations):
            item_where = f"{where}.destinations[{destination_index}]"
            if not isinstance(item, dict):
                die(f"{item_where} must be an object")
            host = domain(item.get("domain"), item_where)
            mode = item.get("tls")
            if mode not in ("bump", "splice"):
                die(f"{item_where}.tls must be bump or splice")
            if host in seen and seen[host] != mode:
                die(f"{item_where}: domain {host} cannot mix bump and splice")
            seen[host] = mode
            methods = item.get("methods", [])
            paths = item.get("paths", [])
            if mode == "splice" and (methods or paths):
                die(f"{item_where}: splice destinations cannot inspect methods or paths")
            if mode == "bump":
                if not isinstance(methods, list) or not methods or any(not isinstance(method, str) or not METHOD_RE.fullmatch(method) for method in methods):
                    die(f"{item_where}.methods must contain uppercase HTTP methods")
                methods = list(dict.fromkeys(methods))
                if not isinstance(paths, list) or not paths:
                    die(f"{item_where}.paths must contain anchored POSIX regular expressions")
                for path in paths:
                    if not isinstance(path, str) or not path.startswith("^") or any(char in path for char in "\r\n\0") or "(?" in path or len(path) > 512:
                        die(f"{item_where}: invalid path expression {path!r}")
            entries.append({"domain": host, "tls": mode, "methods": methods, "paths": paths})
        parsed.append({"index": index, "name": name, "listen": listen, "client": str(client), "entries": entries})

    output.mkdir(parents=True, exist_ok=True)
    if output.is_symlink():
        die(f"refusing symlink output directory: {output}")
    # The host parent stays mode 0700; the bind-mounted directory itself must
    # be traversable by Squid's unprivileged container UID.
    output.chmod(0o755)
    for old in output.iterdir():
        if old.is_file() and (old.name == "squid.conf" or old.name.startswith("policy-")):
            old.unlink()

    config = [
        "# Generated by scripts/render-egress-policy.py; edit policy.json, not this file.",
        f"# policy-sha256: {hashlib.sha256(raw).hexdigest()}",
        "visible_hostname ai-gateway-egress-proxy",
        "pid_filename /tmp/squid.pid",
        "coredump_dir /tmp",
        "pinger_enable off",
        "workers 1",
        "",
    ]
    for service in parsed:
        config.append(
            f"http_port {service['listen']} name={service['name']} ssl-bump "
            "cert=/etc/squid/ca.crt key=/etc/squid/ca.key "
            "generate-host-certificates=on dynamic_cert_mem_cache_size=16MB"
        )
    config += [
        "",
        "sslcrtd_program /usr/lib/squid/security_file_certgen -s /var/lib/squid/ssl_db/db -M 16MB",
        "sslcrtd_children 4 startup=1 idle=1",
        "dns_nameservers 127.0.0.1",
        "tls_outgoing_options cafile=/etc/ssl/certs/ca-certificates.crt",
        "sslproxy_cert_error deny all",
        "external_acl_type exact_host ttl=0 negative_ttl=0 children-max=4 children-startup=1 children-idle=1 concurrency=0 %ssl::>sni %>rd /usr/local/bin/ai-gateway-exact-host-helper",
        "acl exact_tls_host external exact_host",
        "acl step1 at_step SslBump1",
        "acl SSL_ports port 443",
        "acl CONNECT method CONNECT",
        'acl blocked_dst4 dst "/etc/squid/generated/policy-blocked-ipv4-cidrs"',
        'acl blocked_dst6 dst "/etc/squid/generated/policy-blocked-ipv6-cidrs"',
        "",
    ]
    write_lines(output / "policy-blocked-ipv4-cidrs", BLOCKED_IPV4_CIDRS)
    write_lines(output / "policy-blocked-ipv6-cidrs", BLOCKED_IPV6_CIDRS)

    for service in parsed:
        prefix = f"policy_{service['index']}"
        config += [
            f"acl {prefix}_port myportname {service['name']}",
            f"acl {prefix}_client src {service['client']}",
        ]
        entries = service["entries"]
        if entries:
            write_lines(output / f"policy-{service['index']}-connect-domains", list(dict.fromkeys(entry["domain"] for entry in entries)))
            config.append(f'acl {prefix}_connect dstdomain -n "/etc/squid/generated/policy-{service["index"]}-connect-domains"')
        for mode in ("splice", "bump"):
            domains = list(dict.fromkeys(entry["domain"] for entry in entries if entry["tls"] == mode))
            if domains:
                write_lines(output / f"policy-{service['index']}-{mode}-domains", domains)
                config.append(f'acl {prefix}_{mode}_sni ssl::server_name --client-requested "/etc/squid/generated/policy-{service["index"]}-{mode}-domains"')
        for destination_index, entry in enumerate(entries):
            if entry["tls"] != "bump":
                continue
            acl = f"{prefix}_destination_{destination_index}"
            write_lines(output / f"policy-{service['index']}-{destination_index}-domain", [entry["domain"]])
            write_lines(output / f"policy-{service['index']}-{destination_index}-paths", entry["paths"])
            config += [
                f'acl {acl}_host dstdomain -n "/etc/squid/generated/policy-{service["index"]}-{destination_index}-domain"',
                f"acl {acl}_method method {' '.join(entry['methods'])}",
                f'acl {acl}_path urlpath_regex "/etc/squid/generated/policy-{service["index"]}-{destination_index}-paths"',
            ]
        config.append("")

    config += [
        "ssl_bump peek step1",
        "ssl_bump terminate to_localhost",
        "ssl_bump terminate to_linklocal",
        "ssl_bump terminate blocked_dst4",
        "ssl_bump terminate blocked_dst6",
    ]
    for service in parsed:
        prefix = f"policy_{service['index']}"
        if any(entry["tls"] == "splice" for entry in service["entries"]):
            config.append(f"ssl_bump splice {prefix}_port {prefix}_client exact_tls_host {prefix}_splice_sni")
        if any(entry["tls"] == "bump" for entry in service["entries"]):
            config.append(f"ssl_bump bump {prefix}_port {prefix}_client exact_tls_host {prefix}_bump_sni")
    config += ["ssl_bump terminate all", ""]

    for service in parsed:
        prefix = f"policy_{service['index']}"
        config += [
            f"http_access deny {prefix}_port !{prefix}_client",
            f"http_access deny {prefix}_port CONNECT !SSL_ports",
            f"http_access deny {prefix}_port to_localhost",
            f"http_access deny {prefix}_port to_linklocal",
            f"http_access deny {prefix}_port blocked_dst4",
            f"http_access deny {prefix}_port blocked_dst6",
        ]
        if service["entries"]:
            config.append(f"http_access allow {prefix}_port {prefix}_client CONNECT {prefix}_connect")
        for destination_index, entry in enumerate(service["entries"]):
            if entry["tls"] != "bump":
                continue
            acl = f"{prefix}_destination_{destination_index}"
            config.append(f"http_access allow {prefix}_port {prefix}_client exact_tls_host {acl}_host {acl}_method {acl}_path")
        config += [f"http_access deny {prefix}_port", ""]
    config.append("http_access deny all")
    config.append("")

    for spelling in ("WebSocket", "websocket"):
        for service in parsed:
            prefix = f"policy_{service['index']}"
            for destination_index, entry in enumerate(service["entries"]):
                if entry["tls"] == "bump" and "GET" in entry["methods"]:
                    acl = f"{prefix}_destination_{destination_index}"
                    config.append(f"http_upgrade_request_protocols {spelling} allow {prefix}_port {prefix}_client exact_tls_host {acl}_host {acl}_path")
        config.append(f"http_upgrade_request_protocols {spelling} deny all")
    config += [
        "http_upgrade_request_protocols OTHER deny all",
        "",
        "cache deny all",
        "cache_mem 16 MB",
        "maximum_object_size 0 KB",
        "maximum_object_size_in_memory 0 KB",
        "forwarded_for delete",
        "follow_x_forwarded_for deny all",
        "request_header_access Proxy-Authorization deny all",
        "request_header_access X-Forwarded-For deny all",
        "request_header_access Forwarded deny all",
        "connect_timeout 10 seconds",
        "request_timeout 10 minutes",
        "read_timeout 30 minutes",
        "write_timeout 30 minutes",
        "client_lifetime 24 hours",
        "shutdown_lifetime 3 seconds",
        "half_closed_clients off",
        "positive_dns_ttl 5 minutes",
        "negative_dns_ttl 30 seconds",
        "logfile_rotate 0",
        "logformat security %ts.%03tu listener=%la:%lp src=%>a method=%rm host=%>rd path=%>rp status=%>Hs sni=%ssl::>sni bump=%ssl::bump_mode upstream=%<a",
        "access_log stdio:/dev/stdout security",
        "cache_log stdio:/dev/stderr",
        "cache_store_log none",
        "",
    ]
    (output / "squid.conf").write_text("\n".join(config), encoding="utf-8")
    (output / "squid.conf").chmod(0o644)


if __name__ == "__main__":
    main()
