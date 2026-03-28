import { pb } from './client';
import {
	DEFAULT_MOOD_OPTIONS,
	DEFAULT_WEATHER_OPTIONS,
	sanitizeMoodOptions,
	sanitizeWeatherOptions
} from '$lib/utils/diaryEmoji';

export interface ApiTokenStatus {
	exists: boolean;
	enabled: boolean;
	token: string;
}

export interface DiaryEmojiSettings {
	mood_options: string[];
	weather_options: string[];
}

async function getSettingValue(key: string): Promise<unknown> {
	const response = await fetch(`/api/v1/settings/${encodeURIComponent(key)}`, {
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`
		}
	});

	if (!response.ok) {
		throw new Error(`Failed to get setting: ${key}`);
	}

	const data = await response.json();
	return data?.value;
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

export async function getDiaryEmojiSettings(): Promise<DiaryEmojiSettings> {
	try {
		const [moodOptions, weatherOptions] = await Promise.all([
			getSettingValue('diary.mood_options'),
			getSettingValue('diary.weather_options')
		]);

		return {
			mood_options: sanitizeMoodOptions(moodOptions),
			weather_options: sanitizeWeatherOptions(weatherOptions)
		};
	} catch (error) {
		console.error('Error fetching diary emoji settings:', error);
		return {
			mood_options: [...DEFAULT_MOOD_OPTIONS],
			weather_options: [...DEFAULT_WEATHER_OPTIONS]
		};
	}
}

export async function saveDiaryEmojiSettings(settings: DiaryEmojiSettings): Promise<{ success: boolean }> {
	const response = await fetch('/api/v1/settings/batch', {
		method: 'PUT',
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({
			settings: {
				'diary.mood_options': sanitizeMoodOptions(settings.mood_options),
				'diary.weather_options': sanitizeWeatherOptions(settings.weather_options)
			}
		})
	});

	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new Error(data.message || 'Failed to save diary emoji settings');
	}

	return await response.json();
}

