import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// Vite 配置保持尽量精简，便于直接部署到 Cloudflare Pages。
export default defineConfig({
  plugins: [react(), tailwindcss()],
});
