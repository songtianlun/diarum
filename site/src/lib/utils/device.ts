/**
 * A random ID for this browser, sent with diary reads so visitor statistics
 * can tell the owner's devices apart and count repeat reads from the same
 * device once. It identifies nothing beyond "this browser profile".
 */
const STORAGE_KEY = 'diarum_device_id';
const HEADER = 'X-Diarum-Device';

let cached = '';

function randomId(): string {
	try {
		if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
			return crypto.randomUUID().replace(/-/g, '').slice(0, 24);
		}
	} catch {
		// Fall through to Math.random.
	}
	return Array.from({ length: 24 }, () => Math.floor(Math.random() * 36).toString(36)).join('');
}

export function deviceId(): string {
	if (cached) return cached;
	try {
		const stored = localStorage.getItem(STORAGE_KEY);
		if (stored && /^[A-Za-z0-9_-]{8,40}$/.test(stored)) {
			cached = stored;
			return cached;
		}
		cached = randomId();
		localStorage.setItem(STORAGE_KEY, cached);
	} catch {
		// Private mode or storage blocked: keep an ID for this page load.
		cached = cached || randomId();
	}
	return cached;
}

/** Headers to merge into diary read requests. */
export function deviceHeaders(): Record<string, string> {
	if (typeof window === 'undefined') return {};
	return { [HEADER]: deviceId() };
}
