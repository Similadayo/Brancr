import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  base: '/admin/app/',
  plugins: [react()],
  server: {
    port: 3001,
    strictPort: true,
  },
});
