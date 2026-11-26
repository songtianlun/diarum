import { browser } from '$app/environment';
import { derived, get, writable } from 'svelte/store';
import { locale } from '$lib/i18n';
import { isAuthenticated } from '$lib/api/client';
import {
	fetchCNHolidays,
	getCNHolidayFeatureStatus,
	type CNHolidayDay,
	type CNHolidayPreference,
	type CNHolidayYearResponse
} from '$lib/api/holidays';
import {
	getCNHolidayPreference,
	sanitizeCNHolidays,
	saveCNHolidayPreference
} from '$lib/api/settings';
import { getPreviousDay, parseDate } from '$lib/utils/date';

const PREF_STORAGE_KEY = 'diarum.calendar.cn_holidays';
const CACHE_STORAGE_PREFIX = 'diarum.holidays.cn.v2.';
const CACHE_TTL_MS = 10 * 60 * 1000; // Short local TTL so admin manual overrides propagate quickly

interface CachedYearEntry {
	savedAt: number;
	data: CNHolidayYearResponse;
}

export interface CNDayStatus {
	kind: 'holiday' | 'workday_override' | 'weekend' | 'workday';
	badge?: '休' | '班';
	name?: string;
	shortName?: string;
	isOffDay: boolean;
	isFirstHolidayDay: boolean;
	custom?: boolean;
}

function readStoredPref(): CNHolidayPreference {
	if (!browser) return 'auto';
	return sanitizeCNHolidays(localStorage.getItem(PREF_STORAGE_KEY));
}

/** Whether an admin has globally enabled China mainland holidays for this server. */
export const cnHolidaysGlobalEnabled = writable<boolean>(false);

/** Per-user preference ('auto' follows zh locale when globally enabled). */
export const cnHolidayPreference = writable<CNHolidayPreference>(readStoredPref());

/**
 * Whether China mainland holiday & lieu-day badges are currently active.
 * Always false unless globally enabled by an administrator.
 */
export const cnHolidaysEnabled = derived(
	[cnHolidaysGlobalEnabled, cnHolidayPreference, locale],
	([$globalEnabled, $pref, $locale]) => {
		if (!$globalEnabled) return false;
		if ($pref === 'on') return true;
		if ($pref === 'off') return false;
		return $locale === 'zh';
	}
);

export const holidayDaysByDate = writable<Record<string, CNHolidayDay>>({});

const loadedYears = new Set<number>();
const inFlightYears = new Set<number>();
let initPromise: Promise<void> | null = null;
let initialized = false;

function readYearFromStorage(year: number): CachedYearEntry | null {
	if (!browser) return null;
	try {
		const raw = localStorage.getItem(`${CACHE_STORAGE_PREFIX}${year}`);
		if (!raw) return null;
		const parsed = JSON.parse(raw) as CachedYearEntry;
		if (!parsed || typeof parsed.savedAt !== 'number' || !parsed.data || parsed.data.year !== year) {
			return null;
		}
		return parsed;
	} catch {
		return null;
	}
}

function writeYearToStorage(year: number, data: CNHolidayYearResponse): void {
	if (!browser) return;
	try {
		const entry: CachedYearEntry = { savedAt: Date.now(), data };
		localStorage.setItem(`${CACHE_STORAGE_PREFIX}${year}`, JSON.stringify(entry));
	} catch {
		// Ignore quota errors
	}
}

function mergeYearData(year: number, data: CNHolidayYearResponse): void {
	const prefix = `${year}-`;
	holidayDaysByDate.update((prev) => {
		const next: Record<string, CNHolidayDay> = {};
		for (const [k, v] of Object.entries(prev)) {
			if (!k.startsWith(prefix)) {
				next[k] = v;
			}
		}
		for (const day of data.days ?? []) {
			next[day.date] = day;
		}
		return next;
	});
}

export async function initHolidaySettings(force = false): Promise<void> {
	if (!browser || !get(isAuthenticated)) return;
	if (initialized && !force) return;
	if (initPromise && !force) return initPromise;

	initPromise = (async () => {
		try {
			const status = await getCNHolidayFeatureStatus();
			cnHolidaysGlobalEnabled.set(!!status.enabled);
			if (status.enabled) {
				const pref = await getCNHolidayPreference();
				cnHolidayPreference.set(pref);
				localStorage.setItem(PREF_STORAGE_KEY, pref);
			}
			initialized = true;
		} catch {
			// Leave conservative default (false) on failure so it can retry
		} finally {
			initPromise = null;
		}
	})();

	return initPromise;
}

