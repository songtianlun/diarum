import { writable, get } from 'svelte/store';
import { browser } from '$app/environment';
import type { Diary } from '$lib/api/client';
import {
	loadPersistedData,
	persistEntries,
	removePersistedEntry,
	removePersistedEntries,
	type PersistedEntry
} from './persistence';
import { checkOnlineStatus, initOnlineStatus } from './onlineStatus';

export interface CacheEntry {
	content: string;
	mood: string;
	weather: string;
	localUpdatedAt: number;
	serverUpdatedAt: string | null;
	isDirty: boolean;
}

interface DiaryCache {
	[date: string]: CacheEntry;
}

interface SyncState {
	isSyncing: boolean;
	currentDate: string | null;
	status: 'idle' | 'saving' | 'saved' | 'error';
	message: string;
}

export interface CacheStats {
	totalCached: number;
	pendingSync: number;
	entries: { date: string; isDirty: boolean; localUpdatedAt: number }[];
}

// Cache store
export const diaryCache = writable<DiaryCache>({});

// Global sync state
export const syncState = writable<SyncState>({
	isSyncing: false,
	currentDate: null,
	status: 'idle',
	message: ''
});

// Cache statistics store
export const cacheStats = writable<CacheStats>({
	totalCached: 0,
	pendingSync: 0,
	entries: []
});

// Sync timer
let syncTimer: ReturnType<typeof setTimeout> | null = null;
let persistTimer: ReturnType<typeof setTimeout> | null = null;
let initialized = false;
let cleanupOnlineStatus: (() => void) | null = null;
let storageEventHandler: ((e: StorageEvent) => void) | null = null;

// Retry state for offline sync
let retryCount = 0;
const MAX_RETRY_INTERVAL = 60000; // Max 60 seconds between retries
const BASE_RETRY_INTERVAL = 3000; // Start with 3 seconds
const AUTO_SAVE_DEBOUNCE_INTERVAL = 1000; // Save 1s after user stops typing

// Pending persistence queue
let pendingPersist: Map<string, PersistedEntry> = new Map();

// Storage key for cross-tab detection
const STORAGE_KEY = 'diarum_diary_cache';

/**
 * Reload cache from localStorage (for cross-tab sync)
 */
function reloadFromStorage(): void {
	const persisted = loadPersistedData();
	const cache: DiaryCache = {};
	const legacySyncedDates: string[] = [];

	for (const [date, entry] of Object.entries(persisted.entries)) {
		if (!entry.isDirty) {
			legacySyncedDates.push(date);
			continue;
		}

		cache[date] = {
			content: entry.content,
			mood: entry.mood || '',
			weather: entry.weather || '',
			localUpdatedAt: entry.localUpdatedAt,
			serverUpdatedAt: entry.serverUpdatedAt,
			isDirty: entry.isDirty
		};
	}

	if (legacySyncedDates.length > 0) {
		removePersistedEntries(legacySyncedDates);
	}

	diaryCache.set(cache);
	updateCacheStats();
}

/**
 * Initialize the diary cache system
 */
export function initDiaryCache(): void {
	if (!browser || initialized) return;
	initialized = true;

	// Initialize dependencies
	cleanupOnlineStatus = initOnlineStatus();

	// Load persisted data
	reloadFromStorage();

	// Listen for storage changes from other tabs
	storageEventHandler = (e: StorageEvent) => {
		if (e.key === STORAGE_KEY) {
			// Flush pending writes before reloading to avoid data loss
			flushPendingPersist();
			reloadFromStorage();
		}
	};
	window.addEventListener('storage', storageEventHandler);
}

/**
 * Cleanup function for when app unmounts
 */
export function cleanupDiaryCache(): void {
	if (syncTimer) {
		clearTimeout(syncTimer);
		syncTimer = null;
	}
	if (persistTimer) {
		clearTimeout(persistTimer);
		persistTimer = null;
	}
	if (cleanupOnlineStatus) {
		cleanupOnlineStatus();
		cleanupOnlineStatus = null;
	}
	if (storageEventHandler) {
		window.removeEventListener('storage', storageEventHandler);
		storageEventHandler = null;
	}
	// Flush any pending persistence
	flushPendingPersist();
	retryCount = 0;
	initialized = false;
}

/**
 * Flush pending persistence immediately
 */
function flushPendingPersist(): void {
	if (pendingPersist.size > 0) {
		const entries = Array.from(pendingPersist.values());
		persistEntries(entries);
		pendingPersist.clear();
	}
}

/**
 * Debounced persistence - batches writes to localStorage
 */
