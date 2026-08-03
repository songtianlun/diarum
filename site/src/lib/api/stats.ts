import { pb } from './client';

export interface YearWordCount {
	year: number;
	words: number;
	entries: number;
}

/**
 * Everything the statistics page renders, delivered by a single request.
 *
 * `series` is a dense day-by-day word count running from `series_start` to
 * `series_end` inclusive, so the client can re-slice any range (7 days, a
 * year, ...) without going back to the server.
 */
export interface WordStats {
	total_words: number;
	total_entries: number;
	last_month_words: number;
	last_six_months_words: number;
	last_twelve_months_words: number;
	first_date: string;
	last_date: string;
	series_start: string;
	series_end: string;
	series: number[];
	yearly: YearWordCount[];
}

export const emptyWordStats: WordStats = {
	total_words: 0,
	total_entries: 0,
	last_month_words: 0,
	last_six_months_words: 0,
	last_twelve_months_words: 0,
	first_date: '',
	last_date: '',
	series_start: '',
	series_end: '',
	series: [],
	yearly: []
};

/** Local calendar date as YYYY-MM-DD (never UTC-shifted). */
function localToday(): string {
	const now = new Date();
	const month = String(now.getMonth() + 1).padStart(2, '0');
	const day = String(now.getDate()).padStart(2, '0');
	return `${now.getFullYear()}-${month}-${day}`;
}

/**
 * Fetch all diary word statistics in one call.
 * The server sends its own local date so rolling windows match the calendar
 * the user is looking at.
 */
export async function getWordStats(): Promise<WordStats> {
	const response = await fetch(`/api/v1/diaries/word-stats?today=${localToday()}`, {
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});

	if (!response.ok) {
		throw new Error(`Failed to load statistics (HTTP ${response.status})`);
	}

	const data = await response.json();
	return {
		...emptyWordStats,
		...data,
		series: Array.isArray(data.series) ? data.series : [],
		yearly: Array.isArray(data.yearly) ? data.yearly : []
	};
}
