# AttributionKit verification instructions

When setting up or verifying an app, mirror these sections into the agent task list before running commands:

1. Config
2. Build
3. Your Logic
4. Device
5. Production

Run `attribution verify --json` and update the list only from emitted events. The final run manifest is the sole truth for the run. Never turn a static or simulator pass into device, Apple, or provider evidence.

Do not enter, retrieve, or transform account credentials. Authentication and provider authorization stay with the human in their browser. Public app IDs are configuration; private keys and access tokens never enter app source or agent output.

Before mutation, show `attribution plan`. If the working tree has unrelated changes, use the CLI's branch flow or stop; never overwrite user work. After apply, run apply again and require no diff, then run `npx expo prebuild --clean --platform ios` for Expo hosts.

Preserve execution, verdict, evidence, basis, integrity, and comparability independently. `unknown` is an honest result. `hidden` is never a basis. Postback copies are winning attributed-conversion copies, not an install ledger.