function debouncedPersist(entry: PersistedEntry): void {
	pendingPersist.set(entry.date, entry);

	if (persistTimer) {
		clearTimeout(persistTimer);
	}

	persistTimer = setTimeout(() => {
		flushPendingPersist();
		persistTimer = null;
	}, 300); // 300ms debounce for persistence
}

/**
 * Update cache statistics
 */
function updateCacheStats(): void {
	const cache = get(diaryCache);
	const entries = Object.entries(cache).map(([date, entry]) => ({
		date,
		isDirty: entry.isDirty,
		localUpdatedAt: entry.localUpdatedAt
	}));

	// Sort by date descending
	entries.sort((a, b) => b.date.localeCompare(a.date));

	cacheStats.set({
		totalCached: entries.length,
		pendingSync: entries.filter(e => e.isDirty).length,
		entries
	});
}

/**
 * Get cached content for a date
 */
export function getCachedContent(date: string): CacheEntry | null {
	const cache = get(diaryCache);
	return cache[date] || null;
}

/**
 * Update local cache with edited content
 */
export function updateLocalCache(
	date: string,
	updates: { content: string; mood?: string; weather?: string }
): void {
	const existing = getCachedContent(date);

	const entry: CacheEntry = {
		content: updates.content,
		mood: updates.mood ?? existing?.mood ?? '',
		weather: updates.weather ?? existing?.weather ?? '',
		localUpdatedAt: Date.now(),
		serverUpdatedAt: existing?.serverUpdatedAt || null,
		isDirty: true
	};

	diaryCache.update(cache => ({
		...cache,
		[date]: entry
	}));

	// Debounced persist to localStorage
	debouncedPersist({
		date,
		content: entry.content,
		mood: entry.mood,
		weather: entry.weather,
		localUpdatedAt: entry.localUpdatedAt,
		serverUpdatedAt: entry.serverUpdatedAt,
		isDirty: true
	});

	updateCacheStats();

	// Schedule sync
	scheduleSyncToServer();
}

/**
 * Update cache from server data
 */
export function updateFromServer(date: string, diary: Diary | null): void {
	const cache = get(diaryCache);
	const existing = cache[date];

	// If local cache exists and is dirty, keep local changes
	if (existing && existing.isDirty) {
		return;
	}

	// Browser cache is disabled: only keep unsynced local drafts.
	if (existing && !existing.isDirty) {
		clearCache(date);
	}
	if (!diary) {
		removePersistedEntry(date);
	}

	updateCacheStats();
}

/**
 * Check if date has dirty cache
 */
export function hasDirtyCache(date: string): boolean {
	const cache = get(diaryCache);
	return cache[date]?.isDirty || false;
}

/**
 * Get all dirty entries
 */
export function getDirtyEntries(): { date: string; content: string; mood: string; weather: string }[] {
	const cache = get(diaryCache);
	return Object.entries(cache)
		.filter(([_, entry]) => entry.isDirty)
		.map(([date, entry]) => ({
			date,
			content: entry.content,
			mood: entry.mood || '',
			weather: entry.weather || ''
		}));
}

/**
 * Mark entry as synced
 */
export function markAsSynced(date: string, serverUpdatedAt: string): void {
	void serverUpdatedAt;
	// Browser cache is disabled: once synced, remove local draft snapshot.
	clearCache(date);

	updateCacheStats();
}

/**
 * Schedule sync to server with exponential backoff for retries
 */
function scheduleSyncToServer(isRetry: boolean = false): void {
	if (syncTimer) {
		clearTimeout(syncTimer);
	}

	let interval = AUTO_SAVE_DEBOUNCE_INTERVAL;

	// Use exponential backoff for retries
	if (isRetry) {
		interval = Math.min(BASE_RETRY_INTERVAL * Math.pow(2, retryCount), MAX_RETRY_INTERVAL);
		retryCount++;
	} else {
		retryCount = 0; // Reset retry count for new edits
	}

	syncTimer = setTimeout(() => {
		syncDirtyEntries();
	}, interval);
}

/**
 * Sync all dirty entries to server
 */
