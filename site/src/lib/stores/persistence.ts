import { browser } from '$app/environment';

const STORAGE_KEY = 'diarum_diary_cache';

export interface PersistedEntry {
	date: string;
	content: string;
	mood?: string;
	weather?: string;
	localUpdatedAt: number;
	serverUpdatedAt: string | null;
	isDirty: boolean;
}

export interface PersistedData {
	entries: { [date: string]: PersistedEntry };
	version: number;
}

const CURRENT_VERSION = 1;

/**
 * Load persisted data from localStorage
 */
export function loadPersistedData(): PersistedData {
	if (!browser) {
		return { entries: {}, version: CURRENT_VERSION };
	}

	try {
		const stored = localStorage.getItem(STORAGE_KEY);
		if (!stored) {
			return { entries: {}, version: CURRENT_VERSION };
		}

		const data = JSON.parse(stored) as PersistedData;

		// Handle version migrations if needed
		if (!data.version || data.version < CURRENT_VERSION) {
			return migrateData(data);
		}

		return data;
	} catch (e) {
		console.error('Failed to load persisted diary data:', e);
		return { entries: {}, version: CURRENT_VERSION };
	}
}

/**
 * Save data to localStorage
 */
export function savePersistedData(data: PersistedData): void {
	if (!browser) return;

	try {
		localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
	} catch (e) {
		console.error('Failed to persist diary data:', e);
	}
}

/**
 * Save multiple entries to persistence (batch operation)
 */
export function persistEntries(entries: PersistedEntry[]): void {
	if (entries.length === 0) return;
	const data = loadPersistedData();
	for (const entry of entries) {
		data.entries[entry.date] = entry;
	}
	savePersistedData(data);
}

/**
 * Remove an entry from persistence
 */
export function removePersistedEntry(date: string): void {
	const data = loadPersistedData();
	delete data.entries[date];
	savePersistedData(data);
}

/**
 * Remove multiple entries from persistence (batch operation)
 */
export function removePersistedEntries(dates: string[]): void {
	if (dates.length === 0) return;
	const data = loadPersistedData();
	for (const date of dates) {
		delete data.entries[date];
	}
	savePersistedData(data);
}

/**
 * Clear all persisted data
 */
export function clearAllPersistedData(): void {
	if (!browser) return;
	localStorage.removeItem(STORAGE_KEY);
}

/**
 * Migrate data from older versions
 */
function migrateData(data: PersistedData): PersistedData {
	const migratedEntries: PersistedData['entries'] = {};
	for (const [date, entry] of Object.entries(data.entries || {})) {
		migratedEntries[date] = {
			...entry,
			mood: entry.mood ?? '',
			weather: entry.weather ?? ''
		};
	}

	return {
		entries: migratedEntries,
		version: CURRENT_VERSION
	};
}

