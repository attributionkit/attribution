import { NativeModule, requireNativeModule } from 'expo';

declare class AttributionKitExpoNativeModule extends NativeModule {
  runtimeVersion: string;
  schemaHash(): string;
  conversionValue(event: string): number;
  recordRaw(event: string): Promise<string>;
}

export default requireNativeModule<AttributionKitExpoNativeModule>('AttributionKit');
