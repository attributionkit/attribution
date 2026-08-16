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

The resulting `.attribution/cloud.json` contains only the API base URL, organization/application identifiers, bundle identifier, schema version, and an opaque credential reference. The access token is stored in macOS Keychain under service `sh.attribution.cli`. Device codes and access tokens are never written to repository files or printed.

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

The exact HTTP contract is in [`contracts/openapi.yaml`](../contracts/openapi.yaml). Agent-safe tool schemas are in [`contracts/mcp-tools.json`](../contracts/mcp-tools.json); `attribution_connect` and `attribution_connect_complete` split the interactive browser handoff while server-retained possession proof and bearer tokens are intentionally never exposed in MCP tool output.

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

$ attribution runs upload
Uploaded exact .attribution/last-run.json bytes (manifest upload upload_example; status: accepted).

$ attribution ping
Hosted control plane is reachable (ping ping_example; status: reachable).
This connectivity ping is not Apple, device, or Production evidence.

$ attribution live-check --json
{"schemaVersion":"1.0.0","applicationId":"app_example","productionEvidence":false,"sections":{"config":{...},"build":{...},"your-logic":{...},"device":{"status":"unknown",...},"production":{"status":"unknown",...}}}
```

The ellipses are deliberate redactions of non-secret detail for readability, not values to paste into a parser. Device and Production remain grey in this transcript.
