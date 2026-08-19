'use strict';

const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const test = require('node:test');
const vm = require('node:vm');
const ts = require('typescript');

function loadTypeScriptFallback(name) {
  const source = readFileSync(join(__dirname, '..', 'src', name), 'utf8');
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
    fileName: name,
    reportDiagnostics: true,
  });
  assert.deepEqual(compiled.diagnostics, []);

  const module = { exports: {} };
  vm.runInNewContext(compiled.outputText, {
    exports: module.exports,
    module,
  });
  return module.exports.default;
}

for (const file of [
  'AttributionKitExpoModule.android.ts',
  'AttributionKitExpoModule.web.ts',
]) {
  test(`${file} is import-safe and fails only when an Apple API is invoked`, async () => {
    const fallback = loadTypeScriptFallback(file);

    assert.equal(fallback.runtimeVersion, '0.1.0-preview.5');
    assert.throws(() => fallback.schemaHash(), /available only in an Apple native build/);
    assert.throws(() => fallback.conversionValue('install'), /available only in an Apple native build/);
    await assert.rejects(
      fallback.recordRaw('install'),
      /available only in an Apple native build/,
    );
  });
}
