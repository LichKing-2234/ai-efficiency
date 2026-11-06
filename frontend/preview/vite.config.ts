import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { fileURLToPath, URL } from 'node:url'

// Preview-only config. Serves preview/index.html and swaps the analytics
// composable for the fixture, so the real component renders with no backend.
export default defineConfig({
  root: fileURLToPath(new URL('..', import.meta.url)),
  plugins: [
    vue(),
    AutoImport({ resolvers: [ElementPlusResolver()] }),
    Components({ resolvers: [ElementPlusResolver()] }),
  ],
  resolve: {
    alias: [
      { find: /^@\/composables\/useActivityAnalytics$/, replacement: fileURLToPath(new URL('./useActivityAnalytics.ts', import.meta.url)) },
      { find: '@', replacement: fileURLToPath(new URL('../src', import.meta.url)) },
    ],
  },
  server: { port: 5199, open: '/preview/index.html' },
})
