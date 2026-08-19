'use strict';

const assert = require('node:assert/strict');
const test = require('node:test');
const plugin = require('./withAttribution');

const valid = {
  endpoint: 'https://attribution.sh/path-is-ignored',
  publisherMode: true,
  skAdNetworkIds: ['cstr6suwn9.skadnetwork'],
  events: ['install', 'purchase'],
  schemaHash: 'a'.repeat(64),
  metaAppId: '123456789',
  disableMetaConversionReporting: true,
};

test('normalizes the endpoint and validates the shared plan', () => {
  const result = plugin._internal.normalizeOptions(valid);
  assert.equal(result.endpoint, 'https://attribution.sh/');
  assert.deepEqual(result.events, ['install', 'purchase']);
});

test('rejects duplicate events before native code generation', () => {
  assert.throws(
    () => plugin._internal.normalizeOptions({ ...valid, events: ['install', 'install'] }),
    /unique/,
  );
});

test('rejects placeholder Meta ids', () => {
  assert.throws(
    () => plugin._internal.normalizeOptions({ ...valid, metaAppId: '0000000000' }),
    /non-placeholder/,
  );
});

test('requires an explicit boolean for Meta conversion ownership', () => {
  assert.throws(
    () => plugin._internal.normalizeOptions({ ...valid, disableMetaConversionReporting: 'false' }),
    /must be a boolean/,
  );
});

test('allows Meta conversion reporting to be disabled when another plugin supplies the app id', () => {
  const result = plugin._internal.normalizeOptions({
    ...valid,
    metaAppId: undefined,
    disableMetaConversionReporting: true,
  });
  assert.equal(result.metaAppId, undefined);
  assert.equal(result.disableMetaConversionReporting, true);
});

test('sets the final Meta conversion flag from declared ownership', () => {
  const transport = plugin._internal.normalizeOptions(valid);
  const transportPlist = { FacebookSKAdNetworkReportEnabled: true };
  plugin._internal.applyInfoPlist(transportPlist, transport);
  assert.equal(transportPlist.FacebookSKAdNetworkReportEnabled, false);

  const authority = plugin._internal.normalizeOptions({
    ...valid,
    disableMetaConversionReporting: false,
  });
  const authorityPlist = { FacebookSKAdNetworkReportEnabled: false };
  plugin._internal.applyInfoPlist(authorityPlist, authority);
  assert.equal(authorityPlist.FacebookSKAdNetworkReportEnabled, true);
});

test('leaves Meta unmanaged when no Meta app or disable request is configured', () => {
  const options = plugin._internal.normalizeOptions({
    ...valid,
    metaAppId: undefined,
    disableMetaConversionReporting: false,
  });
  const plist = { FacebookSKAdNetworkReportEnabled: false };
  plugin._internal.applyInfoPlist(plist, options);
  assert.equal(plist.FacebookSKAdNetworkReportEnabled, false);
});

test('can disable Meta when another plugin supplies the app id', () => {
  const options = plugin._internal.normalizeOptions({
    ...valid,
    metaAppId: undefined,
    disableMetaConversionReporting: true,
  });
  const plist = { FacebookSKAdNetworkReportEnabled: true };
  plugin._internal.applyInfoPlist(plist, options);
  assert.equal(plist.FacebookSKAdNetworkReportEnabled, false);
});

test('merges desired SKAdNetwork ids without clobbering other plugins', () => {
  const merged = plugin._internal.mergeSKAdNetworkItems(
    [{ SKAdNetworkIdentifier: 'existing.skadnetwork', Extra: true }],
    ['existing.skadnetwork', 'new.skadnetwork'],
  );
  assert.deepEqual(merged, [
    { SKAdNetworkIdentifier: 'existing.skadnetwork', Extra: true },
    { SKAdNetworkIdentifier: 'new.skadnetwork' },
  ]);
});

test('does not add source-app identifiers in the advertiser default', () => {
  const options = plugin._internal.normalizeOptions({
    ...valid,
    publisherMode: false,
    skAdNetworkIds: [],
  });
  const plist = { SKAdNetworkItems: [{ SKAdNetworkIdentifier: 'existing.skadnetwork' }] };
  plugin._internal.applyInfoPlist(plist, options);
  assert.deepEqual(plist.SKAdNetworkItems, [
    { SKAdNetworkIdentifier: 'existing.skadnetwork' },
  ]);
  assert.equal(plist.AttributionCopyEndpoint, 'https://attribution.sh/');
  assert.equal(plist.NSAdvertisingAttributionReportEndpoint, 'https://attribution.sh/');
});

test('requires explicit publisher mode before adding SKAdNetwork identifiers', () => {
  assert.throws(
    () => plugin._internal.normalizeOptions({ ...valid, publisherMode: false }),
    /publisherMode/,
  );
});

test('rejects credential-shaped fields in the bundled release manifest', () => {
  assert.throws(
    () =>
      plugin._internal.normalizeOptions({
        ...valid,
        releaseManifest: { appId: 'public', accessToken: 'forbidden' },
      }),
    /forbidden credential-like field/,
  );
});
