// Release-completeness guard for the UI remote (issue #10, fixed by b1089a5):
// webpack MF lazy-loads async chunks next to remoteEntry.js, so every dist
// *.js must be uploaded to the UI GitHub Release. Exits 1 listing any dist
// file missing from the release assets.
import { readdirSync } from 'node:fs';
import { execSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const { basename, resolve, dirname } = path;

const isRequired = (name) => name.endsWith('.js') || name.endsWith('.js.LICENSE.txt');

export function findMissingAssets(distFiles, assetNames) {
  const uploaded = new Set(assetNames);
  return distFiles.map((f) => basename(f)).filter(isRequired).filter((name) => !uploaded.has(name));
}

const isMain = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) {
  const tag = process.argv[2];
  if (!tag) {
    console.error('usage: node scripts/check-release-assets.mjs <ui-release-tag>');
    process.exit(2);
  }
  const distDir = resolve(dirname(fileURLToPath(import.meta.url)), '..', 'dist');
  const distFiles = readdirSync(distDir);
  const assetsJson = execSync(`gh release view "${tag}" --json assets`, { encoding: 'utf8' });
  const assetNames = JSON.parse(assetsJson).assets.map((a) => a.name);
  const missing = findMissingAssets(distFiles, assetNames);
  if (missing.length > 0) {
    console.error(`Release ${tag} is missing ${missing.length} dist asset(s):`);
    for (const name of missing) console.error(`  - ${name}`);
    process.exit(1);
  }
  console.log(`Release ${tag} contains all ${distFiles.filter(isRequired).length} dist js assets.`);
}
