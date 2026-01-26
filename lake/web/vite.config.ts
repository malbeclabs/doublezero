import path from 'path'
import fs from 'fs'
import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

const buildTimestamp = new Date().toISOString()

// Plugin to write version.json to dist folder after build
function versionPlugin(): Plugin {
  return {
    name: 'version-plugin',
    writeBundle() {
      const versionInfo = { buildTimestamp }
      fs.writeFileSync(
        path.resolve(__dirname, 'dist', 'version.json'),
        JSON.stringify(versionInfo)
      )
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), versionPlugin()],
  define: {
    __BUILD_TIMESTAMP__: JSON.stringify(buildTimestamp),
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: process.env.VITE_API_URL || 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
