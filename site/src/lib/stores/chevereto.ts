import { writable } from 'svelte/store';
import { browser } from '$app/environment';
import { getCheveretoSettings, type CheveretoSettings } from '$lib/api/chevereto';

const defaultSettings: CheveretoSettings = {
	enabled: false,
	domain: '',
	api_key: '',
	album_id: ''
};

export const cheveretoSettings = writable<CheveretoSettings>({ ...defaultSettings });

let loaded = false;

export async function loadCheveretoSettings(): Promise<CheveretoSettings> {
	if (!browser) return defaultSettings;

	try {
		const settings = await getCheveretoSettings();
		cheveretoSettings.set(settings);
		loaded = true;
		return settings;
	} catch (error) {
		console.error('Failed to load Chevereto settings:', error);
		return defaultSettings;
	}
}

export function isCheveretoLoaded(): boolean {
	return loaded;
}

export function resetCheveretoStore(): void {
	cheveretoSettings.set({ ...defaultSettings });
	loaded = false;
}
