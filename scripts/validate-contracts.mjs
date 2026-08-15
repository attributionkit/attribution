import { readFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { resolve } from 'node:path';
import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';

const root = resolve(import.meta.dirname, '..');
const schemaPaths = [
  'contracts/attribution-config.schema.json',
  'contracts/change-set.schema.json',
  'contracts/export-ledger-row.schema.json',
  'contracts/run-manifest.schema.json',
  'contracts/run-event.schema.json',
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
  ['https://attribution.sh/contracts/run-manifest.v1.schema.json', 'test-vectors/run-manifest-static.json'],
  ['https://attribution.sh/contracts/change-set.v1.schema.json', 'test-vectors/change-set-expo.json'],
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
}

console.log(`Validated ${schemaPaths.length} schemas and ${fixtures.length} semantic golden fixtures.`);
