import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'node:path'

export default defineConfig({
  plugins: [vue()],
  build: {
    target: 'es2020',
    sourcemap: true,
    minify: 'esbuild',
    lib: {
      entry: resolve(__dirname, 'src/index.ts'),
      name: 'RoomkitVue',
      fileName: (format) => `roomkit-vue.${format === 'umd' ? 'umd.cjs' : 'js'}`,
      formats: ['es', 'umd'],
    },
    rollupOptions: {
      external: ['vue', '@dixydo/roomkit'],
      output: {
        globals: { vue: 'Vue', '@dixydo/roomkit': 'Roomkit' },
      },
    },
  },
})
