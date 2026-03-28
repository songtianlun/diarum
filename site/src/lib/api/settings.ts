import { pb } from './client';

export interface ApiTokenStatus {
	exists: boolean;
	enabled: boolean;
	token: string;
}

export interface SyncSettings {
	autoSaveInterval: number;
}

/**
 * Get API token status and value
 */
export async function getApiToken(): Promise<ApiTokenStatus> {
	try {
		const response = await fetch('/api/settings/api-token', {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			throw new Error('Failed to get API token');
		}

		return await response.json();
	} catch (error) {
		console.error('Error fetching API token:', error);
		return { exists: false, enabled: false, token: '' };
	}
}

/**
 * Toggle API token enabled/disabled
 */
export async function toggleApiToken(): Promise<ApiTokenStatus> {
	try {
		const response = await fetch('/api/settings/api-token/toggle', {
			method: 'POST',
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			throw new Error('Failed to toggle API token');
		}

		const data = await response.json();
		return { exists: true, enabled: data.enabled, token: data.token };
	} catch (error) {
		console.error('Error toggling API token:', error);
		throw error;
	}
}

/**
 * Reset API token (generate new one)
 */
export async function resetApiToken(): Promise<ApiTokenStatus> {
	try {
		const response = await fetch('/api/settings/api-token/reset', {
			method: 'POST',
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			throw new Error('Failed to reset API token');
		}

		const data = await response.json();
		return { exists: true, enabled: data.enabled, token: data.token };
	} catch (error) {
		console.error('Error resetting API token:', error);
		throw error;
	}
}

/**
 * Get sync settings from backend
 */
export async function getSyncSettings(): Promise<SyncSettings> {
	try {
		const response = await fetch('/api/v1/settings', {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			throw new Error('Failed to get settings');
		}

		const data = await response.json();
		const settings = data.settings || {};

		return {
			autoSaveInterval: settings['sync.autoSaveInterval'] ?? 3000
		};
	} catch (error) {
		console.error('Error fetching sync settings:', error);
		return { autoSaveInterval: 3000 };
	}
}

/**
 * Save sync settings to backend
 */
export async function saveSyncSettings(settings: SyncSettings): Promise<boolean> {
	try {
		const response = await fetch('/api/v1/settings/batch', {
			method: 'PUT',
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`,
				'Content-Type': 'application/json'
			},
			body: JSON.stringify({
				settings: {
					'sync.autoSaveInterval': settings.autoSaveInterval
				}
			})
		});

		if (!response.ok) {
			throw new Error('Failed to save settings');
		}

		return true;
	} catch (error) {
		console.error('Error saving sync settings:', error);
		return false;
	}
}
