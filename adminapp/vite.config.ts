import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  base: '/admin/app/',
  plugins: [react()],
  build: {
    // Use stable filenames to avoid 404s during rolling deploys where
    // index.html and hashed assets may temporarily come from different instances.
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name].js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name][extname]',
      },
    },
  },
  server: {
    port: 3001,
    strictPort: true,
  },
});
