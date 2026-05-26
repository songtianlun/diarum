import { pb, type Media, type Diary } from './client';

export interface MediaWithDiary extends Media {
    expand?: {
        diary?: Diary[];
    };
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
