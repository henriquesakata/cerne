import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist' },
  server: {
    proxy: { '/api': 'http://localhost:7717' },
  },
  test: {
    environment: 'jsdom',
  },
})
