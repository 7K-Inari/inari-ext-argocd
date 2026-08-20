import webpack from 'webpack';

const { container } = webpack;
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

// Shared-dependency singleton contract (inari-ui-plugin-sdk
// docs/shared-dependencies.md): never bundle react/react-dom/zod or the SDK
// itself — the host shell provides them.
const shared = {
  react: { singleton: true, requiredVersion: '^18.3.1' },
  'react-dom': { singleton: true, requiredVersion: '^18.3.1' },
  zod: { singleton: true },
  '@inari/ui-plugin-sdk': { singleton: true },
};

export default {
  entry: './src/index.tsx',
  mode: 'production',
  output: { path: resolve(here, 'dist'), publicPath: 'auto', clean: true },
  resolve: { extensions: ['.tsx', '.ts', '.js'] },
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        use: [{ loader: 'ts-loader', options: { configFile: 'tsconfig.build.json' } }],
        exclude: /node_modules/,
      },
    ],
  },
  plugins: [
    new container.ModuleFederationPlugin({
      name: 'inari_ext_argocd',
      filename: 'remoteEntry.js',
      exposes: { './extension': './src/index.tsx' },
      shared,
    }),
  ],
};
