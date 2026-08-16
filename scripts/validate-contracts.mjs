import { readFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { resolve } from 'node:path';
import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';
import { parse as parseYaml } from 'yaml';

const root = resolve(import.meta.dirname, '..');
const schemaPaths = [
  'contracts/attribution-config.schema.json',
  'contracts/attribution-update-report.schema.json',
  'contracts/change-set.schema.json',
  'contracts/export-ledger-row.schema.json',
  'contracts/generated-manifest.schema.json',
  'contracts/run-manifest.schema.json',
  'contracts/run-event.schema.json',
  'contracts/live-status.schema.json',
  'contracts/runtime-probe.schema.json',
  'comparison-contracts/contract.schema.json',
];

const ajv = new Ajv2020({ allErrors: true, strict: true });
addFormats(ajv);

for (const path of schemaPaths) {
  const schema = JSON.parse(await readFile(resolve(root, path), 'utf8'));
  ajv.addSchema(schema);
}

const fixtures = [
  ['https://attribution.sh/contracts/attribution-config.v1.schema.json', 'test-vectors/config-managed.json'],
  ['https://attribution.sh/contracts/attribution-update-report.v1.schema.json', 'test-vectors/attribution-update-report-simulator.json'],
  ['https://attribution.sh/contracts/run-manifest.v1.schema.json', 'test-vectors/run-manifest-static.json'],
  ['https://attribution.sh/contracts/change-set.v1.schema.json', 'test-vectors/change-set-expo.json'],
  ['https://attribution.sh/contracts/generated-manifest.v1.schema.json', 'test-vectors/generated-manifest-expo.json'],
  ['https://attribution.sh/contracts/generated-manifest.v1.schema.json', 'test-vectors/generated-manifest-swiftui.json'],
  ['https://attribution.sh/contracts/live-status.v1.schema.json', 'test-vectors/live-status-connectivity.json'],
  ['https://attribution.sh/contracts/runtime-probe.v1.schema.json', 'test-vectors/runtime-probe-simulator.json'],
  ['https://attribution.sh/comparison-contracts/contract.v1.schema.json', 'test-vectors/comparison-contract-meta-aak.json'],
];

for (const schemaId of schemaPaths.map(async (path) => {
  const schema = JSON.parse(await readFile(resolve(root, path), 'utf8'));
  return schema.$id;
})) {
  const id = await schemaId;
  if (id && !ajv.getSchema(id)) throw new Error(`Schema failed to compile: ${id}`);
}

const fixtureData = new Map();
for (const [schemaId, fixturePath] of fixtures) {
  const validate = ajv.getSchema(schemaId);
  if (!validate) throw new Error(`Schema was not registered: ${schemaId}`);
  const fixture = JSON.parse(await readFile(resolve(root, fixturePath), 'utf8'));
  if (!validate(fixture)) {
    throw new Error(`${fixturePath} failed ${schemaId}: ${ajv.errorsText(validate.errors)}`);
  }
  fixtureData.set(fixturePath, fixture);
}

const sha256 = (value) => createHash('sha256').update(value, 'utf8').digest('hex');

function duplicateRunManifestCheckIds(value) {
  const seen = new Set();
  const duplicates = new Set();
  for (const result of value.results) {
    if (seen.has(result.checkId)) duplicates.add(result.checkId);
    seen.add(result.checkId);
  }
  return [...duplicates];
}

const config = fixtureData.get('test-vectors/config-managed.json');
if (config.mode !== 'managed' || config.conversionAuthority.owner !== 'managed-runtime') {
  throw new Error('managed config vector must use the Go CLI managed-runtime authority');
}
if (new Set(config.schema.events).size !== config.schema.events.length) {
  throw new Error('config vector events must be unique');
}
const expectedSchemaHash = sha256(JSON.stringify({ events: config.schema.events }));

const changeSet = fixtureData.get('test-vectors/change-set-expo.json');
for (const operation of changeSet.operations) {
  if (operation.kind === 'write_file' && sha256(operation.content) !== operation.sha256) {
    throw new Error(`change-set digest mismatch for ${operation.path}`);
  }
}

const generatedExpo = fixtureData.get('test-vectors/generated-manifest-expo.json');
const generatedSwiftUI = fixtureData.get('test-vectors/generated-manifest-swiftui.json');
if (
  generatedExpo.host !== 'expo' ||
  generatedExpo.appConfig.plugin !== './.attribution/plugin/withAttribution.js' ||
  generatedSwiftUI.host !== 'swiftui' ||
  generatedSwiftUI.appConfig.generatedSwift !== '.attribution/swift/AttributionKit.generated.swift' ||
  generatedSwiftUI.appConfig.packageProduct !== 'AttributionCore'
) {
  throw new Error('generated-manifest host vectors drifted from their exact integration contracts');
}
const validateGeneratedManifest = ajv.getSchema('https://attribution.sh/contracts/generated-manifest.v1.schema.json');
for (const [label, mutate] of [
  ['Expo appConfig on SwiftUI', (value) => { value.appConfig = structuredClone(generatedExpo.appConfig); }],
  ['SwiftPM on Expo', (value) => { value.packageManager = 'swiftpm'; }],
  ['wrong Swift source path', (value) => { value.generatedFiles[0].path = 'Fixture/AttributionKit.generated.swift'; }],
  ['external SwiftUI mode', (value) => { value.mode = 'external'; }],
]) {
  const base = label === 'SwiftPM on Expo' ? generatedExpo : generatedSwiftUI;
  const invalid = structuredClone(base);
  mutate(invalid);
  if (validateGeneratedManifest(invalid)) {
    throw new Error(`generated-manifest accepted ${label}`);
  }
}
const appPatch = changeSet.operations.find(
  (operation) => operation.kind === 'patch_json' && operation.path === 'app.json',
);
const registrations = appPatch?.merge?.expo?.plugins ?? [];
if (!registrations.includes('./.attribution/plugin/withAttribution.js')) {
  throw new Error('change-set vector must register the exact .js Expo wrapper path');
}

const manifest = fixtureData.get('test-vectors/run-manifest-static.json');
if (manifest.project.schemaHash !== expectedSchemaHash) {
  throw new Error('run-manifest schema hash drifted from the managed config vector');
}
if (duplicateRunManifestCheckIds(manifest).length !== 0) {
  throw new Error('run-manifest check ids must be unique');
}

const validateRunManifest = ajv.getSchema('https://attribution.sh/contracts/run-manifest.v1.schema.json');
for (const [label, mutate] of [
  ['result count', (value) => {
    value.results = Array.from({ length: 129 }, (_, index) => ({
      ...value.results[0],
      checkId: `bounded-check-${index}`,
    }));
  }],
  ['check id', (value) => { value.results[0].checkId = 'x'.repeat(129); }],
  ['rule version', (value) => { value.results[0].ruleVersion = 'x'.repeat(65); }],
  ['reason', (value) => { value.results[0].reason = 'x'.repeat(1001); }],
  ['remediation', (value) => { value.results[0].remediation = 'x'.repeat(2001); }],
  ['bundle id', (value) => { value.project.bundleId = 'x'.repeat(256); }],
  ['config hash', (value) => { value.project.configHash = 'x'.repeat(65); }],
  ['schema hash', (value) => { value.project.schemaHash = 'x'.repeat(65); }],
]) {
  const invalid = structuredClone(manifest);
  mutate(invalid);
  if (validateRunManifest(invalid)) {
    throw new Error(`run-manifest accepted an oversized ${label}`);
  }
}

const duplicateCheckManifest = structuredClone(manifest);
duplicateCheckManifest.results.push({
  ...duplicateCheckManifest.results[0],
  reason: 'A second result reused an existing check id.',
});
if (!validateRunManifest(duplicateCheckManifest)) {
  throw new Error('duplicate check-id semantic vector must remain structurally valid');
}
if (duplicateRunManifestCheckIds(duplicateCheckManifest).length !== 1) {
  throw new Error('run-manifest semantic validation did not reject a duplicate check id');
}

const runtimeProbe = fixtureData.get('test-vectors/runtime-probe-simulator.json');
const runtimeReport = fixtureData.get('test-vectors/attribution-update-report-simulator.json');
if (Date.parse(runtimeProbe.expiresAt) - Date.parse(runtimeProbe.importedAt) !== 15 * 60 * 1000) {
  throw new Error('runtime probe fixture must use the 15-minute freshness window');
}
if (
  Date.parse(runtimeProbe.source.modifiedAt) < Date.parse(runtimeProbe.importedAt) - 15 * 60 * 1000 ||
  Date.parse(runtimeProbe.source.modifiedAt) > Date.parse(runtimeProbe.importedAt) + 60 * 1000
) {
  throw new Error('runtime probe source timestamp is outside its accepted import window');
}
if (runtimeProbe.report.schemaHash !== runtimeProbe.project.schemaHash) {
  throw new Error('runtime probe report must bind the project schema hash');
}
if (runtimeProbe.report.fineConversionValue !== config.schema.events.indexOf(runtimeProbe.report.event)) {
  throw new Error('runtime probe event/value mapping drifted from the managed config vector');
}
if (
  runtimeReport.event !== runtimeProbe.report.event ||
  runtimeReport.fineConversionValue !== runtimeProbe.report.fineConversionValue ||
  runtimeReport.schemaHash !== runtimeProbe.report.schemaHash ||
  runtimeReport.adAttributionKit.status !== runtimeProbe.report.adAttributionKit.status ||
  runtimeReport.skAdNetwork.status !== runtimeProbe.report.skAdNetwork.status
) {
  throw new Error('sanitized runtime probe vector drifted from its exact source report');
}
const runtimeReportBytes = await readFile(resolve(root, 'test-vectors/attribution-update-report-simulator.json'));
if (
  runtimeProbe.source.sha256 !== createHash('sha256').update(runtimeReportBytes).digest('hex') ||
  runtimeProbe.source.byteLength !== runtimeReportBytes.byteLength
) {
  throw new Error('runtime probe source binding does not match the exact report vector bytes');
}

const comparison = fixtureData.get('test-vectors/comparison-contract-meta-aak.json');
if (comparison.residualName !== 'unexplained_delta' || comparison.comparability !== 'directional') {
  throw new Error('comparison fixture must preserve the unexplained residual and directional scope');
}

const liveStatus = fixtureData.get('test-vectors/live-status-connectivity.json');
const validateLiveStatus = ajv.getSchema('https://attribution.sh/contracts/live-status.v1.schema.json');
const falseProductionPass = structuredClone(liveStatus);
falseProductionPass.sections.production.status = 'pass';
if (validateLiveStatus(falseProductionPass)) {
  throw new Error('live status accepted a Production pass without Production evidence');
}
const unverifiedProductionPass = structuredClone(falseProductionPass);
unverifiedProductionPass.productionEvidence = true;
if (validateLiveStatus(unverifiedProductionPass)) {
  throw new Error('live status accepted Production evidence without a verified Apple receipt fact');
}

const openapi = parseYaml(await readFile(resolve(root, 'contracts/openapi.yaml'), 'utf8'));
const expectedOperations = new Set([
  'createAuthorizationSession',
  'exchangeAuthorizationSession',
  'linkApplication',
  'uploadVerificationRun',
  'createConnectivityPing',
  'getLiveStatus',
]);
const observedOperations = new Set();
for (const path of Object.values(openapi.paths)) {
  for (const operation of Object.values(path)) {
    if (operation && typeof operation === 'object' && operation.operationId) {
      observedOperations.add(operation.operationId);
    }
  }
}
if (observedOperations.size !== expectedOperations.size || [...expectedOperations].some((id) => !observedOperations.has(id))) {
  throw new Error(`OpenAPI operation set drifted: ${[...observedOperations].join(', ')}`);
}
const pingResponse = openapi.paths['/v1/applications/{applicationId}/pings'].post.responses['200'].content['application/json'].schema;
if (pingResponse.properties.productionEvidence.const !== false) {
  throw new Error('ping contract must explicitly prohibit Production evidence');
}
const exchangeResponses =
  openapi.paths['/v1/cli/authorization-sessions/{authorizationSessionId}/exchange'].post.responses;
const slowDown = exchangeResponses['429'];
if (
  slowDown?.headers?.['Retry-After']?.schema?.minimum !== 1 ||
  slowDown?.headers?.['Retry-After']?.schema?.maximum !== 60 ||
  slowDown?.content?.['application/json']?.schema?.properties?.status?.const !== 'slow_down'
) {
  throw new Error('authorization exchange must publish its bounded slow_down response');
}
for (const operationPath of [
  '/v1/applications/{applicationId}/verification-runs',
  '/v1/applications/{applicationId}/pings',
]) {
  const refs = openapi.paths[operationPath].post.parameters.map((parameter) => parameter.$ref);
  if (!refs.includes('#/components/parameters/ContentDigest') || !refs.includes('#/components/parameters/IdempotencyKey')) {
    throw new Error(`${operationPath} must require Content-Digest and Idempotency-Key`);
  }
}

const mcp = JSON.parse(await readFile(resolve(root, 'contracts/mcp-tools.json'), 'utf8'));
const expectedTools = new Set([
  'attribution_link_application',
  'attribution_upload_run',
  'attribution_ping',
  'attribution_live_check',
]);
const observedMcpTools = new Set(mcp.tools.map((tool) => tool.name));
if (
  mcp.schemaVersion !== '1.1.0' ||
  mcp.tools.length !== expectedTools.size ||
  observedMcpTools.size !== expectedTools.size ||
  [...expectedTools].some((name) => !observedMcpTools.has(name))
) {
  throw new Error('MCP hosted-control-plane tool set drifted');
}
const mcpPublicSurface = JSON.stringify(mcp);
for (const forbidden of [
  'attribution_connect',
  'authorizationSessionId',
  'verificationUri',
  'userCode',
  'deviceCode',
  'accessToken',
]) {
  if (mcpPublicSurface.includes(forbidden)) {
    throw new Error(`MCP resource-server contract exposed forbidden authorization surface ${forbidden}`);
  }
}
if (
  mcp.status !== 'authenticated-post-connect-resource-server' ||
  mcp.authorizationBoundary?.initialAuthorization !== 'cli-human-browser-only' ||
  mcp.authorizationBoundary?.mcpAuthentication !== 'preconfigured-bearer' ||
  mcp.authorizationBoundary?.credentialToolArguments !== false ||
  mcp.authorizationBoundary?.credentialToolResults !== false
) {
  throw new Error('MCP authorization boundary drifted from CLI plus human browser authorization');
}
if (
  mcp.localProjectBridge?.status !== 'post-connect-keychain-backed-stdio' ||
  mcp.localProjectBridge?.setupCommand !== 'attribution agent setup --host codex' ||
  mcp.localProjectBridge?.configurationContainsCredential !== false ||
  mcp.localProjectBridge?.toolArgumentsContainCredential !== false ||
  mcp.localProjectBridge?.toolResultsContainCredential !== false ||
  JSON.stringify(mcp.localProjectBridge?.tools) !== JSON.stringify([...expectedTools])
) {
  throw new Error('MCP local project bridge drifted from the Keychain-backed post-connect boundary');
}
const linkApplicationTool = mcp.tools.find((tool) => tool.name === 'attribution_link_application');
const bundleIdPattern = '^[A-Za-z0-9-]+(?:\\.[A-Za-z0-9-]+)+$';
if (
  linkApplicationTool.inputSchema.additionalProperties !== false ||
  JSON.stringify(linkApplicationTool.inputSchema.required) !== JSON.stringify(['bundleId']) ||
  linkApplicationTool.inputSchema.properties.bundleId.pattern !== bundleIdPattern ||
  linkApplicationTool.outputSchema.additionalProperties !== false ||
  JSON.stringify(linkApplicationTool.outputSchema.required) !==
    JSON.stringify(['applicationId', 'organizationId', 'bundleId']) ||
  linkApplicationTool.outputSchema.properties.bundleId.pattern !== bundleIdPattern ||
  linkApplicationTool.annotations?.readOnlyHint !== true ||
  linkApplicationTool.annotations?.destructiveHint !== false ||
  linkApplicationTool.annotations?.idempotentHint !== true ||
  linkApplicationTool.annotations?.openWorldHint !== false
) {
  throw new Error('attribution_link_application drifted from the private read-confirm tool');
}
const uploadRunTool = mcp.tools.find((tool) => tool.name === 'attribution_upload_run');
if (
  uploadRunTool.inputSchema.additionalProperties !== false ||
  uploadRunTool.inputSchema.properties.applicationId.maxLength !== 128 ||
  uploadRunTool.inputSchema.properties.manifestBytesBase64.maxLength !== 1398104 ||
  uploadRunTool.inputSchema.properties.contentDigest.pattern !== '^sha-256=:[A-Za-z0-9+/]{43}=:$' ||
  uploadRunTool.inputSchema.properties.idempotencyKey.pattern !== '^[A-Za-z0-9._:-]+$' ||
  uploadRunTool.outputSchema.additionalProperties !== false ||
  uploadRunTool.annotations?.readOnlyHint !== false ||
  uploadRunTool.annotations?.destructiveHint !== false ||
  uploadRunTool.annotations?.idempotentHint !== true ||
  uploadRunTool.annotations?.openWorldHint !== false
) {
  throw new Error('attribution_upload_run drifted from the private exact-byte tool');
}
const pingTool = mcp.tools.find((tool) => tool.name === 'attribution_ping');
if (
  pingTool.inputSchema.additionalProperties !== false ||
  pingTool.inputSchema.properties.applicationId.maxLength !== 128 ||
  pingTool.inputSchema.properties.idempotencyKey.pattern !== '^[A-Za-z0-9._:-]+$' ||
  pingTool.outputSchema.properties.status.const !== 'reachable' ||
  pingTool.outputSchema.properties.productionEvidence.const !== false ||
  pingTool.annotations?.readOnlyHint !== false ||
  pingTool.annotations?.destructiveHint !== false ||
  pingTool.annotations?.idempotentHint !== true ||
  pingTool.annotations?.openWorldHint !== false
) {
  throw new Error('attribution_ping drifted from the private connectivity-only tool');
}
const liveCheckTool = mcp.tools.find((tool) => tool.name === 'attribution_live_check');
if (
  liveCheckTool.inputSchema.additionalProperties !== false ||
  liveCheckTool.inputSchema.properties.applicationId.maxLength !== 128 ||
  liveCheckTool.outputSchema.$ref !== 'https://attribution.sh/contracts/live-status.v1.schema.json' ||
  liveCheckTool.annotations?.readOnlyHint !== true ||
  liveCheckTool.annotations?.destructiveHint !== false ||
  liveCheckTool.annotations?.idempotentHint !== true ||
  liveCheckTool.annotations?.openWorldHint !== false
) {
  throw new Error('attribution_live_check drifted from the private read-only tool');
}
const canonicalCheckIds = new Set([
  'schema.valid',
  'expo.package-installed',
  'expo.plugin-registered',
  'expo.plugin-wrapper',
  'app.bundle-id-matches',
  'apple.conversion-authority.single-owner',
  'apple.endpoint.report-attribution',
  'apple.skan.items-present',
  'meta.app-id-wired',
  'meta.conversion-management-disabled',
  'secrets.none-in-client',
  'generated.manifest-hashes',
  'runtime.report-imported',
  'device.aak-postback',
  'production.winning-copy',
]);
for (const result of manifest.results) {
  if (!canonicalCheckIds.has(result.checkId)) {
    throw new Error(`run-manifest vector uses unknown check id ${result.checkId}`);
  }
  if (result.evidence === 'static' && result.basis !== 'unknown') {
    throw new Error(`static result ${result.checkId} must not claim a measured basis`);
  }
  if (!result.collectionHealth || !result.finality) {
    throw new Error(`result ${result.checkId} is missing collection health or finality`);
  }
}

console.log(`Validated ${schemaPaths.length} schemas and ${fixtures.length} semantic golden fixtures.`);
