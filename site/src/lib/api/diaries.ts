import { pb, type Diary } from './client';

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
 * Get diary by date
 */
export async function getDiaryByDate(date: string): Promise<Diary | null> {
	try {
		const response = await fetch(`/api/diaries/by-date/${date}`, {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			return null;
		}

		const data = await response.json();
		return data.exists ? data : null;
	} catch (error) {
		console.error('Error fetching diary:', error);
		return null;
	}
}

/**
 * Create or update diary
 */
export async function saveDiary(diary: Partial<Diary>): Promise<boolean> {
	try {
		const userId = pb.authStore.model?.id;
		if (!userId) {
			throw new Error('Not authenticated');
		}

		// Use custom API to get diary by date first
		const existing = await getDiaryByDate(diary.date!);

		if (existing && existing.id) {
			// Update existing diary
			await pb.collection('diaries').update(existing.id, {
				content: diary.content,
				mood: diary.mood,
				weather: diary.weather
			});
		} else {
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
export async function getDatesWithDiaries(start: string, end: string): Promise<string[]> {
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
		return data.dates || [];
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
