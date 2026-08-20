// Temporary workaround (SDK gap, see README "SDK gaps"): @inari/ui-plugin-sdk
// is not published to npm yet, and npm's git-dependency tarball respects the
// package "files" whitelist which excludes tsup.config.ts/src, so the
// installed copy cannot be built in place. Until the SDK is published, we
// clone it (pinned ref), install, and build dist/ into node_modules. Delete
// this script and the postinstall hook once the SDK is on npm.
import { existsSync, rmSync, cpSync } from 'node:fs';
import { execSync } from 'node:child_process';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const SDK_GIT = 'https://github.com/7K-Inari/inari-ui-plugin-sdk';
const SDK_REF = 'main'; // pin a commit sha for reproducibility
const uiDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const sdkDir = resolve(uiDir, 'node_modules/@inari/ui-plugin-sdk');
// Build outside node_modules: npm refuses to install devDependencies in a
// directory nested under another project's node_modules.
const buildDir = resolve(uiDir, '.sdk-build');

// Minimal env for nested npm: the ambient session leaks npm lifecycle vars
// (npm_config_*, INIT_CWD, stale PWD, ...) that make nested installs silently
// no-op. Pass only the essentials.
const cleanEnv = {
  PATH: process.env.PATH,
  HOME: process.env.HOME,
  LANG: process.env.LANG ?? 'C.UTF-8',
};
const run = (cmd, cwd) => execSync(cmd, { cwd, stdio: 'inherit', env: cleanEnv });

if (existsSync(resolve(sdkDir, 'dist/index.js'))) {
  console.log('SDK dist already built');
  process.exit(0);
}
console.log(`Building @inari/ui-plugin-sdk from ${SDK_GIT}#${SDK_REF} (not yet published to npm)...`);
rmSync(buildDir, { recursive: true, force: true });
execSync(`git clone --depth 1 --branch ${SDK_REF} ${SDK_GIT} "${buildDir}"`, { stdio: 'inherit' });
run('npm ci --ignore-scripts', buildDir);
run('npm run build', buildDir);
rmSync(sdkDir, { recursive: true, force: true });
cpSync(buildDir, sdkDir, { recursive: true });
rmSync(buildDir, { recursive: true, force: true });
console.log('SDK built and installed');