export function setCNHolidaysGlobalEnabledLocal(enabled: boolean): void {
	cnHolidaysGlobalEnabled.set(enabled);
	initialized = true;
	if (!enabled) {
		holidayDaysByDate.set({});
		loadedYears.clear();
	}
}

export async function setCNHolidayPreference(
	pref: CNHolidayPreference,
	persistToServer = false
): Promise<void> {
	const normalized = sanitizeCNHolidays(pref);
	cnHolidayPreference.set(normalized);
	if (browser) {
		localStorage.setItem(PREF_STORAGE_KEY, normalized);
	}
	if (persistToServer && get(isAuthenticated)) {
		await saveCNHolidayPreference(normalized);
	}
}

export async function ensureHolidayYears(years: number[]): Promise<void> {
	if (!browser || !get(isAuthenticated) || !get(cnHolidaysEnabled)) return;

	const toFetch: number[] = [];

	for (const y of years) {
		if (!Number.isInteger(y) || y < 2000 || y > 2100) continue;
		if (loadedYears.has(y) || inFlightYears.has(y)) continue;

		const cached = readYearFromStorage(y);
		if (cached) {
			mergeYearData(y, cached.data);
			if (Date.now() - cached.savedAt < CACHE_TTL_MS) {
				loadedYears.add(y);
				continue;
			}
		}
		toFetch.push(y);
	}

	const uniqueFetch = Array.from(new Set(toFetch));
	if (uniqueFetch.length === 0) return;

	for (const y of uniqueFetch) {
		inFlightYears.add(y);
	}
	try {
		const res = await fetchCNHolidays(uniqueFetch);
		if (!res.enabled) {
			setCNHolidaysGlobalEnabledLocal(false);
			return;
		}
		for (const y of uniqueFetch) {
			const data = res.years[y];
			if (data) {
				mergeYearData(y, data);
				writeYearToStorage(y, data);
				loadedYears.add(y);
			}
		}
	} catch (error) {
		console.error('Failed to load CN holidays:', error);
	} finally {
		for (const y of uniqueFetch) {
			inFlightYears.delete(y);
		}
	}
}

export function invalidateHolidayCache(): void {
	const yearsToReload = Array.from(loadedYears);
	loadedYears.clear();
	if (browser) {
		const keysToRemove: string[] = [];
		for (let i = 0; i < localStorage.length; i++) {
			const k = localStorage.key(i);
			if (k && k.startsWith(CACHE_STORAGE_PREFIX)) {
				keysToRemove.push(k);
			}
		}
		for (const k of keysToRemove) {
			localStorage.removeItem(k);
		}
	}
	if (yearsToReload.length > 0 && get(cnHolidaysEnabled)) {
		void ensureHolidayYears(yearsToReload);
	}
}

export function getHolidayShortName(name: string): string {
	const trimmed = name.trim();
	if (!trimmed) return '';
	if (trimmed.length >= 3 && trimmed.endsWith('节')) {
		return trimmed.slice(0, -1);
	}
	return trimmed.length > 3 ? trimmed.slice(0, 3) : trimmed;
}

export function getCNDayStatus(
	dateStr: string,
	daysMap: Record<string, CNHolidayDay>
): CNDayStatus {
	const special = daysMap[dateStr];
	if (special) {
		if (special.isOffDay) {
			const prev = daysMap[getPreviousDay(dateStr)];
			const isFirstHolidayDay = !(prev && prev.isOffDay && prev.name === special.name);
			return {
				kind: 'holiday',
				badge: '休',
				name: special.name,
				shortName: getHolidayShortName(special.name),
				isOffDay: true,
				isFirstHolidayDay,
				custom: special.custom
			};
		}
		return {
			kind: 'workday_override',
			badge: '班',
			name: special.name,
			shortName: getHolidayShortName(special.name),
			isOffDay: false,
			isFirstHolidayDay: false,
			custom: special.custom
		};
	}

	const dayOfWeek = parseDate(dateStr).getDay();
	if (dayOfWeek === 0 || dayOfWeek === 6) {
		return {
			kind: 'weekend',
			isOffDay: true,
			isFirstHolidayDay: false
		};
	}

	return {
		kind: 'workday',
		isOffDay: false,
		isFirstHolidayDay: false
	};
}
