# Architecture boundary

This repository is the public client and semantics plane described by the corrected Vercel-native plan.

```text
customer repository
  attribution Go CLI
       ├─ Expo config plugin + Swift Expo module
       └─ Xcode host plan + SwiftPM/CocoaPods AttributionCore
  public schemas, rules, vectors, and contracts

Apple → attribution.sh Function → private Blob → Vercel Workflow
      → PlanetScale ledger → Next.js/API/MCP/Parquet
      → WorkOS organizations and customer credential vault
```

The lower cloud path is a separate private repository and deployment boundary. It is not implemented or simulated here. In particular, the old Supabase text receiver is not a release candidate: it cannot preserve exact request bytes, stores request headers, and lacks Blob/workflow reconciliation.

## Public invariants

1. `conversionAuthority` and `eventTransports` are different roles.
2. Managed mode has one semantic owner for Apple conversion values. AdAttributionKit and SKAdNetwork may both be updated by that owner because Apple documents independent interoperability.
3. No secrets belong in the app. Public IDs are not credentials.
4. A run manifest is the only truth about a verification run.
5. `hidden` is never a metric basis. Basis, integrity, and comparability labels survive every format.
6. Simulator results never claim an Apple protocol round trip.
7. Generated changes are deterministic; unsupported shapes fail before mutation.
8. Imported simulator reports are strict, SHA-256-bound local copies with a 15-minute lifetime. They may pass only Your Logic and remain `copy_observed_unsigned`.
9. Native generated files are not integration evidence by themselves. SwiftUI verification observes the application target's package product, Sources build phase, and explicit target Info.plist without mutating those human-owned files.

The Expo and SwiftUI variants of `.attribution/manifest.json` are published in [`contracts/generated-manifest.schema.json`](../contracts/generated-manifest.schema.json). The host discriminator fixes the allowed package manager, generated-file set, and app-target binding; it is also the source-framework binding for simulator probe imports.

## Release boundary

The client release contains Go binaries, the source Swift package, the Expo npm tarball, contracts, checksums, an SBOM, and GitHub build provenance. It does not contain customer credentials, cloud migrations, provider connectors, or production evidence.
