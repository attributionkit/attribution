import NativeModule from './AttributionKitExpoModule';
import type { AttributionEvent, AttributionUpdateReport } from './AttributionKitExpo.types';

export * from './AttributionKitExpo.types';

export const runtimeVersion = NativeModule.runtimeVersion;

export function schemaHash(): string {
  return NativeModule.schemaHash();
}

export function conversionValue(event: AttributionEvent | string): number {
  return NativeModule.conversionValue(event);
}

export async function record(event: AttributionEvent | string): Promise<AttributionUpdateReport> {
  return JSON.parse(await NativeModule.recordRaw(event)) as AttributionUpdateReport;
}
