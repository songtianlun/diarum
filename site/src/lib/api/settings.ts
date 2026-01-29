import { pb } from './client';

export interface ApiTokenStatus {
	exists: boolean;
	enabled: boolean;
	token: string;
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
