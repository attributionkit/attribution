const unsupported = (): never => {
  throw new Error('@attributionkit/expo is available only in an Apple native build.');
};

export default {
  runtimeVersion: '0.1.0-preview.4',
  schemaHash: unsupported,
  conversionValue: unsupported,
  recordRaw: async () => unsupported(),
};
