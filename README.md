# AttributionKit

AttributionKit is an auditable attribution configuration compiler and Apple conversion runtime for iOS. The public client surface is deliberately small:

- a Go CLI that plans, applies, and independently verifies repository-owned attribution state;
- a project-bound local MCP bridge that lets Codex use the hosted live check while the application credential remains in macOS Keychain;
- a local-only, expiring simulator-report importer that makes Your Logic agent-visible without promoting it to Device or Production;
- `AttributionCore`, a SwiftPM/CocoaPods runtime shared by native apps;
- `@attributionkit/expo`, an Expo Modules API bridge and config plugin;
- a native Xcode host compiler that generates owned Swift/plist guidance and verifies the real target declaration without rewriting `project.pbxproj`;
- versioned schemas, hosted-client contracts, rules, comparison-contract definitions, and golden vectors.

The generated host manifest has a published Expo/SwiftUI discriminator in [`contracts/generated-manifest.schema.json`](contracts/generated-manifest.schema.json); cross-host artifacts and probe framework labels are rejected.

The runtime contains no identifiers, event stream, or network client. It maps a declared typed event to a fine conversion value, then updates AdAttributionKit and SKAdNetwork independently through one semantic owner. Apple API results remain separate and errors are returned instead of discarded.

## Status

`v0.1.0-preview.5` is the current **client preview** candidate. Setup and static verification work fully offline. Simulator calls prove app wiring and business-logic routing only; Apple postbacks require supported physical-device or production evidence and remain `unknown` until that evidence exists.

**Do not ship the preview's `https://attribution.sh/` endpoint in a production app.** The receiver is not claimed by the current preview release. The public repository now defines an explicit hosted CLI client and its OpenAPI/MCP contracts, while the Vercel Blob → Workflow → PlanetScale → WorkOS implementation remains a separate private cloud system. This source change alone is not deployment evidence.

The hosted client flow is documented in [docs/hosted-control-plane.md](docs/hosted-control-plane.md). It adds `connect`, `runs upload`, `ping`, `live-check`, and `agent setup` to the Go CLI without adding networking to either app runtime. Browser authorization remains CLI plus human only. After that authorization, `attribution agent setup` can register a repository-bound stdio MCP server in Codex; it reads the application credential from Keychain at execution time and never transports credentials through configuration, tool arguments, or tool results.

## Install

Build the CLI from source with Go 1.24 or newer:

```sh
go install github.com/attributionkit/attribution/cmd/attribution@latest
```

Release archives, third-party notices, and SHA-256 checksums are attached to each GitHub release. The Expo package is attached as an npm tarball until npm trusted publishing is activated:

```sh
npm install https://github.com/attributionkit/attribution/releases/download/v0.1.0-preview.5/attributionkit-expo-0.1.0-preview.5.tgz
```

For native apps, add `https://github.com/attributionkit/attribution` in Xcode and select the root package's `AttributionCore` product as documented in [the SwiftUI guide](docs/swiftui-quickstart.md). A root podspec is also included for CocoaPods consumers.

## SwiftUI quick start

`v0.1.0-preview.5` includes the first-class native CLI flow. Install that CLI, then run from a fresh Xcode SwiftUI app directory:

```sh
go install github.com/attributionkit/attribution/cmd/attribution@v0.1.0-preview.5
attribution init
attribution plan
attribution apply --branch
# Follow .attribution/swift/README.md in Xcode, then:
attribution verify --json
# Run the real app and save its exact report JSON:
attribution probe import --framework swiftui --target simulator --report /path/to/runtime-report.json
attribution verify --json
```

The native verifier requires the official Swift package product, generated source target membership, and exact values in an explicit target Info.plist. Merely generating `.attribution` files cannot produce a green build/config result. Continue with the same `connect` → exact upload → ping → live-check → optional agent setup flow described below.

## Expo quick start

```sh
npx create-expo-app@latest my-app --template blank-typescript
cd my-app
# Preview only: do not ship the inactive attribution.sh endpoint to production.
# Set expo.ios.bundleIdentifier in app.json first.
npm install https://github.com/attributionkit/attribution/releases/download/v0.1.0-preview.5/attributionkit-expo-0.1.0-preview.5.tgz
attribution init
# Review .attribution/config.yaml, then:
attribution plan
attribution apply --branch
npx expo prebuild --clean --platform ios
# Run the event in the simulator and save its exact report JSON, then:
attribution probe import --framework expo --target simulator --report /path/to/runtime-report.json
attribution verify --json
```

See [the Expo guide](docs/expo-quickstart.md) for a runnable call site and what each verification layer proves.

## GitHub Action

The Action verifies both the archive checksum and GitHub build provenance. Give the job read access to contents and attestations:

```yaml
permissions:
  contents: read
  attestations: read
steps:
  - uses: attributionkit/attribution/action@v0.1.0-preview.5
```

## Repository map

```text
cmd/attribution/            Go CLI entrypoint
internal/attribution/       compiler and independent verification rules
native/AttributionCore/     SwiftPM + CocoaPods runtime
packages/expo/              Expo module + config plugin
contracts/                  JSON Schema/export contracts and hosted API/MCP client contracts
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
- A runtime probe is a strict, project-bound, unsigned local copy that expires after 15 minutes and can affect only Your Logic.
- Apple postback copies contain no direct user identifier; AttributionKit does not attempt to reidentify them or join them to user-level records.

Licensed under Apache-2.0. Security reports should follow [SECURITY.md](SECURITY.md).
