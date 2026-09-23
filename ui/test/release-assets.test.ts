// @vitest-environment node
import { describe, it, expect } from 'vitest';

import { findMissingAssets } from '../scripts/check-release-assets.mjs';

describe('findMissingAssets', () => {
  it('returns empty when every dist js file is uploaded', () => {
    const dist = ['remoteEntry.js', '616.js', '616.js.LICENSE.txt', 'styles.css'];
    const assets = ['remoteEntry.js', '616.js', '616.js.LICENSE.txt', 'styles.css'];
    expect(findMissingAssets(dist, assets)).toEqual([]);
  });

  it('flags a missing async chunk (Loading chunk 616 regression)', () => {
    const dist = ['remoteEntry.js', '616.js'];
    const assets = ['remoteEntry.js'];
    expect(findMissingAssets(dist, assets)).toEqual(['616.js']);
  });

  it('ignores non-js dist files', () => {
    const dist = ['remoteEntry.js', 'index.html', 'stats.json'];
    const assets = ['remoteEntry.js'];
    expect(findMissingAssets(dist, assets)).toEqual([]);
  });

  it('treats .js.LICENSE.txt as required when present in dist', () => {
    const dist = ['remoteEntry.js', '616.js.LICENSE.txt'];
    const assets = ['remoteEntry.js'];
    expect(findMissingAssets(dist, assets)).toEqual(['616.js.LICENSE.txt']);
  });

  it('strips directory prefixes from dist paths', () => {
    const dist = ['ui/dist/remoteEntry.js', 'dist/616.js'];
    const assets = ['remoteEntry.js', '616.js'];
    expect(findMissingAssets(dist, assets)).toEqual([]);
  });
});
