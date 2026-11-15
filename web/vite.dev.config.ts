import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const root = fileURLToPath(new URL('.', import.meta.url))
const proxy = { target: 'http://api:8080', changeOrigin: false }

// Docker-only product development; production keeps vite.config.ts and Nginx.
export default defineConfig({
  root,
  envDir: false,
  cacheDir: '/home/node/.cache/wsl-vite',
  define: { __APP_VERSION__: JSON.stringify('0.1.0-dev') },
  plugins: [
    {
      name: 'wsl-product-source-boundary',
      configureServer(server) {
        server.middlewares.use((request, response, next) => {
          let pathname: string
          try {
            pathname = decodeURIComponent(new URL(request.url || '/', 'http://localhost').pathname)
          } catch {
            response.statusCode = 400
            response.end()
            return
          }
          if (
            /(?:^|\/)(?:\.env[^/]*|\.git)(?:\/|$)/.test(pathname) ||
            /\.(?:crt|pem|key)$/i.test(pathname)
          ) {
            response.statusCode = 403
            response.end()
            return
          }
          const productChart =
            /^\/src\/components\/core\/charts\/(?:art-(?:bar|ring)-chart\/index\.vue|useChartSurface\.ts)$/.test(
              pathname
            )
          if (
            pathname.startsWith('/src/') &&
            !/^\/src\/(wsl|assets)\//.test(pathname) &&
            !productChart
          ) {
            response.statusCode = 404
            response.end()
            return
          }
          next()
        })
      }
    },
    vue()
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      '@styles': fileURLToPath(new URL('./src/assets/styles', import.meta.url))
    }
  },
  server: {
    host: '0.0.0.0',
    port: 8080,
    strictPort: true,
    open: false,
    allowedHosts: ['localhost', '127.0.0.1'],
    hmr: {
      path: '/__wsl_hmr',
      clientPort: Number(process.env.WSL_DEV_PORT || 5173)
    },
    fs: {
      strict: true,
      allow: [root],
      deny: ['**/.env*', '**/*.{crt,pem,key}', '**/.git/**']
    },
    watch: {
      usePolling: process.env.WSL_DEV_POLLING === 'true',
      interval: 1000
    },
    proxy: {
      '^/api/v1/(system/monitor/events|accounts/[0-9a-f-]+/events|assistant/turns/[0-9a-f-]+/events)(\\?.*)?$':
        {
          ...proxy,
          timeout: 105000,
          proxyTimeout: 105000
        },
      // The API retains its own ordinary/explicit extended endpoint deadlines.
      '/api/': proxy,
      '/healthz': proxy
    }
  },
  css: {
    preprocessorMaxWorkers: 0,
    preprocessorOptions: {
      scss: {
        additionalData: `
          @use "@styles/core/el-light.scss" as *;
          @use "@styles/core/mixin.scss" as *;
        `
      }
    }
  }
})
