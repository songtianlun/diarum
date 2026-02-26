import { pb } from '$lib/api/client';
import type { Media, UploadProgress } from '$lib/api/client';
import { get } from 'svelte/store';
import { cheveretoSettings, loadCheveretoSettings, isCheveretoLoaded } from '$lib/stores/chevereto';
import { uploadToChevereto } from '$lib/api/chevereto';

/**
 * Get or create diary ID for a given date
 */
export async function getOrCreateDiaryId(date: string): Promise<string | undefined> {
	try {
		// Try to find existing diary
		const response = await fetch(`/api/diaries/by-date/${date}`, {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (response.ok) {
			const data = await response.json();
			if (data.exists && data.id) {
				return data.id;
			}
		}

		// Create new diary if not exists
		const userId = pb.authStore.model?.id;
		if (!userId) return undefined;

		const newDiary = await pb.collection('diaries').create({
			date: date + ' 00:00:00.000Z',
			content: '',
			owner: userId
		});

		return newDiary.id;
	} catch (error) {
		console.error('Failed to get/create diary:', error);
		return undefined;
	}
}

export interface UploadOptions {
	diaryId?: string;
	diaryDate?: string; // Date string (YYYY-MM-DD) to auto-link diary
	alt?: string;
	onProgress?: (progress: UploadProgress) => void;
}

export interface CheveretoUploadResult {
	cheveretoUrl: string;
}

export function isCheveretoResult(result: Media | CheveretoUploadResult): result is CheveretoUploadResult {
	return 'cheveretoUrl' in result;
}

/**
 * Upload an image file to PocketBase or Chevereto
 * @param file - The image file to upload
 * @param options - Upload options
 * @returns The created media record or Chevereto URL
 */
export async function uploadImage(file: File, options: UploadOptions = {}): Promise<Media | CheveretoUploadResult> {
	const { diaryId, diaryDate, alt, onProgress } = options;

	// Validate file type
	const allowedTypes = ['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/svg+xml'];
	if (!allowedTypes.includes(file.type)) {
		throw new Error(`Invalid file type: ${file.type}. Allowed types: ${allowedTypes.join(', ')}`);
	}

	// Validate file size (50MB max - must match PocketBase media collection setting)
	const maxSize = 50 * 1024 * 1024;
	if (file.size > maxSize) {
		throw new Error(`File size exceeds 50MB limit. File size: ${(file.size / 1024 / 1024).toFixed(2)}MB`);
	}

	// Check if Chevereto is enabled
	if (!isCheveretoLoaded()) {
		await loadCheveretoSettings();
	}
	const settings = get(cheveretoSettings);

	if (settings.enabled) {
		try {
			const result = await uploadToChevereto(file);
			return { cheveretoUrl: result.url };
		} catch (error) {
			console.error('Chevereto upload failed:', error);
			throw new Error('Failed to upload image to Chevereto. Please try again.');
		}
	}

	// Fallback to PocketBase upload
	let resolvedDiaryId = diaryId;
	if (!resolvedDiaryId && diaryDate) {
		resolvedDiaryId = await getOrCreateDiaryId(diaryDate);
	}

	const formData = new FormData();
	formData.append('file', file);
	formData.append('name', file.name);
	formData.append('owner', pb.authStore.model?.id || '');

	if (alt) {
		formData.append('alt', alt);
	}

	if (resolvedDiaryId) {
		formData.append('diary', resolvedDiaryId);
	}

	try {
		const record = await pb.collection('media').create<Media>(formData, {
			requestKey: `upload_${Date.now()}`,
		});

		return record;
	} catch (error: any) {
		console.error('Upload failed:', error);
		console.error('Error data:', JSON.stringify(error?.data, null, 2));
		throw new Error('Failed to upload image. Please try again.');
	}
}

/**
 * Get the full URL for a media file
 * @param media - The media record
 * @param thumb - Optional thumbnail size (e.g., "100x100", "300x300", "800x600")
 * @returns The full URL to the image
 */
export function getMediaUrl(media: Media, thumb?: string): string {
	if (!media.id || !media.file) {
		throw new Error('Invalid media record');
	}

	return pb.files.getUrl(media as any, media.file, { thumb });
}

/**
 * Delete a media record
 * @param mediaId - The ID of the media record to delete
 */
export async function deleteMedia(mediaId: string): Promise<void> {
	try {
		await pb.collection('media').delete(mediaId);
	} catch (error) {
		console.error('Delete failed:', error);
		throw new Error('Failed to delete media. Please try again.');
	}
}

/**
 * Upload image from URL
 * @param url - The image URL
 * @param options - Upload options
 */
export async function uploadImageFromUrl(url: string, options: UploadOptions = {}): Promise<Media | CheveretoUploadResult> {
	try {
		const response = await fetch(url);
		if (!response.ok) {
			throw new Error('Failed to fetch image from URL');
		}

		const blob = await response.blob();
		const filename = url.split('/').pop() || 'image.jpg';
		const file = new File([blob], filename, { type: blob.type });

		return await uploadImage(file, options);
	} catch (error) {
		console.error('Upload from URL failed:', error);
		throw new Error('Failed to upload image from URL. Please try again.');
	}
}
