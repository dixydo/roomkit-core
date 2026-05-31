import { defineConfig } from 'vite'
import { resolve } from 'node:path'

export default defineConfig({
  build: {
    target: 'es2020',
    sourcemap: true,
    minify: 'esbuild',
    lib: {
      entry: resolve(__dirname, 'src/index.ts'),
      name: 'RoomkitReact',
      fileName: (format) => `roomkit-react.${format === 'umd' ? 'umd.cjs' : 'js'}`,
      formats: ['es', 'umd'],
    },
    rollupOptions: {
      external: ['react', 'react/jsx-runtime', '@dixydo/roomkit'],
      output: {
        globals: {
          react: 'React',
          'react/jsx-runtime': 'jsxRuntime',
          '@dixydo/roomkit': 'Roomkit',
        },
      },
    },
  },
})
