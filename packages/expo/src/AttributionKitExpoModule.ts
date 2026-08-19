import { NativeModule, requireNativeModule } from 'expo';

declare class AttributionKitExpoNativeModule extends NativeModule {
  runtimeVersion: string;
  ready(): Promise<void>;
  trackRaw(event: string, propertiesJSON: string, consentState: string): Promise<void>;
  diagnosticsRaw(): Promise<string>;
  flushRaw(): Promise<string>;
  schemaHash(): string;
  conversionValue(event: string): number;
  recordRaw(event: string): Promise<string>;
}

export default requireNativeModule<AttributionKitExpoNativeModule>('AttributionKit');
