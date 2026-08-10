import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { fileURLToPath, URL } from 'node:url'
import type { Plugin } from 'vite'

const frontendRoot = fileURLToPath(new URL('.', import.meta.url)).replaceAll('\\', '/').replace(/\/$/, '')

function normalizeModuleId(rawId: string) {
  let moduleId = rawId.replaceAll('\\', '/')
  const queryIndex = moduleId.indexOf('?')
  if (queryIndex >= 0) moduleId = moduleId.slice(0, queryIndex)

  if (moduleId.startsWith(`${frontendRoot}/`)) {
    return moduleId.slice(frontendRoot.length + 1)
  }

  const nodeModulesIndex = moduleId.lastIndexOf('/node_modules/')
  if (nodeModulesIndex >= 0) {
    return moduleId.slice(nodeModulesIndex + 1)
  }

  if (moduleId.startsWith('/') || /^[A-Za-z]:\//.test(moduleId)) {
    throw new Error(`cannot normalize absolute module id: ${rawId}`)
  }

  return moduleId
}

function moduleEvidencePlugin(): Plugin {
  return {
    name: 'module-evidence',
    apply: 'build',
    generateBundle(_options, bundle) {
      const chunks = []
      for (const output of Object.values(bundle)) {
        if (output.type !== 'chunk') continue
        chunks.push({
          file: output.fileName,
          modules: [...new Set(Object.keys(output.modules).map(normalizeModuleId))].sort(),
        })
      }
      chunks.sort((left, right) => left.file.localeCompare(right.file))
      this.emitFile({
        type: 'asset',
        fileName: '.vite/module-evidence.json',
        source: `${JSON.stringify({ version: 1, chunks }, null, 2)}\n`,
      })
    },
  }
}

export default defineConfig(({ mode }) => {
  const measuring = mode === 'measure'

  return {
    plugins: [
      vue(),
      AutoImport({
        dts: 'src/auto-imports.d.ts',
        resolvers: [ElementPlusResolver()],
      }),
      Components({
        dts: 'src/components.d.ts',
        resolvers: [ElementPlusResolver({ importStyle: 'css' })],
      }),
      ...(measuring ? [moduleEvidencePlugin()] : []),
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    build: {
      manifest: measuring,
      rollupOptions: {
        output: {
          hashCharacters: 'base36',
        },
      },
    },
    server: {
      proxy: {
        '/api': {
          target: 'http://localhost:8081',
          changeOrigin: true,
        },
        '/oauth/authorize/approve': {
          target: 'http://localhost:8081',
          changeOrigin: true,
        },
        '/oauth/token': {
          target: 'http://localhost:8081',
          changeOrigin: true,
        },
      },
    },
    test: {
      environment: 'jsdom',
      globals: true,
      setupFiles: ['./vitest.setup.ts'],
      server: {
        deps: {
          inline: ['element-plus'],
        },
      },
    },
  }
})
