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
  'attribution_connect',
  'attribution_connect_complete',
  'attribution_upload_run',
  'attribution_ping',
  'attribution_live_check',
]);
if (mcp.tools.length !== expectedTools.size || mcp.tools.some((tool) => !expectedTools.has(tool.name))) {
  throw new Error('MCP hosted-control-plane tool set drifted');
}
const mcpPublicSurface = JSON.stringify(mcp);
if (mcpPublicSurface.includes('deviceCode') || mcpPublicSurface.includes('accessToken')) {
  throw new Error('MCP tool schemas must not expose device codes or bearer tokens');
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
