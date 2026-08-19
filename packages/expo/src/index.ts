import NativeModule from './AttributionKitExpoModule';
import type {
  AttributionConsentState,
  AttributionDiagnostics,
  AttributionEvent,
  AttributionFlushResult,
  AttributionProperties,
  AttributionUpdateReport,
} from './AttributionKitExpo.types';

export * from './AttributionKitExpo.types';

export const runtimeVersion = NativeModule.runtimeVersion;

export const Attribution = {
  ready(): Promise<void> {
    return NativeModule.ready();
  },

  track(
    event: AttributionEvent | string,
    properties: AttributionProperties = {},
    consentState: AttributionConsentState = 'unknown',
  ): Promise<void> {
    return NativeModule.trackRaw(event, JSON.stringify(properties), consentState);
  },

  async getDiagnostics(): Promise<AttributionDiagnostics> {
    return JSON.parse(await NativeModule.diagnosticsRaw()) as AttributionDiagnostics;
  },

  async flush(): Promise<AttributionFlushResult> {
    return JSON.parse(await NativeModule.flushRaw()) as AttributionFlushResult;
  },
};

export function schemaHash(): string {
  return NativeModule.schemaHash();
}

export function conversionValue(event: AttributionEvent | string): number {
  return NativeModule.conversionValue(event);
}

export async function record(event: AttributionEvent | string): Promise<AttributionUpdateReport> {
  return JSON.parse(await NativeModule.recordRaw(event)) as AttributionUpdateReport;
}
