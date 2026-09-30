import { pb, type Media, type Diary } from './client';

export interface MediaWithDiary extends Media {
    expand?: {
        diary?: Diary[];
    };
    /** On the library timeline: "media" (stored by Diarum) or "external". */
    kind?: 'media' | 'external';
    /** Whether Diarum stores the image, so it can be selected and deleted here. */
    managed?: boolean;
    /** The image address, for external images. */
    url?: string;
}

export async function getAllMedia(page: number = 1, perPage: number = 50): Promise<{
    items: MediaWithDiary[];
    totalPages: number;
    totalItems: number;
}> {
    try {
        const response = await fetch(`/api/v1/media?page=${page}&perPage=${perPage}`, {
            headers: { Authorization: `Bearer ${pb.authStore.token}` }
        });
        if (!response.ok) return { items: [], totalPages: 0, totalItems: 0 };
        const result = await response.json();
        return { items: result.items || [], totalPages: result.totalPages || 0, totalItems: result.totalItems || 0 };
    } catch (error) {
        console.error('Error fetching media:', error);
        return { items: [], totalPages: 0, totalItems: 0 };
    }
}

/**
 * Like getAllMedia, but throws on failure so callers can tell an empty
 * library from an unreachable one.
 */
export async function fetchMediaPage(page: number, perPage: number, signal?: AbortSignal): Promise<{
    items: MediaWithDiary[];
    totalPages: number;
    totalItems: number;
}> {
    const response = await fetch(`/api/v1/media?page=${page}&perPage=${perPage}`, {
        headers: { Authorization: `Bearer ${pb.authStore.token}` },
        signal
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok) {
        throw new Error(result?.message || 'Failed to load media');
    }
    return { items: result.items || [], totalPages: result.totalPages || 0, totalItems: result.totalItems || 0 };
}

/**
 * A page of the library timeline: images stored by Diarum plus external
 * images (e.g. Chevereto) entries show, each once, by the image's own date.
 */
export async function fetchGalleryPage(page: number, perPage: number, signal?: AbortSignal): Promise<{
    items: MediaWithDiary[];
    totalPages: number;
    totalItems: number;
}> {
    const response = await fetch(`/api/v1/media/gallery?page=${page}&perPage=${perPage}`, {
        headers: { Authorization: `Bearer ${pb.authStore.token}` },
        signal
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok) {
        throw new Error(result?.message || 'Failed to load media');
    }
    return { items: result.items || [], totalPages: result.totalPages || 0, totalItems: result.totalItems || 0 };
}

export async function getMediaById(id: string): Promise<MediaWithDiary | null> {
    try {
        const response = await fetch(`/api/v1/media/${encodeURIComponent(id)}`, {
            headers: { Authorization: `Bearer ${pb.authStore.token}` }
        });
        if (!response.ok) return null;
        return await response.json();
    } catch (error) {
        console.error('Error fetching media:', error);
        return null;
    }
}

export async function addMediaDiary(mediaId: string, diaryId: string): Promise<boolean> {
    try {
        const media = await getMediaById(mediaId);
        if (!media) return false;
        const currentDiaries = Array.isArray(media.diary) ? media.diary : [];
        if (currentDiaries.includes(diaryId)) return true;
        const response = await fetch(`/api/v1/media/${encodeURIComponent(mediaId)}`, {
            method: 'PATCH',
            headers: {
                Authorization: `Bearer ${pb.authStore.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ diary: [...currentDiaries, diaryId] })
        });
        return response.ok;
    } catch (error) {
        console.error('Error adding diary to media:', error);
        return false;
    }
}

export async function updateMediaDiary(mediaId: string, diaryId: string): Promise<boolean> {
    try {
        const response = await fetch(`/api/v1/media/${encodeURIComponent(mediaId)}`, {
            method: 'PATCH',
            headers: {
                Authorization: `Bearer ${pb.authStore.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ diary: [diaryId] })
        });
        return response.ok;
    } catch (error) {
        console.error('Error updating media:', error);
        return false;
    }
}

export async function deleteMediaById(id: string): Promise<boolean> {
    try {
        const response = await fetch(`/api/v1/media/${encodeURIComponent(id)}`, {
            method: 'DELETE',
            headers: { Authorization: `Bearer ${pb.authStore.token}` }
        });
        return response.ok;
    } catch (error) {
        console.error('Error deleting media:', error);
        return false;
    }
}

export function getMediaFileUrl(media: Media, thumb?: string): string {
    if (!media.id || !media.file) return '';
    const url = `/api/v1/files/media/${encodeURIComponent(media.id)}/${encodeURIComponent(media.file)}`;
    return thumb ? `${url}?thumb=${encodeURIComponent(thumb)}` : url;
}

/**
 * Direct (S3 public URL) addresses of image URLs, for sharing. URLs without
 * one are left out of the result; on failure the result is empty.
 */
export async function fetchShareUrls(urls: string[]): Promise<Record<string, string>> {
    if (urls.length === 0) return {};
    try {
        const response = await fetch('/api/v1/media/share-urls', {
            method: 'POST',
            headers: { Authorization: `Bearer ${pb.authStore.token}`, 'Content-Type': 'application/json' },
            body: JSON.stringify({ urls })
        });
        if (!response.ok) return {};
        const result = await response.json();
        return result?.urls ?? {};
    } catch (error) {
        console.error('Error resolving share URLs:', error);
        return {};
    }
}

// ---------------------------------------------------------------------------
// Trash, statistics, unused image scan and housekeeping settings

export interface MediaPage {
    items: MediaWithDiary[];
    totalPages: number;
    totalItems: number;
}

export interface TrashPage extends MediaPage {
    /** Days an image stays in the trash; 0 keeps it until removed by hand. */
    retentionDays: number;
}

export interface MediaBatchResult {
    done: string[];
    failed: Record<string, string>;
}

export interface MediaLibraryStats {
    provider: string;
    /** Images stored with the current provider. */
    current: number;
    stats: {
        total: number;
        byStorage: Record<string, number>;
        linked: number;
        trash: number;
        oldestTrash: string;
        /** External images (e.g. Chevereto) entries show; listed in the library but managed where hosted. */
        external?: number;
    };
}

export interface MediaLibrarySettings {
    trash_retention_days: number;
    auto_clean_unlinked: boolean;
}

async function mediaRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = { Authorization: `Bearer ${pb.authStore.token}` };
    if (init.body) headers['Content-Type'] = 'application/json';
    const response = await fetch(`/api/v1/media${path}`, { ...init, headers: { ...headers, ...(init.headers as Record<string, string> | undefined) } });
    const result = await response.json().catch(() => ({}));
    if (!response.ok) {
        throw new Error(result?.message || `Request failed (${response.status})`);
    }
    return result as T;
}

export async function fetchTrashPage(page: number, perPage: number, signal?: AbortSignal): Promise<TrashPage> {
    const result = await mediaRequest<Partial<TrashPage>>(`/trash?page=${page}&perPage=${perPage}`, { signal });
    return {
        items: result.items || [],
        totalPages: result.totalPages || 0,
        totalItems: result.totalItems || 0,
        retentionDays: result.retentionDays ?? 30
    };
}

/** IDs of every image in the trash, loaded or not, and the batch size limit. */
export async function fetchTrashIds(): Promise<{ ids: string[]; maxBatch: number }> {
    const result = await mediaRequest<{ ids?: string[]; maxBatch?: number }>('/trash/ids');
    return { ids: result.ids || [], maxBatch: result.maxBatch || 1000 };
}

/** Sends ids in batches the server accepts and merges the outcomes. */
async function batchMediaRequest(path: string, ids: string[], batchSize = 1000): Promise<MediaBatchResult> {
    const merged: MediaBatchResult = { done: [], failed: {} };
    for (let start = 0; start < ids.length; start += batchSize) {
        const chunk = ids.slice(start, start + batchSize);
        const result = await mediaRequest<MediaBatchResult>(path, { method: 'POST', body: JSON.stringify({ ids: chunk }) });
        merged.done.push(...(result.done || []));
        Object.assign(merged.failed, result.failed || {});
    }
    return merged;
}

/** Moves stored images to the trash, like deleting them one by one. */
export function trashMedia(ids: string[], batchSize?: number): Promise<MediaBatchResult> {
    return batchMediaRequest('/trash', ids, batchSize);
}

export function restoreMedia(ids: string[], batchSize?: number): Promise<MediaBatchResult> {
    return batchMediaRequest('/trash/restore', ids, batchSize);
}

export function purgeMedia(ids: string[], batchSize?: number): Promise<MediaBatchResult> {
    return batchMediaRequest('/trash/purge', ids, batchSize);
}

export function emptyTrash(): Promise<MediaBatchResult> {
    return mediaRequest('/trash/empty', { method: 'POST' });
}

export function getMediaStats(): Promise<MediaLibraryStats> {
    return mediaRequest('/stats');
}

export function scanUnlinkedMedia(): Promise<{ items: MediaWithDiary[]; total: number }> {
    return mediaRequest('/unlinked');
}

export function cleanUnlinkedMedia(ids: string[]): Promise<MediaBatchResult> {
    return mediaRequest('/unlinked/clean', { method: 'POST', body: JSON.stringify({ ids }) });
}

export function getMediaLibrarySettings(): Promise<MediaLibrarySettings> {
    return mediaRequest('/settings');
}

export async function saveMediaLibrarySettings(settings: MediaLibrarySettings): Promise<MediaLibrarySettings> {
    const result = await mediaRequest<{ settings: MediaLibrarySettings }>('/settings', { method: 'PUT', body: JSON.stringify(settings) });
    return result.settings;
}
