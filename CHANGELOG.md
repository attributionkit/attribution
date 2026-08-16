# Changelog

## Unreleased

## 0.1.0-preview.3 - 2026-08-16

- Added `attribution agent setup --host codex` and a project-bound stdio MCP server whose name and Keychain account are bound to the canonical API, organization, application, and bundle identity without placing credentials in MCP configuration, arguments, results, or conversation context.
- Bound the local agent tools to the repository application and exact local run manifest, while preserving the four post-connect actions and the independent Device/Production evidence boundary.
- Bound Keychain lookup to the canonical API, organization, application, and bundle identity; preview.2 cloud bindings must reconnect and cannot redirect an existing token by editing repository state.
- Made live-check consumers reject extra sections, invented taxonomy labels, and malformed observation timestamps before CLI or MCP rendering.
- Fixed browser authorization polling so the CLI waits for the advertised initial interval and honors bounded HTTP 429 `slow_down` / `Retry-After` responses.
- Published the bounded authorization polling response in OpenAPI and added semantic contract checks for both polling and the Keychain-backed local MCP bridge.

## 0.1.0-preview.2 - 2026-08-16

- Corrected the public MCP contract to the four authenticated post-connect tools; initial authorization remains an explicit CLI plus human-browser ceremony and credentials never enter MCP arguments or results.
- Added strict, local-only `probe import` support for fresh Expo and SwiftUI `AttributionUpdateReport` JSON, with a non-secret expiring artifact and an isolated Your Logic simulator result.
- Added the explicit Go hosted-control-plane client: browser `connect`, exact-byte `runs upload`, connectivity-only `ping`, and labeled `live-check`.
- Published the corresponding OpenAPI and agent-safe MCP contracts without adding networking or credentials to the Swift/Expo runtimes.
- Promoted the versioned comparison contract with separate scopes, alignment rules, finality/materiality gates, and the fixed `unexplained_delta` residual.
- Bumped run-manifest semantics to `1.1.0`, requiring `collectionHealth` and `finality` on every result.
- Bounded every run-manifest string and result collection, rejected duplicate check IDs semantically, and aligned the Go validator with the published schema.

## 0.1.0-preview.1 - 2026-08-14

- First public Go CLI with deterministic Expo init, plan, apply, and independent verify flows.
- First `AttributionCore` SwiftPM/CocoaPods runtime with observable AdAttributionKit and SKAdNetwork results.
- First Expo Modules API bridge and config plugin.
- Public desired-state, run-manifest, ChangeSet, export-label, and comparison-contract schemas, plus explicitly deferred OpenAPI/MCP shells.
- Cross-language conversion-plan golden vector.
- GitHub Action, checksums, SBOM, and build-provenance release plumbing.

Hosted evidence ingestion, provider reconciliation, device-lab proof, and production postback evidence are not part of this pre-release. Its example `attribution.sh` endpoint is inactive and must not be shipped in production.
