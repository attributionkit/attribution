# Published Expo consumer probe

This fixture installs the immutable `v0.1.0-preview.5` release artifact and uses
Expo 57's compatible React and React Native versions. `index.js` registers the
app entry point. The desired configuration and generated plugin are checked in;
native projects, local run manifests, and runtime probes are generated locally.

From this directory, install with `npm ci`, inspect `attribution plan`, apply the
plan, and apply again to require no diff. Use `attribution apply --branch` when
other work is present. Then run `npx expo prebuild --clean --platform ios` and
build and launch the resulting iOS workspace.

Tap **Record Install**. The screen and JavaScript console emit the same one-line
`AttributionUpdateReport` JSON returned by the native SDK. Save that exact JSON,
without editing it, and immediately run:

```sh
attribution probe import --framework expo --target simulator --report /absolute/path/report.json
attribution verify --json
```

The final `.attribution/last-run.json` governs Config, Build, Your Logic, Device,
and Production. A report proving the event-to-value mapping can pass Your Logic
even when Apple APIs report unsupported operations on a simulator. It does not
prove an Apple round trip, an install, or a production winning postback copy.
