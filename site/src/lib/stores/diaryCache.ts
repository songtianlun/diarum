import { writable, get } from 'svelte/store';
import { browser } from '$app/environment';
import type { Diary } from '$lib/api/client';
import {
	loadPersistedData,
	persistEntry,
	persistEntries,
	removePersistedEntry,
	removePersistedEntries,
	getAllPersistedEntries,
	cleanupOldEntries,
	isInCacheRange,
	clearAllPersistedData,
	type PersistedEntry
} from './persistence';
import { checkOnlineStatus, initOnlineStatus } from './onlineStatus';
import { syncConfig, getConfig, initSyncConfig } from './syncConfig';

export interface CacheEntry {
	content: string;
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
let unsubscribeSyncConfig: (() => void) | null = null;
let storageEventHandler: ((e: StorageEvent) => void) | null = null;

// Retry state for offline sync
let retryCount = 0;
const MAX_RETRY_INTERVAL = 60000; // Max 60 seconds between retries
const BASE_RETRY_INTERVAL = 3000; // Start with 3 seconds

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

	for (const [date, entry] of Object.entries(persisted.entries)) {
		cache[date] = {
			content: entry.content,
			localUpdatedAt: entry.localUpdatedAt,
			serverUpdatedAt: entry.serverUpdatedAt,
			isDirty: entry.isDirty
		};
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
	initSyncConfig();
	cleanupOnlineStatus = initOnlineStatus();

	// Load persisted data
	reloadFromStorage();

	// Clean up old entries on startup
	const config = getConfig();
	cleanupOldEntries(config.cacheDays);

	// Subscribe to config changes - save unsubscribe for cleanup
	unsubscribeSyncConfig = syncConfig.subscribe(() => {
		// Config changed, timer will use new interval on next schedule
	});

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
	if (unsubscribeSyncConfig) {
		unsubscribeSyncConfig();
		unsubscribeSyncConfig = null;
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
export function updateLocalCache(date: string, content: string): void {
	const existing = getCachedContent(date);

	const entry: CacheEntry = {
		content,
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
		content,
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

	const serverContent = diary?.content || '';
	const serverUpdated = diary?.updated || null;

	// If local cache exists and is dirty, keep local changes
	if (existing && existing.isDirty) {
		return;
	}

	const entry: CacheEntry = {
		content: serverContent,
		localUpdatedAt: Date.now(),
		serverUpdatedAt: serverUpdated,
		isDirty: false
	};

	diaryCache.update(c => ({
		...c,
		[date]: entry
	}));

	// Persist to localStorage (within cache range)
	const config = getConfig();
	if (isInCacheRange(date, config.cacheDays)) {
		persistEntry({
			date,
			content: serverContent,
			localUpdatedAt: entry.localUpdatedAt,
			serverUpdatedAt: serverUpdated,
			isDirty: false
		});
	}

	updateCacheStats();
}

/**
 * Get content to display (prefers local dirty cache)
 */
export function getDisplayContent(date: string): string {
	const cache = get(diaryCache);
	const entry = cache[date];
	return entry?.content || '';
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
export function getDirtyEntries(): { date: string; content: string }[] {
	const cache = get(diaryCache);
	return Object.entries(cache)
		.filter(([_, entry]) => entry.isDirty)
		.map(([date, entry]) => ({ date, content: entry.content }));
}

/**
 * Get all unsynced entries with details
 */
export function getUnsyncedEntries(): PersistedEntry[] {
	const cache = get(diaryCache);
	return Object.entries(cache)
		.filter(([_, entry]) => entry.isDirty)
		.map(([date, entry]) => ({
			date,
			content: entry.content,
			localUpdatedAt: entry.localUpdatedAt,
			serverUpdatedAt: entry.serverUpdatedAt,
			isDirty: true
		}));
}

/**
 * Mark entry as synced
 */
export function markAsSynced(date: string, serverUpdatedAt: string): void {
	diaryCache.update(cache => {
		if (!cache[date]) return cache;
		return {
			...cache,
			[date]: {
				...cache[date],
				serverUpdatedAt,
				isDirty: false
			}
		};
	});

	// Update persistence
	const cache = get(diaryCache);
	const entry = cache[date];
	if (entry) {
		const config = getConfig();
		if (isInCacheRange(date, config.cacheDays)) {
			persistEntry({
				date,
				content: entry.content,
				localUpdatedAt: entry.localUpdatedAt,
				serverUpdatedAt,
				isDirty: false
			});
		} else {
			// Outside cache range and synced, remove from persistence
			removePersistedEntry(date);
		}
	}

	updateCacheStats();
}

/**
 * Schedule sync to server with exponential backoff for retries
 */
function scheduleSyncToServer(isRetry: boolean = false): void {
	if (syncTimer) {
		clearTimeout(syncTimer);
	}

	const config = getConfig();
	let interval = config.autoSaveInterval;

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

	// Import saveDiary dynamically to avoid circular dependency
	const { saveDiary } = await import('$lib/api/diaries');

	for (const entry of dirtyEntries) {
		try {
			const success = await saveDiary({
				date: entry.date,
				content: entry.content
			});

			if (success) {
				// If content is empty the entry was deleted on the server — clear local cache.
				// Otherwise mark it as synced.
				const contentStripped = entry.content.replace(/<[^>]*>/g, '').trim();
				if (!contentStripped) {
					clearCache(entry.date);
				} else {
					markAsSynced(entry.date, new Date().toISOString());
				}
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

	const { saveDiary } = await import('$lib/api/diaries');

	for (const entry of dirtyEntries) {
		try {
			const success = await saveDiary({
				date: entry.date,
				content: entry.content
			});

			if (success) {
				// If content is empty the entry was deleted on the server — clear local cache.
				// Otherwise mark it as synced.
				const contentStripped = entry.content.replace(/<[^>]*>/g, '').trim();
				if (!contentStripped) {
					clearCache(entry.date);
				} else {
					markAsSynced(entry.date, new Date().toISOString());
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

/**
 * Clear all cache
 */
export function clearAllCache(): void {
	diaryCache.set({});
	clearAllPersistedData();
	updateCacheStats();
}

/**
 * Clear only synced entries from cache
 */
export function clearSyncedCache(): void {
	const cache = get(diaryCache);
	const newCache: DiaryCache = {};
	const datesToRemove: string[] = [];

	for (const [date, entry] of Object.entries(cache)) {
		if (entry.isDirty) {
			newCache[date] = entry;
		} else {
			datesToRemove.push(date);
		}
	}

	removePersistedEntries(datesToRemove);
	diaryCache.set(newCache);
	updateCacheStats();
}

/**
 * Run cache cleanup based on config
 */
export function runCacheCleanup(): number {
	const config = getConfig();
	const removed = cleanupOldEntries(config.cacheDays);

	// Also update in-memory cache
	const cache = get(diaryCache);
	const newCache: DiaryCache = {};

	for (const [date, entry] of Object.entries(cache)) {
		if (entry.isDirty || isInCacheRange(date, config.cacheDays)) {
			newCache[date] = entry;
		}
	}

	diaryCache.set(newCache);
	updateCacheStats();

	return removed;
}

// Pre-cache state
export const preCacheState = writable<{
	isRunning: boolean;
	progress: number;
	total: number;
	message: string;
}>({
	isRunning: false,
	progress: 0,
	total: 0,
	message: ''
});

/**
 * Pre-cache diaries for the configured cache duration
 */
export async function preCacheDiaries(): Promise<{ success: boolean; cached: number }> {
	const config = getConfig();
	const cacheDays = config.cacheDays;

	// Calculate date range
	const today = new Date();
	const startDate = new Date(today);
	startDate.setDate(startDate.getDate() - cacheDays + 1);

	const formatDate = (d: Date) => d.toISOString().split('T')[0];
	const start = formatDate(startDate);
	const end = formatDate(today);

	preCacheState.set({
		isRunning: true,
		progress: 0,
		total: 0,
		message: 'Fetching diary list...'
	});

	try {
		// Check online status
		const online = await checkOnlineStatus();
		if (!online) {
			preCacheState.set({
				isRunning: false,
				progress: 0,
				total: 0,
				message: 'Offline'
			});
			return { success: false, cached: 0 };
		}

		// Get dates with diaries in range
		const { getDatesWithDiaries, getDiaryByDate } = await import('$lib/api/diaries');
		const datesWithDiaries = await getDatesWithDiaries(start, end);

		if (datesWithDiaries.length === 0) {
			preCacheState.set({
				isRunning: false,
				progress: 0,
				total: 0,
				message: 'No diaries to cache'
			});
			return { success: true, cached: 0 };
		}

		// Filter out dates already in cache (not dirty)
		const cache = get(diaryCache);
		const datesToFetch = datesWithDiaries.filter(date => {
			const entry = cache[date];
			return !entry || entry.isDirty === false; // Re-fetch synced entries to ensure fresh
		});

		preCacheState.set({
			isRunning: true,
			progress: 0,
			total: datesToFetch.length,
			message: `Caching 0/${datesToFetch.length}...`
		});

		let cached = 0;
		for (const date of datesToFetch) {
			try {
				const diary = await getDiaryByDate(date);
				if (diary) {
					updateFromServer(date, diary);
					cached++;
				}
				preCacheState.set({
					isRunning: true,
					progress: cached,
					total: datesToFetch.length,
					message: `Caching ${cached}/${datesToFetch.length}...`
				});
			} catch (error) {
				console.error(`Failed to pre-cache diary for ${date}:`, error);
			}
		}

		preCacheState.set({
			isRunning: false,
			progress: cached,
			total: datesToFetch.length,
			message: `Cached ${cached} entries`
		});

		// Clear message after 3 seconds
		setTimeout(() => {
			preCacheState.update(s => {
				if (!s.isRunning) {
					return { ...s, message: '' };
				}
				return s;
			});
		}, 3000);

		return { success: true, cached };
	} catch (error) {
		console.error('Pre-cache failed:', error);
		preCacheState.set({
			isRunning: false,
			progress: 0,
			total: 0,
			message: 'Pre-cache failed'
		});
		return { success: false, cached: 0 };
	}
}
