import { pb } from '$lib/api/client';
import type { Media, UploadProgress } from '$lib/api/client';

export interface UploadOptions {
	diaryId?: string;
	alt?: string;
	onProgress?: (progress: UploadProgress) => void;
}

/**
 * Upload an image file to PocketBase
 * @param file - The image file to upload
 * @param options - Upload options
 * @returns The created media record with file URL
 */
export async function uploadImage(file: File, options: UploadOptions = {}): Promise<Media> {
	const { diaryId, alt, onProgress } = options;

	// Validate file type
	const allowedTypes = ['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/svg+xml'];
	if (!allowedTypes.includes(file.type)) {
		throw new Error(`Invalid file type: ${file.type}. Allowed types: ${allowedTypes.join(', ')}`);
	}

	// Validate file size (5MB max)
	const maxSize = 5 * 1024 * 1024;
	if (file.size > maxSize) {
		throw new Error(`File size exceeds 5MB limit. File size: ${(file.size / 1024 / 1024).toFixed(2)}MB`);
	}

	// Create FormData
	const formData = new FormData();
	formData.append('file', file);
	formData.append('name', file.name);
	formData.append('owner', pb.authStore.model?.id || '');

	if (alt) {
		formData.append('alt', alt);
	}

	if (diaryId) {
		formData.append('diary', diaryId);
	}

	try {
		// Upload with progress tracking
		const record = await pb.collection('media').create<Media>(formData, {
			requestKey: `upload_${Date.now()}`,
		});

		return record;
	} catch (error) {
		console.error('Upload failed:', error);
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
export async function uploadImageFromUrl(url: string, options: UploadOptions = {}): Promise<Media> {
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
