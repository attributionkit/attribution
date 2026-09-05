# @attributionkit/expo

The Apple-only Expo module and config plugin for AttributionKit. It exposes the same auditable Swift runtime used by SwiftPM consumers and adds the report endpoint, SKAdNetwork identifiers, schema hash, and event-value plan to the compiled Info.plist.

This is a client preview. The example `https://attribution.sh/` report endpoint is not cleared for production app use.

Calls to the conversion-only `record` API update AdAttributionKit and SKAdNetwork independently through one semantic owner and return each Apple API result separately. That API does not start the collector runtime.

The optional `Attribution.ready` / `Attribution.track` API creates an app-local installation ID, persists queued events in SQLite, and performs HTTPS delivery to a configured collector. It also collects available AdServices tokens on supported physical iOS devices for claim exchange. It requires a separate release manifest; the conversion setup below does not create that manifest. See the [runtime and data-use guide](https://github.com/attributionkit/attribution/blob/v0.1.0-preview.5/docs/native-runtime.md) for prerequisites and privacy declarations.

The example app may explicitly print the `record` result so a developer can save the exact JSON and run the local-only `attribution probe import --framework expo --target simulator --report <path>` command. Logging belongs to the host example. A report import does not establish collector delivery or Apple production evidence.

The config plugin accepts an explicit `disableMetaConversionReporting` boolean. The CLI sets it from `conversionAuthority.owner`; it is independent of managed versus external setup so Meta can remain an ordinary event transport behind another declared authority.

Install the release tarball, run `attribution init`, edit `.attribution/config.yaml`, then run `attribution apply --branch` and `npx expo prebuild --clean`:

```sh
go install github.com/attributionkit/attribution/cmd/attribution@v0.1.0-preview.5
npm install https://github.com/attributionkit/attribution/releases/download/v0.1.0-preview.5/attributionkit-expo-0.1.0-preview.5.tgz
```

The scoped npm name is reserved but is not claimed as published by v0.1.0-preview.5.
