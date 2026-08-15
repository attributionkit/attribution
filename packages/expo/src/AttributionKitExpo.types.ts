export type AttributionEvent = 'install' | 'trial' | 'purchase' | 'retention';

export type AttributionBackendStatus = 'succeeded' | 'failed' | 'unavailable';

export interface AttributionBackendResult {
  status: AttributionBackendStatus;
  error?: string | null;
}

export interface AttributionUpdateReport {
  event: string;
  fineConversionValue: number;
  schemaHash: string;
  adAttributionKit: AttributionBackendResult;
  skAdNetwork: AttributionBackendResult;
}
