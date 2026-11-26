import { pb } from './client';

export type CNHolidayPreference = 'auto' | 'on' | 'off';

export interface CNHolidayDay {
	name: string;
	date: string;
	isOffDay: boolean;
}

export interface CNHolidayYearResponse {
	region: string;
	year: number;
	published: boolean;
	days: CNHolidayDay[];
	papers: string[];
	source: string;
	fetched_at?: string;
}

export interface CNHolidayYearSummary {
	year: number;
	published: boolean;
	day_count: number;
	source: string;
	fetched_at?: string;
	papers?: string[];
}

export interface CNHolidayStatus {
	region: string;
	years: CNHolidayYearSummary[];
	last_fetched_at?: string;
}

export async function fetchCNHolidays(years: number[]): Promise<Record<number, CNHolidayYearResponse>> {
	const unique = Array.from(new Set(years.filter((y) => Number.isInteger(y) && y >= 2000 && y <= 2100)));
	if (unique.length === 0) return {};

	const response = await fetch(`/api/v1/holidays/cn?years=${unique.join(',')}`, {
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});
	if (!response.ok) {
		throw new Error(`Failed to fetch holiday data (${response.status})`);
	}
	const data = await response.json();
	const rawYears = (data?.years ?? {}) as Record<string, CNHolidayYearResponse>;
	const out: Record<number, CNHolidayYearResponse> = {};
	for (const [k, v] of Object.entries(rawYears)) {
		const year = Number(k);
		if (Number.isInteger(year) && v) {
			out[year] = v;
		}
	}
	return out;
}

export async function getCNHolidayStatus(): Promise<CNHolidayStatus> {
	const response = await fetch('/api/v1/holidays/cn/status', {
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});
	if (!response.ok) {
		throw new Error(`Failed to load holiday status (${response.status})`);
	}
	return await response.json();
}

export async function refreshCNHolidays(): Promise<CNHolidayStatus> {
	const response = await fetch('/api/v1/holidays/cn/refresh', {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});
	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new Error(data.message || `Failed to refresh holiday data (${response.status})`);
	}
	return await response.json();
}
