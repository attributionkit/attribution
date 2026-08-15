import { constants, copyFile, mkdir, readFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const source = resolve(here, '../../../native/AttributionCore/Sources/AttributionCore/AttributionCore.swift');
const target = resolve(here, '../ios/Core/AttributionCore.swift');
const check = process.argv.includes('--check');

if (check) {
  const [expected, actual] = await Promise.all([readFile(source), readFile(target)]);
  if (!expected.equals(actual)) {
    throw new Error('Expo vendored AttributionCore.swift is stale; run npm run sync-core.');
  }
} else {
  await mkdir(dirname(target), { recursive: true });
  await copyFile(source, target, constants.COPYFILE_FICLONE);
}
