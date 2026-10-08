import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import { SvelteKitPWA } from '@vite-pwa/sveltekit';

export default defineConfig({
	plugins: [
		sveltekit(),
		SvelteKitPWA({
			srcDir: './src',
			mode: 'production',
			strategies: 'generateSW',
			scope: '/',
			base: '/',
			selfDestroying: false,
			manifest: {
				name: 'Diarum - Personal Diary',
				short_name: 'Diarum',
				description: 'A simple, elegant, and self-hosted diary application with AI-powered insights.',
				theme_color: '#ffffff',
				background_color: '#ffffff',
				display: 'standalone',
				scope: '/',
				start_url: '/',
				orientation: 'portrait-primary',
				icons: [
					{
						src: '/android-chrome-192x192.png',
						sizes: '192x192',
						type: 'image/png',
						purpose: 'any'
					},
					{
						src: '/android-chrome-192x192.png',
						sizes: '192x192',
						type: 'image/png',
						purpose: 'maskable'
					},
					{
						src: '/android-chrome-512x512.png',
						sizes: '512x512',
						type: 'image/png',
						purpose: 'any'
					},
					{
						src: '/android-chrome-512x512.png',
						sizes: '512x512',
						type: 'image/png',
						purpose: 'maskable'
					}
				],
				screenshots: [
					{
						src: '/screenshots/mobile-light.png',
						sizes: '930x1734',
						type: 'image/png',
						form_factor: 'narrow',
						label: 'Mobile view - Light theme'
					},
					{
						src: '/screenshots/mobile-dark.png',
						sizes: '924x1734',
						type: 'image/png',
						form_factor: 'narrow',
						label: 'Mobile view - Dark theme'
					},
					{
						src: '/screenshots/desktop-light.png',
						sizes: '2522x2012',
						type: 'image/png',
						form_factor: 'wide',
						label: 'Desktop view - Light theme'
					},
					{
						src: '/screenshots/desktop-dark.png',
						sizes: '2544x2018',
						type: 'image/png',
						form_factor: 'wide',
						label: 'Desktop view - Dark theme'
					}
				]
			},
			injectManifest: {
				globPatterns: ['**/*.{js,css,html,ico,png,svg,webp,woff,woff2}']
			},
			workbox: {
				globPatterns: ['**/*.{js,css,html,ico,png,svg,webp,woff,woff2}'],
				// The HEIC decoder (~3 MB, loaded only to convert HEIC uploads in
				// browsers that cannot decode it) is fetched on demand instead of
				// precached for everyone.
				maximumFileSizeToCacheInBytes: 8 * 1024 * 1024,
				manifestTransforms: [
					async (entries) => ({ manifest: entries.filter((entry) => entry.size <= 2 * 1024 * 1024), warnings: [] })
				],
				cleanupOutdatedCaches: true,
				clientsClaim: true,
				runtimeCaching: [
					{
						urlPattern: /^https:\/\/fonts\.googleapis\.com\/.*/i,
						handler: 'CacheFirst',
						options: {
							cacheName: 'google-fonts-cache',
							expiration: {
								maxEntries: 10,
								maxAgeSeconds: 60 * 60 * 24 * 365 // 365 days
							},
							cacheableResponse: {
								statuses: [0, 200]
							}
						}
					},
					{
						urlPattern: /^https:\/\/fonts\.gstatic\.com\/.*/i,
						handler: 'CacheFirst',
						options: {
							cacheName: 'gstatic-fonts-cache',
							expiration: {
								maxEntries: 10,
								maxAgeSeconds: 60 * 60 * 24 * 365 // 365 days
							},
							cacheableResponse: {
								statuses: [0, 200]
							}
						}
					},
					{
						// Media files bypass the API cache: they are immutable per URL and
						// the browser's HTTP cache serves them, which is also what the
						// editor's cache probe checks before choosing original or variant.
						urlPattern: /\/api\/v1\/files\//i,
						handler: 'NetworkOnly'
					},
					{
						urlPattern: /\/api\/.*/i,
						handler: 'NetworkFirst',
						options: {
							cacheName: 'api-cache',
							networkTimeoutSeconds: 10,
							expiration: {
								maxEntries: 50,
								maxAgeSeconds: 60 * 60 * 24 * 7 // 7 days
							},
							cacheableResponse: {
								statuses: [0, 200]
							}
						}
					}
				]
			},
			devOptions: {
				enabled: false,
				suppressWarnings: true,
				type: 'module'
			}
		})
	],
	server: {
		port: 5173,
		proxy: {
			'/api': {
				target: 'http://localhost:8090',
				changeOrigin: true
			},
			'/_': {
				target: 'http://localhost:8090',
				changeOrigin: true
			}
		}
	}
});
