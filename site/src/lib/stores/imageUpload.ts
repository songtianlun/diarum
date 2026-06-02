import { browser } from '$app/environment';
import { writable } from 'svelte/store';
import {
    defaultImageUploadSettings,
    getImageUploadSettings,
    type ImageUploadSettings
} from '$lib/api/imageUpload';

export const imageUploadSettings = writable<ImageUploadSettings>(structuredClone(defaultImageUploadSettings));

let loaded = false;

export async function loadImageUploadSettings(): Promise<ImageUploadSettings> {
    if (!browser) {
        return structuredClone(defaultImageUploadSettings);
    }

    try {
        const settings = await getImageUploadSettings();
        imageUploadSettings.set(settings);
        loaded = true;
        return settings;
    } catch (error) {
        console.error('Failed to load image upload settings:', error);
        return structuredClone(defaultImageUploadSettings);
    }
}

export function isImageUploadLoaded(): boolean {
    return loaded;
}

export function resetImageUploadStore(): void {
    imageUploadSettings.set(structuredClone(defaultImageUploadSettings));
    loaded = false;
}