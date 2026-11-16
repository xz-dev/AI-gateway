## ADDED Requirements

### Requirement: Recovery guard install supports systemd and OpenRC hosts

The host recovery guard install SHALL detect whether the host runs systemd or OpenRC, SHALL allow an explicit override, and SHALL refuse any other service manager before writing files. On OpenRC hosts it SHALL install the tracked `ai-gateway-recovery` init script, recovery loop and logrotate file with the deploy root substituted, SHALL enable the service in the default runlevel, and SHALL verify that the service cgroup enforces a 64 MiB memory ceiling and a 10% CPU ceiling. It SHALL NOT leave backup copies in the init-script or logrotate directories. On systemd hosts the existing service and timer behaviour SHALL remain unchanged.

#### Scenario: OpenRC host without delegated controllers

- **WHEN** the recovery install runs on an OpenRC host whose cgroup root does not delegate `cpu` and `memory`, or on an OpenRC LXC where `cgroup-delegate` is not in the boot runlevel
- **THEN** it fails with the cause before writing any file

#### Scenario: Check mode on an OpenRC host

- **WHEN** an operator runs the recovery install in check mode on an OpenRC host
- **THEN** it shows file differences only, and does not write files or start, restart or enable the service

#### Scenario: Reinstall on an already matching OpenRC host

- **WHEN** the tracked files already match the host's files
- **THEN** the install reports no changed files and does not restart the running recovery service
