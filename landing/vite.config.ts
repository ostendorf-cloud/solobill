import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

// The build goes to landing/dist. `pnpm run prerender` then writes the pages,
// the 404 page of each language and sitemap.xml into the same directory, the
// layout govue's frontend package reads. cmd/site-frontend serves it from
// disk, nothing is embedded.
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  ssr: {
    // govue ships TypeScript and Vue sources. Node cannot load them from
    // node_modules, so the SSR bundle takes them in instead of importing
    // them at run time.
    noExternal: ['@ostendorf-cloud/govue'],
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // The frontend server serves every file of assets/ and answers 404 for a
    // source map, so a map would only break the developer tools.
    sourcemap: false,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./vitest.setup.ts'],
    css: false,
  },
})
