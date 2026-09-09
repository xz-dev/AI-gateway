# Case-insensitive metadata lookup

The user requested case-insensitive model-ID lookup in the Go enrichment gateway.

## Implemented boundary

`SourceTables.lookupOne` keeps the selected provider/auth namespace and original
index keys. Exact spelling wins. Otherwise `strings.EqualFold` accepts one unique
matching ID; multiple case-equivalent IDs are a lookup miss, never map-order
selection. Both canonical IDs and explicit `lookup_ids` use this rule.

No prefix removal, date/tag/variant rewriting, provider-token normalization,
public-ID changes, membership relaxation or authentication changes. Source-chain
precedence and independent token fields remain unchanged. This does not remove
Commandcode's need for mappings where vendor prefixes differ.

Only exact misses scan that source's existing map. No new dependency or index.

## Verification

- Behavioral red: case-insensitive models.dev/modelparams lookups and the existing
  real merge test failed with the original lookup implementation.
- Race regression: 170 tests/subtests passed; the one failure was the previous
  source-config draft's obsolete assertion that XL cannot have a per-model source.
  Replaced it with the exact approved Muse binding assertion; its race recheck
  passed. Total: 171 passing tests/subtests across these commands.
- Two existing opt-in tests were not run: container HTTP and captured source
  completion. This does not certify the pending XL/Commandcode data completion.
- Tests cover exact-first collisions, ambiguous folded matches, provider/auth
  isolation, missing sources, retained prefixes/dates/tags/variants, explicit zero
  values, and case-insensitive canonical/explicit lookups through real merging
  without changing the public slug.
- `go vet ./...`, formatting and scoped diff checks pass.

Evidence: `/root/.cache/catalog-casefold-20260908/`.

Local implementation only. No image build, production call or deployment in this
slice. Existing source-configuration drafts remain unchanged.
