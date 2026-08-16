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

After a clean build and a runtime probe, run `attribution verify --json` again. Then use this exact hosted sequence when the service is available: `attribution connect` (human browser approval), `attribution runs upload` (exact last-run bytes), `attribution ping` (connectivity only), and `attribution live-check --json` (unified result). Agents may open the browser authorization URL but must pause for the human and must never handle credentials, device codes, or bearer tokens.

Preserve execution, verdict, evidence, basis, integrity, comparability, collection health, and finality independently. `unknown` is an honest result. `hidden` is never a basis. Keep Device and Production grey/unknown absent qualifying evidence. A ping and a manifest upload are supporting facts; Production can pass only from a cryptographically verified real Apple receipt. Postback copies are winning attributed-conversion copies, not an install ledger.
