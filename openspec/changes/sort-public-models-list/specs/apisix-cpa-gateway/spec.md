## ADDED Requirements

### Requirement: Public plain model list order

The front-door APISIX SHALL return the plain public model list in the catalog output order. This covers `GET /models` and `GET /v1/models` without a non-empty `client_version`, both answered from Sub2API. The order is:
1. IDs without `/`, sorted by Unicode code point;
2. then every other ID, sorted by Unicode code point.

Comparison SHALL be exact and case-sensitive, with no locale or case folding. Entries with equal IDs SHALL keep Sub2API's relative order.

The front door SHALL NOT add, remove or alter any entry. Each entry SHALL be emitted byte-for-byte as Sub2API returned it, and every other top-level field of the response SHALL be kept. The request SHALL reach Sub2API with the caller's path, query and credentials, and it SHALL share the existing front-door admission slot for external model-list requests.

A non-2xx Sub2API response SHALL pass through with its status and body unchanged. A 2xx body that the front door cannot recognize SHALL also pass through with its status and body unchanged and SHALL be logged at WARN. Neither case SHALL be turned into a gateway error. A body is not recognized when it is not a JSON object with exactly one `data` or `models` array of objects whose identities (`id`, or `slug` for `models`) are non-empty strings without conflicting `id`/`slug` values.

A reordered `200` SHALL carry:
- `Cache-Control: private, no-cache`;
- a strong ETag computed over the final body;
- `Vary` that includes `Authorization`.

It SHALL honor `If-None-Match` with `304` and a failed `If-Match` with `412`.

The parameterized Codex manifest SHALL keep the order of model-catalog's response.

#### Scenario: Bare IDs first, code-point order

- **WHEN** Sub2API returns `data` with IDs `z/a`, `b`, `A/b`, `B`, `a`, `a/b` in that order
- **THEN** the client receives them as `B`, `a`, `b`, `A/b`, `a/b`, `z/a`, each entry byte-identical to Sub2API's

#### Scenario: Non-ASCII IDs use code-point order

- **WHEN** Sub2API returns IDs `é`, `z`, `😀` and `Z`
- **THEN** the client receives `Z`, `z`, `é`, `😀`

#### Scenario: Same members as Sub2API

- **WHEN** any recognized 2xx list is reordered
- **THEN** the set of IDs, the number of entries, every entry's bytes and every other top-level field equal Sub2API's response

#### Scenario: Sub2API error passes through

- **WHEN** Sub2API answers `401`, `404`, `429` or `5xx`
- **THEN** the front door returns that status and body, and the existing public error shaping still applies

#### Scenario: Unrecognized body passes through

- **WHEN** Sub2API answers `200` with a body that is not one recognizable model collection
- **THEN** the client receives that status and body unchanged, and a WARN is logged

#### Scenario: Conditional request on the ordered list

- **WHEN** a client repeats the request with `If-None-Match` set to the ETag of the ordered list
- **THEN** it receives `304` with an empty body

#### Scenario: Codex manifest order unchanged

- **WHEN** a client requests `GET /v1/models?client_version=1.0.0`
- **THEN** the intersection keeps model-catalog's order and is not reordered again by the front door
