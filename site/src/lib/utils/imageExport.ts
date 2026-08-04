import { toPng, toJpeg } from 'html-to-image';

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
		cacheBust: true,
		skipAutoScale: true,
		style: {
			transform: 'scale(1)',
			transformOrigin: 'top left'
		}
	};

	if (format === 'jpeg') {
		return await toJpeg(element, { ...config, quality: 0.95 });
	}
	return await toPng(element, config);
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
