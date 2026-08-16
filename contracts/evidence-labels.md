# Evidence labels v1

Labels are mandatory and may not be stripped by a renderer, API filter, webhook, warehouse export, or CLI format.

Run-manifest schema `1.1.0` makes `collectionHealth` and `finality` required on every result. This is an explicit schema-version change from the preview's `1.0.0` result shape.

- `basis`: `measured`, `provider_modeled`, or `unknown`. `hidden` is presentation state, never a basis.
- `integrity`: identifies what authenticated or produced a field. Apple-signed JWS fields and unsigned copy-envelope fields are labeled separately.
- `comparability`: `exact`, `bounded`, `directional`, or `none`, and exists only under a versioned comparison contract.
- `collectionHealth`: whether collection is healthy, degraded, stale, or unknown.
- `finality`: provisional or settled.

Unknown and non-comparable rows remain in every export. No API parameter may silently remove them.

A fresh, project-bound local simulator report uses `basis: measured`, `integrity: copy_observed_unsigned`, `comparability: exact`, `collectionHealth: healthy`, and `finality: provisional`. This combination proves only that the local app reached its conversion logic and returned a shape matching the current generated plan. It is never Device or Production evidence; an expired copy retains its provenance but becomes `collectionHealth: stale` with an `unknown` verdict.
