import { browser } from '$app/environment';
import { pb } from '$lib/api/client';

/**
 * Loading images at a lighter size first.
 *
 * Entries always reference the original image. When an entry is shown, each
 * image is loaded as the configured variant instead (Chevereto-style
 * `photo.md.jpg` / `photo.th.jpg`, which the built-in library serves too),
 * falling back to the original if the variant fails. A built-in original the
 * browser already caches is shown directly: the HTTP cache is asked without
 * touching the network, which stays correct when the cache is cleared.
 * External (Chevereto) images cannot be probed across origins, so they always
 * start with the variant. The lightbox always loads the original.
 */

export type DisplayQuality = 'th' | 'md' | 'original';
type Variant = 'th' | 'md';

interface DisplayState {
	quality: DisplayQuality;
	/** Chevereto is set up, so external image links may have variants. */
	externalVariants: boolean;
}

const STATE_KEY = 'diarum.imageDisplay';
// Written by an earlier version that guessed cache contents; removed on load.
const LEGACY_SEEN_KEY = 'diarum.imageDisplay.seen';
const NO_VARIANT_HOSTS_KEY = 'diarum.imageDisplay.noVariantHosts';
const BUILTIN_PATH = /^\/api\/v1\/files\/media\/[^/]+\/[^/]+$/;
const BUILTIN_EXTENSIONS = new Set(['jpg', 'jpeg', 'png']);
const EXTERNAL_EXTENSIONS = new Set(['jpg', 'jpeg', 'png', 'webp', 'gif']);

let state: DisplayState = { quality: 'md', externalVariants: false };
let noVariantHosts = new Set<string>();
let initialized = false;
let refreshing: Promise<void> | null = null;
// The session the preference was last fetched for.
let refreshedForToken = '';

function readJSON<T>(key: string, fallback: T): T {
	try {
		const raw = localStorage.getItem(key);
		return raw ? (JSON.parse(raw) as T) : fallback;
	} catch {
		return fallback;
	}
}

function writeJSON(key: string, value: unknown) {
	try {
		localStorage.setItem(key, JSON.stringify(value));
	} catch {
		// Storage full or blocked: the in-memory copy still works this session.
	}
}

function isQuality(value: unknown): value is DisplayQuality {
	return value === 'th' || value === 'md' || value === 'original';
}

/** Loads the cached preference synchronously, then refreshes it from the server. */
function ensureInitialized() {
	if (!browser) return;
	if (initialized) {
		if (pb.authStore.token && pb.authStore.token !== refreshedForToken) void refreshImageDisplay();
		return;
	}
	initialized = true;
	const stored = readJSON<Partial<DisplayState>>(STATE_KEY, {});
	state = {
		quality: isQuality(stored.quality) ? stored.quality : 'md',
		externalVariants: !!stored.externalVariants
	};
	try {
		localStorage.removeItem(LEGACY_SEEN_KEY);
	} catch {
		// Ignore.
	}
	noVariantHosts = new Set(readJSON<string[]>(NO_VARIANT_HOSTS_KEY, []));
	void refreshImageDisplay();
}

/** Re-reads the preference from the server (after login or a settings change). */
export function refreshImageDisplay(): Promise<void> {
	if (!browser || !pb.authStore.token) return Promise.resolve();
	refreshedForToken = pb.authStore.token;
	refreshing ??= (async () => {
		try {
			const headers = { Authorization: `Bearer ${pb.authStore.token}` };
			const [display, upload] = await Promise.all([
				fetch('/api/v1/image-upload/display', { headers }).then((r) => (r.ok ? r.json() : null)),
				fetch('/api/v1/image-upload/settings', { headers }).then((r) => (r.ok ? r.json() : null))
			]);
			const next: DisplayState = { ...state };
			if (isQuality(display?.quality)) next.quality = display.quality;
			if (upload) next.externalVariants = !!(upload.chevereto?.domain && upload.chevereto?.api_key);
			state = next;
			writeJSON(STATE_KEY, state);
		} catch {
			// Keep the cached preference.
		} finally {
			refreshing = null;
		}
	})();
	return refreshing;
}

export function getDisplayQuality(): DisplayQuality {
	ensureInitialized();
	return state.quality;
}

export async function saveDisplayQuality(quality: DisplayQuality): Promise<void> {
	const response = await fetch('/api/v1/image-upload/display', {
		method: 'PUT',
		headers: { Authorization: `Bearer ${pb.authStore.token}`, 'Content-Type': 'application/json' },
		body: JSON.stringify({ quality })
	});
	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new Error(data?.message || 'Failed to save');
	}
	ensureInitialized();
	state = { ...state, quality };
	writeJSON(STATE_KEY, state);
}

