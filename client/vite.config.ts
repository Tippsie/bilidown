import { defineConfig } from 'vite'

export default defineConfig({
    clearScreen: false,
    server: {
        proxy: {
            '/api': 'http://localhost:8098'
        },
        host: '0.0.0.0'
    },
    build: {
        outDir: '../server/static',
        emptyOutDir: true,
    },
    css: {
        preprocessorOptions: {
            scss: {
                api: 'modern-compiler'
            }
        }
    }
})
