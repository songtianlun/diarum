import { pb } from '$lib/api/client';
import type { Media, UploadProgress } from '$lib/api/client';
import { get } from 'svelte/store';
import { imageUploadSettings, loadImageUploadSettings, isImageUploadLoaded } from '$lib/stores/imageUpload';
import { isHeicFile, LIVE_SOURCE_MAX_BYTES } from '$lib/utils/livePhoto';

export const IMAGE_UPLOAD_LIMITS = {
	maxSize: 50 * 1024 * 1024, // must match the backend media limit
	allowedTypes: ['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/svg+xml']
};

export interface UploadOptions {
	alt?: string;
	/** The clip of a live photo, stored with the image (built-in library only). */
	live?: File;
	onProgress?: (progress: UploadProgress) => void;
	signal?: AbortSignal;
}

export interface CheveretoUploadResult {
	cheveretoUrl: string;
	/** A live photo's clip was given but Chevereto only takes the still. */
	liveDropped?: boolean;
}

export class UploadAbortedError extends Error {
	constructor() {
		super('Upload cancelled');
		this.name = 'UploadAbortedError';
	}
}

export function isCheveretoResult(result: Media | CheveretoUploadResult): result is CheveretoUploadResult {
	return 'cheveretoUrl' in result;
}

/**
 * Returns a user-facing reason the file cannot be uploaded, or null when it is fine.
 */
export function validateImageFile(file: File): string | null {
	if (!IMAGE_UPLOAD_LIMITS.allowedTypes.includes(file.type)) {
		return 'Unsupported format. Use JPG, PNG, GIF, WebP or SVG';
	}
	if (file.size > IMAGE_UPLOAD_LIMITS.maxSize) {
		return `Larger than ${IMAGE_UPLOAD_LIMITS.maxSize / 1024 / 1024}MB`;
	}
	return null;
}

const TYPES_BY_EXTENSION: Record<string, string> = {
	jpg: 'image/jpeg',
	jpeg: 'image/jpeg',
	png: 'image/png',
	gif: 'image/gif',
	webp: 'image/webp',
	svg: 'image/svg+xml'
};

/** The image type a file name suggests, for pickers that report none. */
function typeFromName(name: string): string {
	return TYPES_BY_EXTENSION[name.split('.').pop()?.toLowerCase() ?? ''] ?? '';
}

/**
 * Like validateImageFile, for a file as picked: HEIC is accepted (it is
 * converted to JPEG before upload) and JPEG/HEIC may be larger, since a
 * motion photo carries its clip inside until it is split off.
 */
export function validateUploadSource(file: File): string | null {
	const heic = isHeicFile(file);
	const type = file.type || (heic ? 'image/heic' : typeFromName(file.name));
	if (!heic && !IMAGE_UPLOAD_LIMITS.allowedTypes.includes(type)) {
		return 'Unsupported format. Use JPG, PNG, GIF, WebP, SVG or HEIC';
	}
	const limit = heic || type === 'image/jpeg' ? LIVE_SOURCE_MAX_BYTES : IMAGE_UPLOAD_LIMITS.maxSize;
	if (file.size > limit) {
		return `Larger than ${limit / 1024 / 1024}MB`;
	}
	return null;
}

/**
 * POST a form with XMLHttpRequest, which (unlike fetch) reports upload progress.
 */
function postForm<T>(url: string, form: FormData, options: UploadOptions): Promise<T> {
	return new Promise((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		const { signal, onProgress } = options;

		if (signal?.aborted) {
			reject(new UploadAbortedError());
			return;
		}
		const abort = () => xhr.abort();
		signal?.addEventListener('abort', abort, { once: true });

		xhr.open('POST', url);
		xhr.setRequestHeader('Authorization', `Bearer ${pb.authStore.token}`);
		xhr.responseType = 'json';

		if (onProgress) {
			xhr.upload.onprogress = (event) => {
				if (!event.lengthComputable) return;
				onProgress({
					loaded: event.loaded,
					total: event.total,
					percentage: Math.round((event.loaded / event.total) * 100)
				});
			};
		}

		xhr.onload = () => {
			signal?.removeEventListener('abort', abort);
			const body = xhr.response;
			if (xhr.status >= 200 && xhr.status < 300 && body) {
				resolve(body as T);
				return;
			}
			const message = body?.message || (xhr.status ? `Upload failed (HTTP ${xhr.status})` : 'Upload failed');
			reject(new Error(message));
		};
		xhr.onerror = () => {
			signal?.removeEventListener('abort', abort);
			reject(new Error('Network error, check your connection'));
		};
		xhr.onabort = () => {
			signal?.removeEventListener('abort', abort);
			reject(new UploadAbortedError());
		};

		xhr.send(form);
	});
}

/**
 * Upload an image to the active provider: the built-in media library (local
 * or S3) or Chevereto. Built-in uploads are linked to their diary by the
 * server when the entry is saved with the image in it, so no entry has to
 * exist yet.
 */
export async function uploadImage(file: File, options: UploadOptions = {}): Promise<Media | CheveretoUploadResult> {
	const invalid = validateImageFile(file);
	if (invalid) {
		throw new Error(invalid);
	}

	if (!isImageUploadLoaded()) {
		await loadImageUploadSettings();
	}
	const settings = get(imageUploadSettings);

	if (settings.provider === 'chevereto') {
		const form = new FormData();
		form.append('source', file);
		const result = await postForm<{ url?: string }>('/api/v1/chevereto/upload', form, options);
		if (!result?.url) {
			throw new Error('Chevereto did not return an image URL');
		}
		return { cheveretoUrl: result.url, liveDropped: !!options.live };
	}

	const form = new FormData();
	form.append('file', file);
	form.append('name', file.name);
	if (options.alt) {
		form.append('alt', options.alt);
	}
	if (options.live) {
		form.append('live', options.live, options.live.name || 'live.mp4');
	}
	return await postForm<Media>('/api/v1/media', form, options);
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

	const url = `/api/v1/files/media/${encodeURIComponent(media.id)}/${encodeURIComponent(media.file)}`;
	return thumb ? `${url}?thumb=${encodeURIComponent(thumb)}` : url;
}

/** The URL of a media record's live photo clip, or null when it has none. */
export function getMediaLiveUrl(media: Media): string | null {
	if (!media.id || !media.live) return null;
	return `/api/v1/files/media/${encodeURIComponent(media.id)}/${encodeURIComponent(media.live)}`;
}

/**
 * Delete a media record
 * @param mediaId - The ID of the media record to delete
 */
export async function deleteMedia(mediaId: string): Promise<void> {
	try {
		const response = await fetch(`/api/v1/media/${encodeURIComponent(mediaId)}`, {
			method: 'DELETE',
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});
		if (!response.ok) {
			throw new Error(await response.text());
		}
	} catch (error) {
		console.error('Delete failed:', error);
		throw new Error('Failed to delete media. Please try again.');
	}
}
