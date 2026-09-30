import { toPng, toJpeg } from 'html-to-image';
import { pb } from '$lib/api/client';

// Share options interface
export interface ShareOptions {
	showDate: boolean;
	showMood: boolean;
	showWeather: boolean;
	showTags: boolean;
	showImages: boolean;
	showBranding: boolean;
	theme: ThemeId;
	width: number;
	scale: number;
}

// Default share options
export const defaultShareOptions: ShareOptions = {
	showDate: true,
	showMood: false,
	showWeather: false,
	showTags: false,
	showImages: true,
	showBranding: true,
	theme: 'warm-paper',
	width: 800,
	scale: 2
};

// Theme types
export type ThemeId = 'warm-paper' | 'dark-elegant' | 'minimal-white' | 'nature-green' | 'win95';

/**
 * How the preview lays itself out. `card` is the plain document look shared by
 * the original four themes; `win95` swaps in full Notepad window chrome
 * (title bar, menu bar, sunken client area, status bar).
 */
export type ThemeVariant = 'card' | 'win95';

export interface Theme {
	id: ThemeId;
	name: string;
	nameZh: string;
	background: string;
	foreground: string;
	mutedForeground: string;
	accent: string;
	border: string;
	fontFamily: string;
	/** Defaults to 'card' when omitted. */
	variant?: ThemeVariant;
}

// Predefined themes
export const themes: Record<ThemeId, Theme> = {
	'warm-paper': {
		id: 'warm-paper',
		name: 'Warm Paper',
		nameZh: '温暖纸张',
		background: '#faf8f5',
		foreground: '#4a3f35',
		mutedForeground: '#8b7355',
		accent: '#d4a574',
		border: '#e8e0d5',
		fontFamily: 'Georgia, "Times New Roman", serif'
	},
	'dark-elegant': {
		id: 'dark-elegant',
		name: 'Dark Elegant',
		nameZh: '深色优雅',
		background: '#1a1f2e',
		foreground: '#e8e6e3',
		mutedForeground: '#9ca3af',
		accent: '#6366f1',
		border: '#374151',
		fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif'
	},
	'minimal-white': {
		id: 'minimal-white',
		name: 'Minimal White',
		nameZh: '极简白',
		background: '#ffffff',
		foreground: '#1f2937',
		mutedForeground: '#6b7280',
		accent: '#3b82f6',
		border: '#e5e7eb',
		fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif'
	},
	'nature-green': {
		id: 'nature-green',
		name: 'Nature Green',
		nameZh: '自然绿',
		background: '#f0f7f4',
		foreground: '#1e3a2f',
		mutedForeground: '#4a7c59',
		accent: '#22c55e',
		border: '#d1e7dd',
		fontFamily: '"Palatino Linotype", "Book Antiqua", Palatino, serif'
	},
	win95: {
		id: 'win95',
		name: 'Windows 95',
		nameZh: 'Windows 95',
		// The client area is the white Notepad page; the #c0c0c0 face lives on
		// the window frame and is applied by the preview's win95 branch.
		background: '#ffffff',
		foreground: '#000000',
		mutedForeground: '#404040',
		accent: '#000080',
		border: '#808080',
		fontFamily:
			"'MS Sans Serif', 'Microsoft Sans Serif', Tahoma, Geneva, Verdana, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'WenQuanYi Micro Hei', sans-serif",
		variant: 'win95'
	}
};

/**
 * The share theme that matches a diary view. Opening the share dialog from a
 * given view preselects its theme so the exported image looks like the page
 * it came from; the user can still pick any other theme afterwards.
 */
export function shareThemeForVisualStyle(style: string | null | undefined): ThemeId {
	return style === 'win95' ? 'win95' : defaultShareOptions.theme;
}

// Export image format
export type ImageFormat = 'png' | 'jpeg';

// A transparent pixel drawn in place of an image that cannot be read, so one
// broken image never makes the whole export fail.
const IMAGE_PLACEHOLDER = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';

// Images already turned into data URLs, by source, for repeated exports.
const inlinedImages = new Map<string, Promise<string | null>>();

function blobToDataUrl(blob: Blob): Promise<string> {
	return new Promise((resolve, reject) => {
		const reader = new FileReader();
		reader.onload = () => resolve(String(reader.result));
		reader.onerror = () => reject(reader.error);
		reader.readAsDataURL(blob);
	});
}

async function fetchAsDataUrl(url: string, init?: RequestInit): Promise<string | null> {
	try {
		const response = await fetch(url, init);
		if (!response.ok) return null;
		const blob = await response.blob();
		if (!blob.type.startsWith('image/')) return null;
		return await blobToDataUrl(blob);
	} catch {
		return null;
	}
}

/**
 * Reads an image from another origin as a data URL. Image hosts such as
 * Chevereto CDNs often send no CORS headers, so the browser refuses to let the
 * page read them. They go through the server's image proxy first; a direct
 * read is only tried if the proxy cannot reach the host.
 */
