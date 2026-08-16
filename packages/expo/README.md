# @attributionkit/expo

The Apple-only Expo module and config plugin for AttributionKit. It exposes the same auditable Swift runtime used by SwiftPM consumers and adds the report endpoint, SKAdNetwork identifiers, schema hash, and event-value plan to the compiled Info.plist.

This is a client preview. The example `https://attribution.sh/` report endpoint is not live and must not be shipped in a production app.

This package performs no networking, reads no identifiers, and emits no event stream. Calls to `record` update AdAttributionKit and SKAdNetwork independently through one semantic owner and return each Apple API result separately.

The example app may explicitly print that returned report so a developer can save the exact JSON and run the local-only `attribution probe import --framework expo --target simulator --report <path>` command. Logging belongs to the host example, not this runtime package; the package still performs no network or telemetry work.

The config plugin accepts an explicit `disableMetaConversionReporting` boolean. The CLI sets it from `conversionAuthority.owner`; it is independent of managed versus external setup so Meta can remain an ordinary event transport behind another declared authority.

Install the release tarball, run `attribution init`, edit `.attribution/config.yaml`, then run `attribution apply --branch` and `npx expo prebuild --clean`:

```sh
go install github.com/attributionkit/attribution/cmd/attribution@v0.1.0-preview.4
npm install https://github.com/attributionkit/attribution/releases/download/v0.1.0-preview.4/attributionkit-expo-0.1.0-preview.4.tgz
```

The scoped npm name is reserved but is not claimed as published by v0.1.0-preview.4.
