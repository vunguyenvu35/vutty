/// <reference types="vitest" />
import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  // noVNC (the VNC viewer) uses top-level await, which is ES2022; the web views of the app (WKWebView 15, WebView2, WebKitGTK 2.36) have it
  build: {target: 'es2022'},
  // the dev server pre-bundles libraries with esbuild, whose default target (es2020) refuses that await too
  optimizeDeps: {esbuildOptions: {target: 'es2022'}},
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    css: false,
  },
})
