# Fresh SwiftUI app

> Client-preview exercise only: `https://attribution.sh/` is not receiving Apple postbacks yet. Do not ship this endpoint in a production app.

## 1. Add the source package

Create an iOS SwiftUI app with Xcode 15 or newer (Swift tools 5.9+). Choose **File → Add Package Dependencies**, enter `https://github.com/attributionkit/attribution`, select the release version, and add the `AttributionCore` product to the app target. During local development, add the repository root as a local package; the root `Package.swift` vends the same product.

For CocoaPods, consume the root podspec from the same immutable tag:

```ruby
pod 'AttributionCore', :git => 'https://github.com/attributionkit/attribution.git', :tag => 'v0.1.0-preview.3'
```

## 2. Add compiled plan values

Add these values to the app target's Info.plist/build configuration:

- `NSAdvertisingAttributionReportEndpoint`: `https://attribution.sh/` (preview wiring only; inactive)
- `SKAdNetworkItems`: an array of dictionaries with `SKAdNetworkIdentifier`
- `AttributionKitSchemaHash`: the SHA-256 plan hash
- `AttributionKitEventValues`: a dictionary such as `install = 0`, `trial = 1`, `purchase = 2`, `retention = 3`

Use [examples/swiftui/ContentView.swift](../examples/swiftui/ContentView.swift) as the minimal call site. `AttributionConfiguration.fromBundle()` validates the compiled values before any Apple call.

The example encodes the returned `AttributionUpdateReport` as one exact JSON line, prints it, and renders selectable text. Save only that JSON object to a fresh file. From the configured project checkout whose `.attribution/config.yaml` and generated `.attribution/manifest.json` supplied the same schema plan, import it within 15 minutes:

```sh
attribution probe import \
  --framework swiftui \
  --target simulator \
  --report /path/to/runtime-report.json \
  --project /path/to/configured-project
attribution verify --json --project /path/to/configured-project
```

`--framework swiftui` records unsigned source provenance; the report itself has no framework field, so it is not a cryptographic framework attestation. The importer independently requires exact event/value/schema agreement with the current config and generated plan, rejects stale or malformed input, redacts backend error text from its local artifact, and can pass only Your Logic.

## 3. Interpret the result honestly

The simulator proves that the package links, the compiled plan is readable, the UI reaches the native runtime, and each Apple backend returns an observable result. It cannot produce a real SKAdNetwork or AdAttributionKit postback. Device-lab and production rows therefore remain pending.
