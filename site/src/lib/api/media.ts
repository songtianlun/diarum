import { pb, type Media, type Diary } from './client';

export interface MediaWithDiary extends Media {
	expand?: {
		diary?: Diary[];
	};
}

/**
 * Get all media for current user, sorted by creation time
 */
export async function getAllMedia(page: number = 1, perPage: number = 50): Promise<{
	items: MediaWithDiary[];
	totalPages: number;
	totalItems: number;
}> {
	try {
		const result = await pb.collection('media').getList<MediaWithDiary>(page, perPage, {
			sort: '-created',
			expand: 'diary'
		});
		return {
			items: result.items,
			totalPages: result.totalPages,
			totalItems: result.totalItems
		};
	} catch (error) {
		console.error('Error fetching media:', error);
		return { items: [], totalPages: 0, totalItems: 0 };
	}
}

/**
 * Get media by ID with diary info
 */
export async function getMediaById(id: string): Promise<MediaWithDiary | null> {
	try {
		return await pb.collection('media').getOne<MediaWithDiary>(id, {
			expand: 'diary'
		});
	} catch (error) {
		console.error('Error fetching media:', error);
		return null;
	}
}

/**
 * Add diary association to media (supports multiple diaries)
 */
export async function addMediaDiary(mediaId: string, diaryId: string): Promise<boolean> {
	try {
		// Get current media to check existing diary associations
		const media = await pb.collection('media').getOne<Media>(mediaId);

		// Handle both old format (string) and new format (array)
		let currentDiaries: string[] = [];
		if (Array.isArray(media.diary)) {
			currentDiaries = media.diary;
		} else if (typeof media.diary === 'string' && media.diary) {
			currentDiaries = [media.diary];
		}

		// Check if already associated
		if (currentDiaries.includes(diaryId)) {
			return true;
		}

		// Add new diary to the list
		await pb.collection('media').update(mediaId, {
			diary: [...currentDiaries, diaryId]
		});
		return true;
	} catch (error) {
		console.error('Error adding diary to media:', error);
		return false;
	}
}

/**
 * Update media diary association (replace all)
 */
export async function updateMediaDiary(mediaId: string, diaryId: string): Promise<boolean> {
	try {
		await pb.collection('media').update(mediaId, { diary: [diaryId] });
		return true;
	} catch (error) {
		console.error('Error updating media:', error);
		return false;
	}
}

/**
 * Delete media by ID
 */
export async function deleteMediaById(id: string): Promise<boolean> {
	try {
		await pb.collection('media').delete(id);
		return true;
	} catch (error) {
		console.error('Error deleting media:', error);
		return false;
	}
}

/**
 * Get media URL with optional thumbnail
 */
export function getMediaFileUrl(media: Media, thumb?: string): string {
	if (!media.id || !media.file) {
		return '';
	}
	return pb.files.getUrl(media as any, media.file, { thumb });
}
