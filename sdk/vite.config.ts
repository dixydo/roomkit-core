import { defineConfig } from 'vite'
import { resolve } from 'node:path'

export default defineConfig({
  build: {
    target: 'es2020',
    sourcemap: true,
    minify: 'esbuild',
    lib: {
      entry: resolve(__dirname, 'src/index.ts'),
      name: 'Roomkit',
      fileName: (format) => `roomkit-sdk.${format === 'umd' ? 'umd.cjs' : 'js'}`,
      formats: ['es', 'umd'],
    },
  },
})