function inlineExternalImage(src: string): Promise<string | null> {
	let pending = inlinedImages.get(src);
	if (!pending) {
		pending = (async () => {
			const token = pb.authStore.token;
			const proxied = await fetchAsDataUrl(`/api/v1/image-proxy?url=${encodeURIComponent(src)}`, {
				headers: token ? { Authorization: `Bearer ${token}` } : undefined
			});
			return proxied ?? fetchAsDataUrl(src, { mode: 'cors', credentials: 'omit' });
		})();
		inlinedImages.set(src, pending);
		// Do not remember failures: the next export tries again.
		pending.then((value) => value || inlinedImages.delete(src));
	}
	return pending;
}

function isExternal(src: string): boolean {
	if (!src || src.startsWith('data:') || src.startsWith('blob:')) return false;
	try {
		const url = new URL(src, location.href);
		return (url.protocol === 'http:' || url.protocol === 'https:') && url.origin !== location.origin;
	} catch {
		return false;
	}
}

/**
 * Diarum's own image URLs may redirect to the S3 bucket's public URL, which
 * usually sends no CORS headers. For exports they are read with ?direct=1,
 * which always streams from this origin.
 */
function directMediaUrl(src: string): string | null {
	try {
		const url = new URL(src, location.href);
		if (url.origin !== location.origin || !url.pathname.startsWith('/api/v1/files/media/')) return null;
		url.searchParams.set('direct', '1');
		return url.toString();
	} catch {
		return null;
	}
}

/**
 * Swaps every cross-origin image inside element for an inlined copy while fn
 * runs, then puts the original sources back.
 */
async function withInlinedImages<T>(element: HTMLElement, fn: () => Promise<T>): Promise<T> {
	const images = Array.from(element.querySelectorAll('img')).filter((img) => {
		const src = img.currentSrc || img.src;
		return isExternal(src) || directMediaUrl(src) !== null;
	});
	const restore: Array<() => void> = [];
	await Promise.all(
		images.map(async (img) => {
			const src = img.currentSrc || img.src;
			const direct = directMediaUrl(src);
			const inlined = direct ? await fetchAsDataUrl(direct) : await inlineExternalImage(src);
			// Diarum's own image stays as is if it cannot be read; the export
			// can still load it from this origin.
			if (direct && !inlined) return;
			const original = { src: img.getAttribute('src'), srcset: img.getAttribute('srcset') };
			img.removeAttribute('srcset');
			img.src = inlined ?? IMAGE_PLACEHOLDER;
			if (img.decode) await img.decode().catch(() => undefined);
			restore.push(() => {
				if (original.srcset !== null) img.setAttribute('srcset', original.srcset);
				if (original.src !== null) img.setAttribute('src', original.src);
			});
		})
	);
	try {
		return await fn();
	} finally {
		for (const undo of restore) undo();
	}
}

// Generate image from element
export async function generateImage(
	element: HTMLElement,
	options: ShareOptions,
	format: ImageFormat = 'png'
): Promise<string> {
	const config = {
		width: options.width,
		height: element.offsetHeight,
		pixelRatio: options.scale,
		// Media URLs never change content, so the HTTP cache is safe to use;
		// busting it would also defeat the inlined copies above.
		cacheBust: false,
		skipAutoScale: true,
		imagePlaceholder: IMAGE_PLACEHOLDER,
		style: {
			transform: 'scale(1)',
			transformOrigin: 'top left'
		}
	};

	return withInlinedImages(element, async () => {
		if (format === 'jpeg') {
			return await toJpeg(element, { ...config, quality: 0.95 });
		}
		return await toPng(element, config);
	});
}

// Download image
export function downloadImage(dataUrl: string, filename: string): void {
	const link = document.createElement('a');
	link.download = filename;
	link.href = dataUrl;
	document.body.appendChild(link);
	link.click();
	document.body.removeChild(link);
}

// Copy image to clipboard
export async function copyImageToClipboard(dataUrl: string): Promise<boolean> {
	try {
		const response = await fetch(dataUrl);
		const blob = await response.blob();
		await navigator.clipboard.write([
			new ClipboardItem({ 'image/png': blob })
		]);
		return true;
	} catch (error) {
		console.error('Failed to copy image to clipboard:', error);
		return false;
	}
}

// Share via Web Share API (mobile)
export async function shareImage(dataUrl: string, title: string): Promise<boolean> {
	if (!navigator.share || !navigator.canShare) {
		return false;
	}

	try {
		const response = await fetch(dataUrl);
		const blob = await response.blob();
		const file = new File([blob], `${title}.png`, { type: 'image/png' });

		if (navigator.canShare({ files: [file] })) {
			await navigator.share({
				title,
				files: [file]
			});
			return true;
		}
		return false;
	} catch (error) {
		if ((error as Error).name !== 'AbortError') {
			console.error('Failed to share image:', error);
		}
		return false;
	}
}

// Check if Web Share API is available
export function canShare(): boolean {
	return typeof navigator !== 'undefined' &&
		'share' in navigator &&
		'canShare' in navigator;
}
