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
		const record = await pb.collection('diaries').getOne(id);
		return {
			id: record.id,
			date: record.date.split(' ')[0],
			content: record.content || '',
			mood: record.mood,
			weather: record.weather,
			owner: record.owner,
			created: record.created,
			updated: record.updated
		};
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
		const filter = ids.map(id => `id="${id}"`).join(' || ');
		const records = await pb.collection('diaries').getFullList({
			filter,
			sort: '-date'
		});
		return records.map((record: any) => ({
			id: record.id,
			date: record.date.split(' ')[0],
			content: record.content || '',
			mood: record.mood,
			weather: record.weather,
			owner: record.owner,
			created: record.created,
			updated: record.updated
		}));
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
		const response = await fetch(`/api/diaries/by-date/${date}`, {
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
		const userId = pb.authStore.model?.id;
		if (!userId) {
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
			// Update existing diary
			await pb.collection('diaries').update(existing.id, {
				content: diary.content,
				mood: diary.mood,
				weather: diary.weather
			});
		} else {
			if (allEmpty) {
				// Nothing to save — skip creating an empty entry
				return true;
			}
			// Create new diary
			const data: any = {
				date: diary.date + ' 00:00:00.000Z', // Use full timestamp format
				content: diary.content || '',
				owner: userId
			};

			// Only add optional fields if they have values
			if (diary.mood) data.mood = diary.mood;
			if (diary.weather) data.weather = diary.weather;

			await pb.collection('diaries').create(data);
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
		const response = await fetch(`/api/diaries/exists?start=${start}&end=${end}`, {
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

		// Backward compatibility for older API responses.
		// If custom API doesn't return entries metadata yet, fetch full records directly.
		if (Array.isArray(data.dates) && data.dates.length > 0) {
			try {
				const startTime = `${start} 00:00:00.000Z`;
				const endTime = `${end} 23:59:59.999Z`;
				const records = await pb.collection('diaries').getFullList({
					filter: `date >= \"${startTime}\" && date <= \"${endTime}\"`,
					fields: 'date,mood,weather',
					sort: '-date'
				});

				return records.map((record: any) => ({
					date: (record.date || '').split(' ')[0],
					mood: record.mood || '',
					weather: record.weather || ''
				}));
			} catch (fallbackError) {
				console.error('Error fetching diary metadata fallback:', fallbackError);
			}
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
		const records = await pb.collection('diaries').getList(1, limit, {
			sort: '-date',
			fields: 'date,content'
		});

		return records.items.map((item: any) => ({
			date: item.date.split(' ')[0],
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
		const response = await fetch(`/api/diaries/search?q=${encodeURIComponent(query)}`, {
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
		const url = `/api/diaries/stats?tz=${encodeURIComponent(tz)}`;

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
		await pb.collection('diaries').delete(id);
		return true;
	} catch (error) {
		console.error('Error deleting diary:', error);
		return false;
	}
}
