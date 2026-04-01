import { pb, type Diary } from './client';

export type DiaryByDateResult =
	| { status: 'found'; diary: Diary }
	| { status: 'not_found'; diary: null }
	| { status: 'error'; diary: null };

export interface CalendarDiaryMeta {
	date: string;
	mood?: string;
	weather?: string;
}

/**
 * Get diary by ID
 */
export async function getDiaryById(id: string): Promise<Diary | null> {
	try {
		const response = await fetch(`/api/v1/diaries/${encodeURIComponent(id)}`, {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			return null;
		}

		return await response.json();
	} catch (error) {
		console.error('Error fetching diary by ID:', error);
		return null;
	}
}

/**
 * Get multiple diaries by IDs
 */
export async function getDiariesByIds(ids: string[]): Promise<Diary[]> {
	try {
		if (ids.length === 0) return [];
		const response = await fetch('/api/v1/diaries/by-ids', {
			method: 'POST',
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`,
				'Content-Type': 'application/json'
			},
			body: JSON.stringify({ ids })
		});

		if (!response.ok) {
			return [];
		}

		const data = await response.json();
		return data.diaries || [];
	} catch (error) {
		console.error('Error fetching diaries by IDs:', error);
		return [];
	}
}

/**
 * Get diary by date with status (distinguishes not found vs request errors)
 */
export async function getDiaryByDateResult(date: string): Promise<DiaryByDateResult> {
	try {
		const response = await fetch(`/api/v1/diaries/by-date/${date}`, {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (response.status === 404) {
			return { status: 'not_found', diary: null };
		}

		if (!response.ok) {
			console.error(`Error fetching diary: HTTP ${response.status}`);
			return { status: 'error', diary: null };
		}

		const data = await response.json();
		if (!data.exists) {
			return { status: 'not_found', diary: null };
		}

		return { status: 'found', diary: data as Diary };
	} catch (error) {
		console.error('Error fetching diary:', error);
		return { status: 'error', diary: null };
	}
}

/**
 * Get diary by date
 */
export async function getDiaryByDate(date: string): Promise<Diary | null> {
	const result = await getDiaryByDateResult(date);
	return result.status === 'found' ? result.diary : null;
}

/**
 * Check if content is effectively empty (strips HTML tags and whitespace)
 */
function isContentEmpty(content: string | undefined | null): boolean {
	if (!content) return true;

	const normalized = content.replace(/&nbsp;|&#160;/gi, ' ').trim();
	if (!normalized) return true;

	// Treat media/embedded elements as meaningful content even without plain text.
	if (/<(img|video|audio|iframe|embed|object|svg|canvas)\b[^>]*>/i.test(normalized)) {
		return false;
	}

	return normalized.replace(/<[^>]*>/g, '').trim().length === 0;
}

/**
 * Create or update diary. Deletes the entry if all fields are empty.
 */
export async function saveDiary(diary: Partial<Diary>): Promise<boolean> {
	try {
		if (!pb.authStore.model?.id) {
			throw new Error('Not authenticated');
		}

		// Use custom API to get diary by date first
		const existingResult = await getDiaryByDateResult(diary.date!);
		if (existingResult.status === 'error') {
			// Fail closed when existence check fails to avoid false "saved" states.
			return false;
		}
		const existing = existingResult.diary;

		// Use effective values: incoming value takes precedence, fall back to existing record.
		// This prevents accidentally deleting an entry when only content is synced
		// but mood/weather still have values on the server.
		const effectiveContent = diary.content !== undefined ? diary.content : existing?.content;
		const effectiveMood = diary.mood !== undefined ? diary.mood : existing?.mood;
		const effectiveWeather = diary.weather !== undefined ? diary.weather : existing?.weather;

		const allEmpty =
			isContentEmpty(effectiveContent) &&
			!effectiveMood?.trim() &&
			!effectiveWeather?.trim();

		if (existing && existing.id) {
			if (allEmpty) {
				// All fields are empty — delete the entry instead of saving a blank record
				return deleteDiary(existing.id);
			}
		} else {
			if (allEmpty) {
				// Nothing to save — skip creating an empty entry
				return true;
			}
		}

		const response = await fetch('/api/v1/diaries/upsert', {
			method: 'POST',
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`,
				'Content-Type': 'application/json'
			},
			body: JSON.stringify({
				date: diary.date,
				content: diary.content ?? existing?.content ?? '',
				mood: diary.mood ?? existing?.mood ?? '',
				weather: diary.weather ?? existing?.weather ?? ''
			})
		});

		if (!response.ok) {
			return false;
		}

		return true;
	} catch (error) {
		console.error('Error saving diary:', error);
		return false;
	}
}

/**
 * Get dates with diaries in range
 */
export async function getDatesWithDiaries(start: string, end: string): Promise<CalendarDiaryMeta[]> {
	try {
		const response = await fetch(`/api/v1/diaries/exists?start=${start}&end=${end}`, {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			return [];
		}

		const data = await response.json();
		if (Array.isArray(data.entries)) {
			return data.entries.map((entry: any) => ({
				date: entry.date,
				mood: entry.mood || '',
				weather: entry.weather || ''
			}));
		}

		if (Array.isArray(data.dates)) {
			return data.dates.map((date: string) => ({ date, mood: '', weather: '' }));
		}

		return [];
	} catch (error) {
		console.error('Error fetching diary dates:', error);
		return [];
	}
}

/**
 * Get recent diaries
 */
export async function getRecentDiaries(limit: number = 5): Promise<Array<{ date: string; content: string }>> {
	try {
		const response = await fetch(`/api/v1/diaries/recent?limit=${encodeURIComponent(String(limit))}`, {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			return [];
		}

		const data = await response.json();
		const records = data.diaries || [];

		return records.map((item: any) => ({
			date: item.date,
			content: item.content || ''
		}));
	} catch (error) {
		console.error('Error fetching recent diaries:', error);
		return [];
	}
}

/**
 * Search diaries
 */
export async function searchDiaries(query: string) {
	try {
		const response = await fetch(`/api/v1/diaries/search?q=${encodeURIComponent(query)}`, {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			return [];
		}

		const data = await response.json();
		return data.results || [];
	} catch (error) {
		console.error('Error searching diaries:', error);
		return [];
	}
}

/**
 * Get diary stats (streak and total)
 */
export async function getDiaryStats(): Promise<{ streak: number; total: number }> {
	try {
		// Get user's timezone
		const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
		const url = `/api/v1/diaries/stats?tz=${encodeURIComponent(tz)}`;

		const response = await fetch(url, {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			return { streak: 0, total: 0 };
		}

		const data = await response.json();
		return {
			streak: data.streak || 0,
			total: data.total || 0
		};
	} catch (error) {
		console.error('Error fetching diary stats:', error);
		return { streak: 0, total: 0 };
	}
}

/**
 * Delete diary
 */
export async function deleteDiary(id: string): Promise<boolean> {
	try {
		const response = await fetch(`/api/v1/diaries/${encodeURIComponent(id)}`, {
			method: 'DELETE',
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			return false;
		}

		return true;
	} catch (error) {
		console.error('Error deleting diary:', error);
		return false;
	}
}
