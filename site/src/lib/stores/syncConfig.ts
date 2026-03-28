import { writable, get } from 'svelte/store';
import { browser } from '$app/environment';

const STORAGE_KEY = 'diarum_sync_config';

export interface SyncConfig {
	autoSaveInterval: number;  // milliseconds, default 3000
}

const DEFAULT_CONFIG: SyncConfig = {
	autoSaveInterval: 3000
};

function loadConfig(): SyncConfig {
	if (!browser) return DEFAULT_CONFIG;

	try {
		const stored = localStorage.getItem(STORAGE_KEY);
		if (!stored) return DEFAULT_CONFIG;

		const config = JSON.parse(stored) as Partial<SyncConfig>;
		return {
			...DEFAULT_CONFIG,
			...config
		};
	} catch {
		return DEFAULT_CONFIG;
	}
}

export const syncConfig = writable<SyncConfig>(loadConfig());

let configInitialized = false;
let unsubscribeConfig: (() => void) | null = null;

/**
 * Initialize sync config (call on app mount)
 */
export function initSyncConfig(): void {
	if (!browser || configInitialized) return;
	configInitialized = true;

	// Load from localStorage
	const config = loadConfig();
	syncConfig.set(config);

	// Subscribe to changes and persist
	unsubscribeConfig = syncConfig.subscribe(config => {
		try {
			localStorage.setItem(STORAGE_KEY, JSON.stringify(config));
		} catch (e) {
			console.error('Failed to save sync config:', e);
		}
	});
}

/**
 * Cleanup sync config subscription
 */
export function cleanupSyncConfig(): void {
	if (unsubscribeConfig) {
		unsubscribeConfig();
		unsubscribeConfig = null;
	}
	configInitialized = false;
}

/**
 * Update auto-save interval
 */
export function setAutoSaveInterval(interval: number): void {
	if (interval < 1000) interval = 1000;  // Minimum 1 second
	if (interval > 60000) interval = 60000;  // Maximum 60 seconds

	syncConfig.update(c => ({ ...c, autoSaveInterval: interval }));
}

/**
 * Get current config (synchronous)
 */
export function getConfig(): SyncConfig {
	return get(syncConfig);
}

/**
 * Reset to default config
 */
export function resetConfig(): void {
	syncConfig.set(DEFAULT_CONFIG);
}
