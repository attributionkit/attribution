import { constants, copyFile, mkdir, readdir, readFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const sourceDirectory = resolve(here, '../../../native/AttributionCore/Sources/AttributionCore');
const targetDirectory = resolve(here, '../ios/Core');
const check = process.argv.includes('--check');
const files = (await readdir(sourceDirectory)).filter((name) => name.endsWith('.swift')).sort();
const resources = [{
  source: resolve(sourceDirectory, 'PrivacyInfo.xcprivacy'),
  target: resolve(here, '../ios/PrivacyInfo.xcprivacy'),
}];

if (check) {
  const targetFiles = (await readdir(targetDirectory)).filter((name) => name.endsWith('.swift')).sort();
  if (files.join('\n') !== targetFiles.join('\n')) {
    throw new Error('Expo vendored AttributionCore file inventory is stale; run npm run sync-core.');
  }
  for (const file of files) {
    const [expected, actual] = await Promise.all([
      readFile(resolve(sourceDirectory, file)),
      readFile(resolve(targetDirectory, file)),
    ]);
    if (!expected.equals(actual)) {
      throw new Error(`Expo vendored ${file} is stale; run npm run sync-core.`);
    }
  }
  for (const { source, target } of resources) {
    const [expected, actual] = await Promise.all([readFile(source), readFile(target)]);
    if (!expected.equals(actual)) {
      throw new Error('Expo privacy manifest is stale; run npm run sync-core.');
    }
  }
} else {
  await mkdir(targetDirectory, { recursive: true });
  for (const file of files) {
    await copyFile(
      resolve(sourceDirectory, file),
      resolve(targetDirectory, file),
      constants.COPYFILE_FICLONE,
    );
  }
  for (const { source, target } of resources) {
    await copyFile(source, target, constants.COPYFILE_FICLONE);
  }
}
