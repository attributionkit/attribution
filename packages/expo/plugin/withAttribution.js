'use strict';

const { withEntitlementsPlist, withInfoPlist } = require('expo/config-plugins');

const SKAN_ID_PATTERN = /^[a-z0-9]+\.skadnetwork$/;
const SCHEMA_HASH_PATTERN = /^[a-f0-9]{64}$/;
const ALLOWED_EVENTS = new Set(['install', 'trial', 'purchase', 'retention']);

function normalizeOptions(options = {}) {
  const endpointUrl = new URL(options.endpoint);
  if (endpointUrl.protocol !== 'https:') {
    throw new Error('AttributionKit endpoint must use https.');
  }
  if (endpointUrl.username || endpointUrl.password) {
    throw new Error('AttributionKit endpoint must not contain credentials.');
  }
  const endpoint = `${endpointUrl.origin}/`;

  const skAdNetworkIds = [...new Set(options.skAdNetworkIds ?? [])];
  for (const id of skAdNetworkIds) {
    if (!SKAN_ID_PATTERN.test(id)) {
      throw new Error(`Invalid SKAdNetwork identifier: ${id}`);
    }
  }

  const publisherMode = options.publisherMode === true;
  if (!publisherMode && skAdNetworkIds.length > 0) {
    throw new Error(
      'AttributionKit SKAdNetwork identifiers require publisherMode; advertised apps must not install source-app identifiers.',
    );
  }

  const events = options.events ?? [];
  if (!Array.isArray(events) || events.length === 0) {
    throw new Error('AttributionKit requires at least one event.');
  }
  if (new Set(events).size !== events.length) {
    throw new Error('AttributionKit events must be unique.');
  }
  for (const event of events) {
    if (!ALLOWED_EVENTS.has(event)) {
      throw new Error(`Unsupported AttributionKit event: ${event}`);
    }
  }
  if (events.length > 64) {
    throw new Error('AttributionKit supports at most 64 fine conversion values.');
  }

  if (!SCHEMA_HASH_PATTERN.test(options.schemaHash ?? '')) {
    throw new Error('AttributionKit schemaHash must be a lowercase SHA-256 hex digest.');
  }

  let metaAppId;
  if (options.metaAppId !== undefined) {
    metaAppId = String(options.metaAppId);
    if (!/^[0-9]{5,20}$/.test(metaAppId) || /^0+$/.test(metaAppId)) {
      throw new Error('AttributionKit Meta app id must be a non-placeholder numeric id.');
    }
  }

  if (
    options.disableMetaConversionReporting !== undefined &&
    typeof options.disableMetaConversionReporting !== 'boolean'
  ) {
    throw new Error('AttributionKit disableMetaConversionReporting must be a boolean.');
  }


  const associatedDomains = [...new Set(options.associatedDomains ?? [])];
  for (const domain of associatedDomains) {
    if (typeof domain !== 'string' || !/^[a-z0-9.-]+$/i.test(domain) || domain.includes('..')) {
      throw new Error(`Invalid AttributionKit associated domain: ${domain}`);
    }
  }

  if (
    options.reengagementPostbackCopies !== undefined &&
    typeof options.reengagementPostbackCopies !== 'boolean'
  ) {
    throw new Error('AttributionKit reengagementPostbackCopies must be a boolean.');
  }

  let releaseManifestJSON;
  if (options.releaseManifest !== undefined) {
    if (
      typeof options.releaseManifest !== 'object' ||
      options.releaseManifest === null ||
      Array.isArray(options.releaseManifest)
    ) {
      throw new Error('AttributionKit releaseManifest must be an object.');
    }
    releaseManifestJSON = JSON.stringify(options.releaseManifest);
    if (/credential|private[_-]?key|access[_-]?token|refresh[_-]?token|secret/i.test(releaseManifestJSON)) {
      throw new Error('AttributionKit releaseManifest contains a forbidden credential-like field.');
    }
  }

  return {
    endpoint,
    skAdNetworkIds,
    publisherMode,
    events,
    schemaHash: options.schemaHash,
    metaAppId,
    disableMetaConversionReporting: options.disableMetaConversionReporting,
    associatedDomains,
    reengagementPostbackCopies: options.reengagementPostbackCopies === true,
    releaseManifestJSON,
  };
}

function mergeSKAdNetworkItems(current, desiredIds) {
  const currentItems = Array.isArray(current) ? current : [];
  const byIdentifier = new Map();

  for (const item of currentItems) {
    if (item && typeof item === 'object' && typeof item.SKAdNetworkIdentifier === 'string') {
      byIdentifier.set(item.SKAdNetworkIdentifier, item);
    }
  }
  for (const id of desiredIds) {
    if (!byIdentifier.has(id)) {
      byIdentifier.set(id, { SKAdNetworkIdentifier: id });
    }
  }
  return [...byIdentifier.values()];
}

function applyInfoPlist(modResults, options) {
  modResults.AttributionCopyEndpoint = options.endpoint;
  modResults.NSAdvertisingAttributionReportEndpoint = options.endpoint;
  if (options.publisherMode) {
    modResults.SKAdNetworkItems = mergeSKAdNetworkItems(
      modResults.SKAdNetworkItems,
      options.skAdNetworkIds,
    );
  }
  modResults.AttributionKitSchemaHash = options.schemaHash;
  modResults.AttributionKitEventValues = Object.fromEntries(
    options.events.map((event, index) => [event, index]),
  );
  if (options.releaseManifestJSON !== undefined) {
    modResults.AttributionKitReleaseManifestJSON = options.releaseManifestJSON;
  }
  if (options.reengagementPostbackCopies) {
    modResults.EligibleForAdAttributionKitReengagementPostbackCopies = true;
  } else {
    delete modResults.EligibleForAdAttributionKitReengagementPostbackCopies;
  }

  if (options.metaAppId !== undefined) {
    modResults.FacebookAppID = options.metaAppId;
  }

  // A declared Meta app means AttributionKit owns the final conversion-
  // reporting setting. `true` demotes Meta to an event transport; `false`
  // makes Meta the external conversion authority. When this plugin neither
  // receives a Meta app id nor an explicit disable request, it leaves another
  // plugin's Meta state untouched.
  if (options.metaAppId !== undefined || options.disableMetaConversionReporting === true) {
    modResults.FacebookSKAdNetworkReportEnabled =
      options.disableMetaConversionReporting !== true;
  }

  return modResults;
}

function withAttribution(config, rawOptions) {
  const options = normalizeOptions(rawOptions);
  config = withInfoPlist(config, (mod) => {
    applyInfoPlist(mod.modResults, options);
    return mod;
  });
  return withEntitlementsPlist(config, (mod) => {
    const current = Array.isArray(mod.modResults['com.apple.developer.associated-domains'])
      ? mod.modResults['com.apple.developer.associated-domains']
      : [];
    mod.modResults['com.apple.developer.associated-domains'] = [
      ...new Set([...current, ...options.associatedDomains.map((domain) => `applinks:${domain}`)]),
    ];
    return mod;
  });
}

withAttribution._internal = { applyInfoPlist, mergeSKAdNetworkItems, normalizeOptions };

module.exports = withAttribution;
