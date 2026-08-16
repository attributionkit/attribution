---
name: attributionkit-verify
description: Configure, probe, connect, and independently verify AttributionKit in Expo iOS apps without overstating simulator or production evidence.
license: Apache-2.0
---

# AttributionKit setup and verification

1. Create tasks named Config, Build, Your Logic, Device, and Production.
2. Confirm `expo.ios.bundleIdentifier` is explicit and the tree is understood.
3. Run `attribution verify --json` before setup. A missing config is a valid day-zero finding.
4. Run `attribution init`, ask the user to review public provider IDs, then run `attribution plan`.
5. Apply only after the plan is understood. Preserve unrelated dirty work.
6. Run `attribution apply` a second time and require no diff.
7. Run `npx expo prebuild --clean --platform ios`, build the generated app, and invoke a configured event in the simulator.
8. Run `attribution verify --json` again. Update task state only from the event stream and final manifest.
9. Run `attribution connect`. Open the returned browser URL, but pause for the human to approve authorization and never enter, read, or relay their credentials. Confirm that only the non-secret `.attribution/cloud.json` binding was written; the token belongs in the OS keychain.
10. Run `attribution runs upload`. This must upload the exact `.attribution/last-run.json` bytes with `Content-Digest` and `Idempotency-Key`; do not regenerate or normalize the JSON.
11. Run `attribution ping`. Record only authenticated control-plane reachability. Never upgrade a ping to Apple, Device, or Production evidence.
12. Run `attribution live-check --json`. Reconcile Config, Build, Your Logic, Device, and Production from the returned labeled facts. Keep Device and Production grey/unknown unless each has its own qualifying evidence. Production may pass only from a cryptographically verified real Apple receipt.

## Evidence language

- Static proves files/config/dependency observations.
- Build proves native linking and compiled configuration only when build inspection is present.
- Simulator proves the app reaches the runtime and local business logic. It never proves SKAdNetwork, AdAttributionKit, AdServices, or an Apple postback.
- Device and Production remain unknown until real evidence lands.
- Every emitted result and hosted fact must retain `basis`, `integrity`, `comparability`, `collectionHealth`, and `finality`. Schema `1.1.0` makes the last two required on run results.

Never touch auth or place secrets in app config. Never call an unsigned copy-envelope field Apple-signed. Never join Apple postbacks to user-level identities.

The full order is intentional: day-zero verify → init/plan/apply → clean build and runtime probe → final local verify → human browser connect → exact manifest upload → connectivity ping → live check. Do not skip directly to a green hosted status.
