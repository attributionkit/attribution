# Comparison contracts

`contract.schema.json` is the public v1 semantics for provider comparisons. A verdict is forbidden until a versioned contract separately describes both sources, records each scope-alignment relationship, requires comparable scope and final inputs, and defines a materiality gate. The only allowed residual name is `unexplained_delta`.

The Meta/AAK file in `test-vectors/` is a schema fixture, not a shipped provider verdict. It is deliberately `directional`: provider reporting never upgrades to Apple truth. When no validated contract applies, sources remain side-by-side with `comparability: none`.
