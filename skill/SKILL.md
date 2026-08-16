---
name: attributionkit-verify
description: Configure, probe, connect, and independently verify AttributionKit in Expo or native SwiftUI iOS apps without overstating simulator or production evidence.
license: Apache-2.0
---

# AttributionKit setup and verification

1. Create tasks named Config, Build, Your Logic, Device, and Production.
2. Identify the host. For Expo, confirm `expo.ios.bundleIdentifier` is explicit. For SwiftUI, require exactly one non-symlink `.xcodeproj`, one application target, and literal consistent bundle/Info.plist settings. Never guess an ambiguous target or resolve build-setting variables.
3. Run `attribution verify --json` before setup. A missing config is a valid day-zero finding.
4. Run `attribution init`, ask the user to review public provider IDs, then run `attribution plan`.
5. Apply only after the plan is understood. Preserve unrelated dirty work.
6. Run `attribution apply` a second time and require no diff.
7. Complete the host integration. For Expo, run `npx expo prebuild --clean --platform ios`. For SwiftUI, follow `.attribution/swift/README.md`: link the official `AttributionCore` package product, target the exact generated Swift file, and copy the generated keys into the real explicit target Info.plist. Require all three SwiftUI integration checks to pass; generated artifacts alone are not proof.
8. Build the actual app and invoke a configured event in the simulator. Save the exact one-line `AttributionUpdateReport` JSON emitted by the example; do not synthesize or edit it. Within 15 minutes run `attribution probe import --framework <expo|swiftui> --target simulator --report <path>` using the framework that matches the generated host manifest. Never use this import path for a device or Production claim.
9. Run `attribution verify --json` again. Update task state only from the event stream and final manifest. A valid imported report may pass only `runtime.report-imported` under Your Logic; confirm Device and Production remain unknown.
10. Run `attribution connect`. Open the returned browser URL, but pause for the human to approve authorization and never enter, read, or relay their credentials. Confirm that only the non-secret `.attribution/cloud.json` binding was written; the token belongs in the OS keychain. Initial authorization is never an MCP tool call.
11. Choose one authenticated post-connect transport:
    - CLI: continue with steps 12–14.
    - Codex MCP: run `attribution agent setup --host codex`; this registers only an absolute project-bound stdio command and leaves the application token in Keychain. Start a new Codex session in this repository, then call `attribution_link_application`, `attribution_upload_run`, `attribution_ping`, and `attribution_live_check` with no arguments. No MCP connect/exchange tool exists. No MCP argument may contain a token, device code, authorization-session identifier, verification URL, user code, manifest payload, or application ID. Results may contain non-secret application/organization identifiers and labeled status, but never credentials or possession proof. Do not also repeat steps 12–14.
12. Run `attribution runs upload`. This must upload the exact `.attribution/last-run.json` bytes with `Content-Digest` and `Idempotency-Key`; do not regenerate or normalize the JSON.
13. Run `attribution ping`. Record only authenticated control-plane reachability. Never upgrade a ping to Apple, Device, or Production evidence.
14. Run `attribution live-check --json`. Reconcile Config, Build, Your Logic, Device, and Production from the returned labeled facts. Keep Device and Production grey/unknown unless each has its own qualifying evidence. Production may pass only from a cryptographically verified real Apple receipt.

## Evidence language

- Static proves files/config/dependency observations.
- Build proves native linking and compiled configuration only when build inspection is present.
- Simulator proves the app reaches the runtime and local business logic. It never proves SKAdNetwork, AdAttributionKit, AdServices, or an Apple postback.
- The local probe artifact is an expiring unsigned copy: `basis: measured`, `integrity: copy_observed_unsigned`, `comparability: exact`, `collectionHealth: healthy`, and `finality: provisional`. The `--framework` flag is declared provenance, not signed runtime identity.
- Device and Production remain unknown until real evidence lands.
- Every emitted result and hosted fact must retain `basis`, `integrity`, `comparability`, `collectionHealth`, and `finality`. Schema `1.1.0` makes the last two required on run results.

Never touch auth or place secrets in app config. Never call an unsigned copy-envelope field Apple-signed. Never join Apple postbacks to user-level identities.

The full order is intentional: day-zero verify → init/plan/apply → clean build and runtime probe → final local verify → human browser connect → exact manifest upload → connectivity ping → live check. Do not skip directly to a green hosted status.