async function syncDirtyEntries(): Promise<void> {
	const dirtyEntries = getDirtyEntries();

	if (dirtyEntries.length === 0) {
		syncState.set({
			isSyncing: false,
			currentDate: null,
			status: 'idle',
			message: ''
		});
		return;
	}

	// Check online status first
	const online = await checkOnlineStatus();
	if (!online) {
		syncState.set({
			isSyncing: false,
			currentDate: null,
			status: 'error',
			message: 'Offline'
		});
		// Retry later with exponential backoff
		scheduleSyncToServer(true);
		return;
	}

	// Reset retry count on successful online check
	retryCount = 0;

	syncState.set({
		isSyncing: true,
		currentDate: dirtyEntries[0].date,
		status: 'saving',
		message: 'Saving...'
	});

	// Import API dynamically to avoid circular dependency
	const { saveDiary, getDiaryByDateResult } = await import('$lib/api/diaries');

	for (const entry of dirtyEntries) {
		try {
			const success = await saveDiary({
				date: entry.date,
				content: entry.content,
				mood: entry.mood,
				weather: entry.weather
			});

			if (success) {
				// Re-check server state to avoid local/server mismatch.
				const serverState = await getDiaryByDateResult(entry.date);
				if (serverState.status === 'error') {
					syncState.set({
						isSyncing: false,
						currentDate: entry.date,
						status: 'error',
						message: 'Failed to verify save'
					});
					// Retry later; keep dirty cache until verification succeeds.
					scheduleSyncToServer(true);
					return;
				}
				if (serverState.status === 'not_found') {
					clearCache(entry.date);
				} else {
					markAsSynced(entry.date, serverState.diary.updated || new Date().toISOString());
				}
			} else {
				syncState.set({
					isSyncing: false,
					currentDate: entry.date,
					status: 'error',
					message: 'Failed to save'
				});
				// Retry later with exponential backoff
				scheduleSyncToServer(true);
				return;
			}
		} catch (error) {
			console.error(`Failed to sync diary for ${entry.date}:`, error);
			syncState.set({
				isSyncing: false,
				currentDate: entry.date,
				status: 'error',
				message: 'Failed to save'
			});
			// Retry later with exponential backoff
			scheduleSyncToServer(true);
			return;
		}
	}

	syncState.set({
		isSyncing: false,
		currentDate: null,
		status: 'saved',
		message: 'Saved'
	});

	// Clear saved message after 2 seconds
	setTimeout(() => {
		syncState.update(s => {
			if (s.status === 'saved') {
				return { ...s, status: 'idle', message: '' };
			}
			return s;
		});
	}, 2000);
}

/**
 * Force sync immediately
 */
export async function forceSyncNow(): Promise<boolean> {
	if (syncTimer) {
		clearTimeout(syncTimer);
		syncTimer = null;
	}

	const dirtyEntries = getDirtyEntries();
	if (dirtyEntries.length === 0) return true;

	// Check online status
	const online = await checkOnlineStatus();
	if (!online) {
		syncState.set({
			isSyncing: false,
			currentDate: null,
			status: 'error',
			message: 'Offline'
		});
		return false;
	}

	syncState.set({
		isSyncing: true,
		currentDate: dirtyEntries[0].date,
		status: 'saving',
		message: 'Saving...'
	});

	const { saveDiary, getDiaryByDateResult } = await import('$lib/api/diaries');

	for (const entry of dirtyEntries) {
		try {
			const success = await saveDiary({
				date: entry.date,
				content: entry.content,
				mood: entry.mood,
				weather: entry.weather
			});

			if (success) {
				// Re-check server state to avoid local/server mismatch.
				const serverState = await getDiaryByDateResult(entry.date);
				if (serverState.status === 'error') {
					syncState.set({
						isSyncing: false,
						currentDate: entry.date,
						status: 'error',
						message: 'Failed to verify save'
					});
					return false;
				}
				if (serverState.status === 'not_found') {
					clearCache(entry.date);
				} else {
					markAsSynced(entry.date, serverState.diary.updated || new Date().toISOString());
				}
			} else {
				syncState.set({
					isSyncing: false,
					currentDate: entry.date,
					status: 'error',
					message: 'Failed to save'
				});
				return false;
			}
		} catch (error) {
			console.error(`Failed to sync diary for ${entry.date}:`, error);
			syncState.set({
				isSyncing: false,
				currentDate: entry.date,
				status: 'error',
				message: 'Failed to save'
			});
			return false;
		}
	}

	syncState.set({
		isSyncing: false,
		currentDate: null,
		status: 'saved',
		message: 'Saved'
	});

	setTimeout(() => {
		syncState.update(s => {
			if (s.status === 'saved') {
				return { ...s, status: 'idle', message: '' };
			}
			return s;
		});
	}, 2000);

	return true;
}

/**
 * Clear cache for a specific date
 */
export function clearCache(date: string): void {
	diaryCache.update(cache => {
		const { [date]: _, ...rest } = cache;
		return rest;
	});
	removePersistedEntry(date);
	updateCacheStats();
}

