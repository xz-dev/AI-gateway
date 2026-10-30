# Agent guide

- Deploy, update or operate this stack: load `.agents/skills/ai-gateway-ops`.
- Change Sub2API accounts, groups or pool settings: load `.agents/skills/sub2api-ops`.
- Details live in `docs/`; change records live in `docs/records/`.
- Planned changes use OpenSpec (`openspec/`). Generate the OpenSpec workflow skills for your agent with `openspec init`, and validate with `openspec validate --all --strict`.
- Never put private addresses, hostnames, keys or private config in tracked files.
