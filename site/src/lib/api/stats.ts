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

/** Daily word counts for one explicit window. */
export interface DailySeries {
	start: string;
	end: string;
	series: number[];
	total: number;
}

/** Longest window the server will return day-by-day. */
export const MAX_SERIES_DAYS = 3700;

/** Format a Date as a local YYYY-MM-DD (never UTC-shifted). */
export function toLocalDate(date: Date): string {
	const month = String(date.getMonth() + 1).padStart(2, '0');
	const day = String(date.getDate()).padStart(2, '0');
	return `${date.getFullYear()}-${month}-${day}`;
}

export function localToday(): string {
	return toLocalDate(new Date());
}

/** The date `days - 1` before `end`, i.e. the start of an inclusive window. */
export function windowStart(end: string, days: number): string {
	const date = new Date(`${end}T00:00:00`);
	date.setDate(date.getDate() - (days - 1));
	return toLocalDate(date);
}

/**
 * Fetch the statistics overview: headline totals, the per-year series, and the
 * initial daily window. The client sends its own local date so rolling windows
 * match the calendar the user is looking at.
 */
export async function getWordStats(seriesDays = 30): Promise<WordStats> {
	const params = new URLSearchParams({
		today: localToday(),
		series_days: String(seriesDays)
	});
	const response = await fetch(`/api/v1/diaries/word-stats?${params}`, {
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

/**
 * Fetch daily word counts for one window only. Used whenever the reader
 * re-ranges the daily chart, so the headline totals and the yearly chart are
 * never refetched.
 */
export async function getDailySeries(start: string, end: string): Promise<DailySeries> {
	const params = new URLSearchParams({ start, end });
	const response = await fetch(`/api/v1/diaries/word-series?${params}`, {
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});

	if (!response.ok) {
		throw new Error(`Failed to load daily words (HTTP ${response.status})`);
	}

	const data = await response.json();
	const series: number[] = Array.isArray(data.series) ? data.series : [];
	return {
		start: data.start ?? start,
		end: data.end ?? end,
		series,
		total: typeof data.total === 'number' ? data.total : series.reduce((sum, n) => sum + n, 0)
	};
}
