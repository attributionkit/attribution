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

export type AttributionJSONValue =
  | string
  | number
  | boolean
  | null
  | AttributionJSONValue[]
  | { [key: string]: AttributionJSONValue };

export type AttributionProperties = Record<string, AttributionJSONValue>;

export type AttributionConsentState = 'granted' | 'denied' | 'not_required' | 'unknown';

export interface AttributionDiagnostics {
  runtimeVersion: string;
  manifestVersion?: string | null;
  appInstanceId?: string | null;
  queuedRecordCount: number;
  oldestQueuedAt?: string | null;
  lastFlushAt?: string | null;
  lastFlushErrorCode?: string | null;
  lastDeepLink?: string | null;
  adServicesCollection: string;
  conversionAuthority?: 'managed_apple' | 'external_mmp' | 'external_provider' | 'none' | null;
}

export interface AttributionFlushResult {
  attempted: number;
  acknowledged: number;
  remaining: number;
}
