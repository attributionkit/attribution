# Changelog

## Unreleased

- Corrected the public MCP contract to the four authenticated post-connect tools; initial authorization remains an explicit CLI plus human-browser ceremony and credentials never enter MCP arguments or results.
- Added strict, local-only `probe import` support for fresh Expo and SwiftUI `AttributionUpdateReport` JSON, with a non-secret expiring artifact and an isolated Your Logic simulator result.
- Added the explicit Go hosted-control-plane client: browser `connect`, exact-byte `runs upload`, connectivity-only `ping`, and labeled `live-check`.
- Published the corresponding OpenAPI and agent-safe MCP contracts without adding networking or credentials to the Swift/Expo runtimes.
- Promoted the versioned comparison contract with separate scopes, alignment rules, finality/materiality gates, and the fixed `unexplained_delta` residual.
- Bumped run-manifest semantics to `1.1.0`, requiring `collectionHealth` and `finality` on every result.

## 0.1.0-preview.1 - 2026-08-14

- First public Go CLI with deterministic Expo init, plan, apply, and independent verify flows.
- First `AttributionCore` SwiftPM/CocoaPods runtime with observable AdAttributionKit and SKAdNetwork results.
- First Expo Modules API bridge and config plugin.
- Public desired-state, run-manifest, ChangeSet, export-label, and comparison-contract schemas, plus explicitly deferred OpenAPI/MCP shells.
- Cross-language conversion-plan golden vector.
- GitHub Action, checksums, SBOM, and build-provenance release plumbing.

Hosted evidence ingestion, provider reconciliation, device-lab proof, and production postback evidence are not part of this pre-release. Its example `attribution.sh` endpoint is inactive and must not be shipped in production.
