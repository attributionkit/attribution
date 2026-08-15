---
name: attributionkit-verify
description: Configure and independently verify AttributionKit in Expo iOS apps without overstating simulator or production evidence.
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

## Evidence language

- Static proves files/config/dependency observations.
- Build proves native linking and compiled configuration only when build inspection is present.
- Simulator proves the app reaches the runtime and local business logic. It never proves SKAdNetwork, AdAttributionKit, AdServices, or an Apple postback.
- Device and Production remain unknown until real evidence lands.

Never touch auth or place secrets in app config. Never call an unsigned copy-envelope field Apple-signed. Never join Apple postbacks to user-level identities.
