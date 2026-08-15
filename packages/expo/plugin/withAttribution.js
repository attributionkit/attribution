'use strict';

const { withInfoPlist } = require('expo/config-plugins');

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

  return {
    endpoint,
    skAdNetworkIds,
    events,
    schemaHash: options.schemaHash,
    metaAppId,
    disableMetaConversionReporting: options.disableMetaConversionReporting,
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
  modResults.NSAdvertisingAttributionReportEndpoint = options.endpoint;
  modResults.SKAdNetworkItems = mergeSKAdNetworkItems(
    modResults.SKAdNetworkItems,
    options.skAdNetworkIds,
  );
  modResults.AttributionKitSchemaHash = options.schemaHash;
  modResults.AttributionKitEventValues = Object.fromEntries(
    options.events.map((event, index) => [event, index]),
  );

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
  return withInfoPlist(config, (mod) => {
    applyInfoPlist(mod.modResults, options);
    return mod;
  });
}

withAttribution._internal = { applyInfoPlist, mergeSKAdNetworkItems, normalizeOptions };

module.exports = withAttribution;
