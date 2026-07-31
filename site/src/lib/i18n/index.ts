import { writable, derived, get } from 'svelte/store';
import { browser } from '$app/environment';
import { en } from './locales/en';
import { zh } from './locales/zh';

/**
 * Supported concrete locales. English is the fallback for any missing key.
 */
export type Locale = 'en' | 'zh';

/**
 * User-facing preference. `auto` follows the browser language.
 */
export type LocalePreference = 'auto' | Locale;

export const AVAILABLE_LOCALES: Locale[] = ['en', 'zh'];
export const DEFAULT_LOCALE: Locale = 'en';

const STORAGE_KEY = 'locale';

type Dict = Record<string, unknown>;

const dictionaries: Record<Locale, Dict> = { en, zh };

/**
 * Map an arbitrary browser language tag to one of our supported locales.
 * Anything Chinese (zh, zh-CN, zh-TW, ...) resolves to `zh`; everything else
 * falls back to English.
 */
export function matchBrowserLocale(lang: string | undefined | null): Locale {
	if (!lang) return DEFAULT_LOCALE;
	const lower = lang.toLowerCase();
	if (lower.startsWith('zh')) return 'zh';
	return DEFAULT_LOCALE;
}

function detectBrowserLocale(): Locale {
	if (!browser) return DEFAULT_LOCALE;
	const candidates = [
		...(navigator.languages ?? []),
		navigator.language
	].filter(Boolean) as string[];
	for (const cand of candidates) {
		const lower = cand.toLowerCase();
		if (lower.startsWith('zh')) return 'zh';
		if (lower.startsWith('en')) return 'en';
	}
	// No explicit match: honour the first listed language, else default.
	return matchBrowserLocale(candidates[0]);
}

function readStoredPreference(): LocalePreference {
	if (!browser) return 'auto';
	const stored = localStorage.getItem(STORAGE_KEY);
	if (stored === 'en' || stored === 'zh' || stored === 'auto') {
		return stored;
	}
	return 'auto';
}

/**
 * The raw user preference (auto | en | zh).
 */
export const localePreference = writable<LocalePreference>(readStoredPreference());

/**
 * The concrete locale actually in effect after resolving `auto`.
 */
export const locale = derived(localePreference, ($pref) =>
	$pref === 'auto' ? detectBrowserLocale() : $pref
);

// Keep a synchronous snapshot of the resolved locale so non-reactive helpers
// (e.g. date formatting utilities) can read it without subscribing.
let currentLocale: Locale = DEFAULT_LOCALE;
locale.subscribe((value) => {
	currentLocale = value;
	if (browser) {
		document.documentElement.setAttribute('lang', value === 'zh' ? 'zh-CN' : 'en');
	}
});

export function getCurrentLocale(): Locale {
	return currentLocale;
}

/**
 * The BCP-47 tag used with Intl / toLocaleDateString.
 */
export function getIntlLocale(): string {
	return currentLocale === 'zh' ? 'zh-CN' : 'en-US';
}

function lookup(dict: Dict, key: string): string | undefined {
	const value = key.split('.').reduce<unknown>((acc, part) => {
		if (acc && typeof acc === 'object' && part in (acc as Dict)) {
			return (acc as Dict)[part];
		}
		return undefined;
	}, dict);
	return typeof value === 'string' ? value : undefined;
}

function interpolate(template: string, params?: Record<string, string | number>): string {
	if (!params) return template;
	return template.replace(/\{(\w+)\}/g, (match, name) => {
		const replacement = params[name];
		return replacement === undefined ? match : String(replacement);
	});
}

/**
 * Resolve a translation key for a given locale, with English fallback and the
 * key itself as a last resort.
 */
export function translate(
	loc: Locale,
	key: string,
	params?: Record<string, string | number>
): string {
	const value = lookup(dictionaries[loc], key) ?? lookup(dictionaries.en, key) ?? key;
	return interpolate(value, params);
}

function lookupArray(dict: Dict, key: string): string[] | undefined {
	const value = key.split('.').reduce<unknown>((acc, part) => {
		if (acc && typeof acc === 'object' && part in (acc as Dict)) {
			return (acc as Dict)[part];
		}
		return undefined;
	}, dict);
	return Array.isArray(value) ? (value as string[]) : undefined;
}

/**
 * Resolve a translation key expected to hold a string array (e.g. month names),
 * with English fallback and an empty array as a last resort.
 */
export function translateArray(loc: Locale, key: string): string[] {
	return lookupArray(dictionaries[loc], key) ?? lookupArray(dictionaries.en, key) ?? [];
}

/**
 * Reactive translation function. Usage in components: `$t('some.key')`.
 * Re-runs automatically whenever the effective locale changes.
 */
export const t = derived(
	locale,
	($loc) =>
		(key: string, params?: Record<string, string | number>): string =>
			translate($loc, key, params)
);

/**
 * Reactive array translation. Usage: `$ta('calendar.months')`.
 */
export const ta = derived(
	locale,
	($loc) =>
		(key: string): string[] =>
			translateArray($loc, key)
);

/**
 * Set the user preference. Persists to localStorage immediately so the choice
 * survives reloads and applies even when logged out.
 */
export function setLocalePreference(pref: LocalePreference) {
	localePreference.set(pref);
	if (browser) {
		localStorage.setItem(STORAGE_KEY, pref);
	}
}

/**
 * Non-reactive helper to read the current preference synchronously.
 */
export function getLocalePreference(): LocalePreference {
	return get(localePreference);
}

/**
 * Initialise from the stored preference. Safe to call on mount.
 */
export function initLocale() {
	if (!browser) return;
	localePreference.set(readStoredPreference());
}
