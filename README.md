# AttributionKit

AttributionKit is an auditable attribution configuration compiler and Apple conversion runtime for iOS. The public client surface is deliberately small:

- a Go CLI that plans, applies, and independently verifies repository-owned attribution state;
- `AttributionCore`, a SwiftPM/CocoaPods runtime shared by native apps;
- `@attributionkit/expo`, an Expo Modules API bridge and config plugin;
- versioned schemas, rules, comparison-contract definitions, and golden vectors.

The runtime contains no identifiers, event stream, or network client. It maps a declared typed event to a fine conversion value, then updates AdAttributionKit and SKAdNetwork independently through one semantic owner. Apple API results remain separate and errors are returned instead of discarded.

## Status

`v0.1.0-preview.1` is the first **client preview**, published as a GitHub pre-release. Setup and static verification work fully offline. Simulator calls prove app wiring and business-logic routing only; Apple postbacks require supported physical-device or production evidence and remain `unknown` until that evidence exists.

**Do not ship the preview's `https://attribution.sh/` endpoint in a production app.** The receiver is not deployed yet, so Apple postbacks sent there would be lost. The hosted Vercel Blob → Workflow → PlanetScale → WorkOS evidence plane is a separate private cloud system and is not claimed by this release.

## Install

Build the CLI from source with Go 1.24 or newer:

```sh
go install github.com/attributionkit/attribution/cmd/attribution@latest
```

Release archives, third-party notices, and SHA-256 checksums are attached to each GitHub release. The Expo package is attached as an npm tarball until npm trusted publishing is activated:

```sh
npm install https://github.com/attributionkit/attribution/releases/download/v0.1.0-preview.1/attributionkit-expo-0.1.0-preview.1.tgz
```

For native apps, add `https://github.com/attributionkit/attribution` in Xcode and select the root package's `AttributionCore` product as documented in [the SwiftUI guide](docs/swiftui-quickstart.md). A root podspec is also included for CocoaPods consumers.

## Expo quick start

```sh
npx create-expo-app@latest my-app --template blank-typescript
cd my-app
# Preview only: do not ship the inactive attribution.sh endpoint to production.
# Set expo.ios.bundleIdentifier in app.json first.
npm install https://github.com/attributionkit/attribution/releases/download/v0.1.0-preview.1/attributionkit-expo-0.1.0-preview.1.tgz
attribution init
# Review .attribution/config.yaml, then:
attribution plan
attribution apply --branch
npx expo prebuild --clean --platform ios
attribution verify
```

See [the Expo guide](docs/expo-quickstart.md) for a runnable call site and what each verification layer proves.

## GitHub Action

The Action verifies both the archive checksum and GitHub build provenance. Give the job read access to contents and attestations:

```yaml
permissions:
  contents: read
  attestations: read
steps:
  - uses: attributionkit/attribution/action@v0.1.0-preview.1
```

## Repository map

```text
cmd/attribution/            Go CLI entrypoint
internal/attribution/       compiler and independent verification rules
native/AttributionCore/     SwiftPM + CocoaPods runtime
packages/expo/              Expo module + config plugin
contracts/                  JSON Schema/export contracts; reserved API/MCP shells
rules/core/                 published core rule inventory
comparison-contracts/       comparison contract schema
test-vectors/               cross-language golden vectors
action/                     thin GitHub Action wrapper
skill/                      agent-native verification instructions
examples/                   minimal Expo and SwiftUI call sites
```

## Trust boundaries

- Anything that runs in a customer app/repository or defines what evidence means is public here.
- Hosted ingestion, tenancy, connectors, credentials, billing, anti-abuse logic, and operations remain private.
- A static pass is not Apple evidence. The CLI keeps execution, verdict, evidence, basis, integrity, and comparability separate.
- Apple postback copies contain no direct user identifier; AttributionKit does not attempt to reidentify them or join them to user-level records.

Licensed under Apache-2.0. Security reports should follow [SECURITY.md](SECURITY.md).
