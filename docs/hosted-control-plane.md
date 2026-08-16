# Hosted control-plane client

The hosted client is an explicit CLI surface. `AttributionCore` and `@attributionkit/expo` remain deterministic, credential-free, and network-free. The CLI never writes a bearer token into the project.

## Connect

Initialize, apply, run the configured event, import its fresh exact simulator report, and verify the project before connecting it:

```sh
attribution probe import --framework expo --target simulator --report /path/to/runtime-report.json
attribution verify --json
attribution connect
```

The probe import is local-only and can add evidence only to Your Logic. Its ignored `.attribution/probe.json` artifact is sanitized, exact-source-hash-bound, and expires after 15 minutes; it is included in the next run manifest as `copy_observed_unsigned`, never as Device or Production evidence.

`connect` reads the configured bundle identifier, creates a possession-bound authorization session, and opens the returned `verificationUriComplete` in the browser when available. The CLI also displays the fallback URL and user code. After the human authorizes the session, the CLI returns the high-entropy device code to the exchange endpoint and links the exact bundle identifier.

The resulting `.attribution/cloud.json` contains only the canonical API base URL, organization/application identifiers, bundle identifier, schema version, and an opaque credential reference. That reference is derived from the complete non-secret binding, so editing the file cannot redirect an existing Keychain token to another origin. The access token is stored in macOS Keychain under service `sh.attribution.cli`. Device codes and access tokens are never written to repository files or printed.

Bindings written by preview.2 used the older unbound Keychain account format and fail closed in preview.3. Run `attribution connect` once with preview.3 to replace that binding and Keychain entry.

For a local or staging API, use an explicit base URL during connect:

```sh
attribution connect --api-base https://staging-api.example.com
```

Plain HTTP is rejected except for loopback development servers.

## Upload the exact run

```sh
attribution runs upload
```

The CLI reads `.attribution/last-run.json` as bytes and sends those bytes without re-encoding. It sets:

- `Content-Type: application/vnd.attributionkit.run-manifest+json`
- `Content-Digest: sha-256=:<base64>:` over the exact bytes
- `Idempotency-Key: <sha256 hex>`

The cloud validates the manifest schema and bundle/application binding, but the upload does not create Apple or Production evidence.

## Connectivity and live evidence

```sh
attribution ping
attribution live-check
attribution live-check --json
```

`ping` records authenticated CLI-to-control-plane reachability with its own content digest and idempotency key. Its response is required to carry `productionEvidence: false`.

`live-check` returns the same five sections as local verification: Config, Build, Your Logic, Device, and Production. Every section and supporting fact retains basis, integrity, comparability, collection health, and finality. Connectivity and uploaded-manifest facts are supporting facts only. The public client rejects a Production pass unless it includes an `appleReceipt` fact with `basis: measured` and `integrity: apple_core_verified`.

The exact HTTP contract is in [`contracts/openapi.yaml`](../contracts/openapi.yaml). Agent-safe post-connect tool schemas are in [`contracts/mcp-tools.json`](../contracts/mcp-tools.json).

## Coding-agent MCP boundary

Initial authorization is deliberately **CLI plus human browser only**. The MCP resource server does not expose `attribution_connect`, `attribution_connect_complete`, an authorization-session tool, or token exchange. The supported handoff is:

1. The human completes `attribution connect` in their browser.
2. From the connected repository, run:

   ```sh
   attribution agent setup --host codex
   ```

   This registers an absolute, project-bound stdio command in the user's Codex configuration. The default server name includes a stable hash of the canonical non-secret API, organization, application, and bundle binding, so separate connections do not overwrite one another. It does not copy the access token or its Keychain reference into that configuration. Start a new Codex session in the repository after registration.

3. The coding agent calls exactly these four context-bound tools with no arguments:
   - `attribution_link_application` — read-only confirmation that the Keychain-scoped application still matches the repository bundle identifier; returns only `applicationId`, `organizationId`, and `bundleId`.
   - `attribution_upload_run` — reads and validates the repository's exact `.attribution/last-run.json` bytes, then uploads those bytes with their digest and idempotency key.
   - `attribution_ping` — records authenticated connectivity and always returns `productionEvidence: false`.
   - `attribution_live_check` — reads the unified five-section result.

The stdio process reloads `.attribution/cloud.json`, confirms it still matches the configured bundle identifier, and retrieves the token from macOS Keychain only inside the local process. Bearer tokens, device codes, authorization-session identifiers, verification URLs, user codes, application IDs, and manifest payloads are not MCP tool inputs. Credentials are never MCP results. The model must never ask the human to paste a credential into chat.

The hosted streamable-HTTP MCP endpoint remains available for MCP hosts with their own protected bearer-credential facility. Its explicit schemas are in [`contracts/mcp-tools.json`](../contracts/mcp-tools.json). The local CLI commands remain a complete non-MCP path; use one post-connect transport for a given action rather than uploading or pinging twice.

## Sanitized end-to-end transcript

The HTTP behavior and integrity headers in this transcript are exercised by `internal/attribution/cloud_test.go` with an `httptest` control plane.

```text
$ attribution probe import --framework expo --target simulator --report /tmp/runtime-report.json
Imported fresh expo simulator runtime report for install (conversion value 0).
Run `attribution verify --json` next. This probe can affect only Your Logic; Device and Production remain unknown.

$ attribution verify --json
{"type":"run_started","schemaVersion":"1.1.0",...}
...
{"type":"run_completed","manifest":{...}}

$ attribution connect
Authorize AttributionKit in your browser: https://app.attribution.sh/device?user_code=ABCD-EFGH
If prompted, enter code: ABCD-EFGH
[human approves in browser]
Connected sh.example.app to application app_example.
Wrote non-secret binding to .attribution/cloud.json; access token is in the OS keychain.

$ attribution agent setup --host codex
Registered the project-bound Attribution MCP server as "attribution-1156bbd14f5f" in Codex.
The access token remains in macOS Keychain and is never placed in MCP configuration or tool data.

$ attribution runs upload
Uploaded exact .attribution/last-run.json bytes (manifest upload upload_example; status: accepted).

$ attribution ping
Hosted control plane is reachable (ping ping_example; status: reachable).
This connectivity ping is not Apple, device, or Production evidence.

$ attribution live-check --json
{"schemaVersion":"1.0.0","applicationId":"app_example","productionEvidence":false,"sections":{"config":{...},"build":{...},"your-logic":{...},"device":{"status":"unknown",...},"production":{"status":"unknown",...}}}
```

The ellipses are deliberate redactions of non-secret detail for readability, not values to paste into a parser. Device and Production remain grey in this transcript.
