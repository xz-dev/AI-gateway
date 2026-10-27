# One-way relay and netns owner

**Where:** YAML anchors `x-socat-relay` (`&socat-relay`) and `x-netns-owner` (`&netns-owner`) at the top of [`compose.yaml`](../../../compose.yaml). This directory has no build context. The anchors stay in `compose.yaml` because Compose cannot include anchors from another file.

**What:**

- **netns owner** (`*-netns` services): a minimal Alpine container that owns a network namespace, deletes every IPv4/IPv6 default route, then sleeps as `nobody`. Application containers join it with `network_mode: service:<name>-netns`, so they have no Internet route.
- **socat relay** (`*-relay` services): an explicit-version `alpine/socat` process that listens only on its own source-side address (`bind=`) and forwards to one target. Source and target sit on disjoint internal networks, so each TCP edge has a direction: the target cannot open a new connection back toward the source.

**How:**

- Add an edge: copy an existing `*-relay` service with `<<: *socat-relay`, give it one `*-source` and one `*-target` network, and bind the listener to the source address.
- Security model: [security](../../../docs/security.md).
- Tests: [`test-socat-boundary.sh`](../../../ops/scripts/tests/test-socat-boundary.sh) (relay boundary) and [`test-netns-guard.sh`](../../../ops/scripts/tests/test-netns-guard.sh) (route-stripped namespaces). Both run from [`validate.sh`](../../../ops/scripts/validate.sh).
