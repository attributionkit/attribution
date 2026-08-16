# Fresh Expo app

> Client-preview exercise only: `https://attribution.sh/` is not receiving Apple postbacks yet. Do not ship this endpoint in a production app.

## 1. Create the host

```sh
CI=1 npx create-expo-app@latest attribution-probe --template blank-typescript
cd attribution-probe
```

Set a real `expo.ios.bundleIdentifier` in `app.json`; the CLI refuses to invent one.

## 2. Install and compile desired state

```sh
go install github.com/attributionkit/attribution/cmd/attribution@v0.1.0-preview.3
npm install https://github.com/attributionkit/attribution/releases/download/v0.1.0-preview.3/attributionkit-expo-0.1.0-preview.3.tgz
attribution init
```

Ensure Go's install directory is on `PATH` before invoking `attribution`.

Review `.attribution/config.yaml`. Add only the SKAdNetwork identifiers for networks you actually use and a real public Meta app ID if Meta is configured. Then run:

```sh
attribution plan
attribution apply --branch
attribution apply --branch   # reports no diff
npx expo prebuild --clean --platform ios
attribution verify
```

`--branch` creates `attribution/setup` before changing generated/config files and is safe when package installation left the new app dirty. If the intended dependency and config edits are already committed, use plain `attribution apply` instead.

`apply` registers a deterministic wrapper in `app.json`. The wrapper invokes the package config plugin, which merges SKAdNetwork identifiers rather than replacing identifiers written by other plugins. Expo autolinking adds the Swift module to the native target.

## 3. Call the runtime

Use [examples/expo/App.tsx](../examples/expo/App.tsx) as the minimal UI probe. `conversionValue` is deterministic and synchronous. `record` calls both Apple frameworks and returns their results separately.

On an iOS simulator, both Apple framework results are expected to be unavailable or failed. That is a successful simulator integration test, not device or Apple evidence.

The example prints the exact `AttributionUpdateReport` as one JSON line and also renders it as selectable text. Save only that JSON object to a fresh file, then import it within 15 minutes:

```sh
attribution probe import --framework expo --target simulator --report /path/to/runtime-report.json
attribution verify --json
```

The importer accepts only a regular non-symlink file no larger than 16 KiB. It rejects unknown or duplicate JSON keys, unknown backend statuses, stale files, events/conversion values/schema hashes that do not match the current config and generated plan, and any simulator report that claims an Apple backend succeeded. It stores `.attribution/probe.json`, an ignored, non-secret artifact containing the observed statuses and an SHA-256 binding to the exact source bytes; backend error text and the source path are not persisted. The artifact expires after 15 minutes.

A valid import adds one `runtime.report-imported` result under Your Logic with `evidence: simulator`, `basis: measured`, `integrity: copy_observed_unsigned`, `comparability: exact`, `collectionHealth: healthy`, and `finality: provisional`. Device and Production remain unchanged and unknown.