export async function loadDisplayQuality(): Promise<DisplayQuality> {
	ensureInitialized();
	await refreshImageDisplay();
	return state.quality;
}

function parse(src: string): URL | null {
	try {
		return new URL(src, window.location.href);
	} catch {
		return null;
	}
}

/**
 * The URL of a smaller variant of `src`, or null when it has none: only the
 * built-in library (JPEG/PNG) and, with Chevereto set up, external image
 * links qualify.
 */
export function variantUrl(src: string, variant: Variant): string | null {
	if (!browser || !src || src.startsWith('data:') || src.startsWith('blob:')) return null;
	ensureInitialized();
	const url = parse(src);
	if (!url || (url.protocol !== 'http:' && url.protocol !== 'https:')) return null;

	const sameOrigin = url.origin === window.location.origin;
	if (sameOrigin && !BUILTIN_PATH.test(url.pathname)) return null;
	if (!sameOrigin && (!state.externalVariants || noVariantHosts.has(url.host))) return null;

	const slash = url.pathname.lastIndexOf('/');
	const name = url.pathname.slice(slash + 1);
	const dot = name.lastIndexOf('.');
	if (dot <= 0) return null;
	const ext = name.slice(dot + 1).toLowerCase();
	if (!(sameOrigin ? BUILTIN_EXTENSIONS : EXTERNAL_EXTENSIONS).has(ext)) return null;
	const stem = name.slice(0, dot);
	if (stem.endsWith('.th') || stem.endsWith('.md')) return null;

	url.pathname = `${url.pathname.slice(0, slash + 1)}${stem}.${variant}.${name.slice(dot + 1)}`;
	return sameOrigin ? `${url.pathname}${url.search}${url.hash}` : url.toString();
}

/**
 * The URL to show first by setting alone: the configured variant, or the
 * original when it has none or the setting asks for originals.
 */
export function displaySrcSync(original: string): string {
	const quality = getDisplayQuality();
	if (quality === 'original') return original;
	return variantUrl(original, quality) ?? original;
}

/**
 * Like displaySrcSync, but also asks the HTTP cache (without touching the
 * network) whether a built-in original is already there.
 */
export async function resolveDisplaySrc(original: string): Promise<string> {
	const chosen = displaySrcSync(original);
	if (chosen === original) return original;
	const url = parse(original);
	if (url && url.origin === window.location.origin) {
		try {
			const controller = new AbortController();
			const response = await fetch(url, { cache: 'only-if-cached', mode: 'same-origin', signal: controller.signal });
			controller.abort(); // only the cache lookup was wanted, not the body
			if (response.ok) return original;
		} catch {
			// Not cached (or the browser does not support the probe).
		}
	}
	return chosen;
}

/**
 * Call when a variant failed to load and the original is being tried: an
 * external host whose images have no variants is skipped from then on.
 */
export function noteVariantFailed(variant: string, original: string) {
	const url = parse(variant);
	if (!url || url.origin === window.location.origin) return;
	const originalUrl = parse(original);
	if (!originalUrl) return;
	// Only blame the host once its original is known to load.
	const probe = new Image();
	probe.onload = () => {
		noVariantHosts.add(originalUrl.host);
		writeJSON(NO_VARIANT_HOSTS_KEY, [...noVariantHosts]);
	};
	probe.src = original;
}

/**
 * Rewrites the images of read-only entry HTML to load their display variant,
 * keeping the original in data-full-src. Pair with the `imageFallback` action.
 */
export function withDisplayImages(html: string): string {
	if (!browser || !html || !html.includes('<img')) return html;
	const doc = new DOMParser().parseFromString(html, 'text/html');
	let changed = false;
	for (const img of doc.querySelectorAll('img[src]')) {
		const original = img.getAttribute('src')!;
		const display = displaySrcSync(original);
		if (display !== original) {
			img.setAttribute('data-full-src', original);
			img.setAttribute('src', display);
			changed = true;
		}
	}
	return changed ? doc.body.innerHTML : html;
}

/**
 * Svelte action for containers of rewritten images: a variant that fails to
 * load is replaced by its original.
 */
export function imageFallback(node: HTMLElement) {
	function handleError(event: Event) {
		const img = event.target;
		if (!(img instanceof HTMLImageElement)) return;
		const original = img.dataset.fullSrc;
		if (!original || img.getAttribute('src') === original) return;
		noteVariantFailed(img.getAttribute('src')!, original);
		img.setAttribute('src', original);
	}
	// Image error events do not bubble; listen in the capture phase.
	node.addEventListener('error', handleError, true);
	return {
		destroy() {
			node.removeEventListener('error', handleError, true);
		}
	};
}
