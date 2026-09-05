# Native runtime and data use

The iOS client preview exposes two entry points through Swift and Expo.

`AttributionCore.record` (Swift) and `record` (Expo) use the generated conversion
plan. They call the Apple conversion APIs and return an `AttributionUpdateReport`.
They do not start the installation/event collector. The quick starts demonstrate
this surface.

`Attribution.ready`, `Attribution.track`, `Attribution.flush`, and diagnostics use
the optional durable collector. Calling `ready` starts installation registration,
queues `first_open`, attempts AdServices collection on supported physical iOS
devices, and attempts an HTTPS flush. Calling `track` queues a declared event,
applies managed Apple conversion updates when configured, and attempts a flush.
Delivery failures leave records queued; successful `ready` or `track` completion
does not establish delivery. Inspect `flush` results and diagnostics separately.

## Collector prerequisites

The app must contain a validated `AttributionReleaseManifest` in either the
`AttributionKitReleaseManifestJSON` Info.plist value or a bundled
`AttributionKitReleaseManifest.json` resource. The optional
`AttributionKitReleaseManifest` Info.plist value selects another resource name.
The manifest names the application, bundle, collector origin, event schema,
conversion authority, and release configuration. Public app identifiers are
configuration; account credentials never belong in this file.

The existing `attribution init` / `apply` conversion flow does **not** issue this
collector manifest. Enabling the collector requires the corresponding hosted
application and schema configuration. The preview is not cleared for production
app use, and these source APIs do not establish hosted or Apple acceptance.

## Data behavior

- A random installation UUID is stored in the app's SQLite outbox and included
  in collector requests. It is not IDFA or IDFV, and is not an anonymous aggregate.
- The outbox holds event names, timestamps, declared properties, app/release
  metadata, retry state, and available AdServices tokens. Its retention bound is
  30 days and its queue bound is 10,000 records.
- HTTPS registration obtains an app-instance token that stays in local runtime
  storage. It is separate from human account credentials and the CLI Keychain
  connection.
- Deep-link capture strips user info, query strings, and fragments. The remaining
  URL path may still contain application-specific data; only capture suitable
  links. Diagnostics must also be treated as app data.
- The runtime does not read IDFA or IDFV. Client-supplied consent is not treated
  as an authoritative provider assertion; transport sends `unknown`. Hosts
  control when to call the collector APIs and which properties to declare.

## Privacy declarations

The shared privacy manifest declares analytics collection of the app-local
installation identifier, product interactions, advertising data, and purchases
when purchase events are supplied. These are conservatively declared as linked
because requests carry the persistent installation ID. The SDK does not declare
cross-company tracking or tracking domains. SwiftPM and both CocoaPods entry
points bundle the manifest; the Expo copy is checked against the Swift source.

This baseline does not describe every possible host-defined event property or
every collector. Review the final app's data uses and declarations when enabling
the collector or adding properties. Apple's definitions are in
[collected data types](https://developer.apple.com/documentation/bundleresources/app-privacy-configuration/nsprivacycollecteddatatypes/nsprivacycollecteddatatype)
and [App Privacy Details](https://developer.apple.com/app-store/app-privacy-details/).

App events, AdServices claims, conversion-only reports, and cryptographically
verified Apple postbacks retain separate provenance. None turns a simulator
probe into Device or Production evidence.
