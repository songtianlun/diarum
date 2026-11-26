import { pb } from './client';

export type CNHolidayPreference = 'auto' | 'on' | 'off';

export interface CNHolidayDay {
	name: string;
	date: string;
	isOffDay: boolean;
	custom?: boolean;
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

export interface CNHolidayFeatureStatus {
	region: string;
	enabled: boolean;
}

export interface AdminCNHolidayOverview {
	region: string;
	enabled: boolean;
	years: CNHolidayYearSummary[];
	last_fetched_at?: string;
	overrides: CNHolidayDay[];
}

async function parseError(response: Response, fallback: string): Promise<Error> {
	const data = await response.json().catch(() => ({}));
	return new Error(data?.message || `${fallback} (${response.status})`);
}

/** Lightweight feature check for regular users. */
export async function getCNHolidayFeatureStatus(): Promise<CNHolidayFeatureStatus> {
	const response = await fetch('/api/v1/holidays/cn/status', {
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});
	if (!response.ok) {
		throw await parseError(response, 'Failed to load holiday feature status');
	}
	return await response.json();
}

/** Fetch merged holiday days for one or more years (only populated while globally enabled). */
export async function fetchCNHolidays(years: number[]): Promise<{ enabled: boolean; years: Record<number, CNHolidayYearResponse> }> {
	const unique = Array.from(new Set(years.filter((y) => Number.isInteger(y) && y >= 2000 && y <= 2100)));
	if (unique.length === 0) return { enabled: true, years: {} };

	const response = await fetch(`/api/v1/holidays/cn?years=${unique.join(',')}`, {
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});
	if (!response.ok) {
		throw await parseError(response, 'Failed to fetch holiday data');
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
	return { enabled: !!data?.enabled, years: out };
}

// ----- Admin APIs -----

export async function getAdminCNHolidays(): Promise<AdminCNHolidayOverview> {
	const response = await fetch('/api/v1/admin/holidays/cn', {
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});
	if (!response.ok) {
		throw await parseError(response, 'Failed to load admin holiday settings');
	}
	return await response.json();
}

export async function getAdminCNHolidayYear(year: number): Promise<CNHolidayYearResponse> {
	const response = await fetch(`/api/v1/admin/holidays/cn/year?year=${year}`, {
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});
	if (!response.ok) {
		throw await parseError(response, 'Failed to load holiday year');
	}
	return await response.json();
}

export async function setAdminCNHolidaysEnabled(enabled: boolean): Promise<{ enabled: boolean }> {
	const response = await fetch('/api/v1/admin/holidays/cn/settings', {
		method: 'PUT',
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ enabled })
	});
	if (!response.ok) {
		throw await parseError(response, 'Failed to update holiday settings');
	}
	return await response.json();
}

export async function refreshAdminCNHolidays(): Promise<AdminCNHolidayOverview> {
	const response = await fetch('/api/v1/admin/holidays/cn/refresh', {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});
	if (!response.ok) {
		throw await parseError(response, 'Failed to refresh holiday data');
	}
	return await response.json();
}

export async function upsertAdminCNHolidayOverride(payload: {
	date: string;
	name: string;
	isOffDay: boolean;
}): Promise<CNHolidayDay> {
	const response = await fetch('/api/v1/admin/holidays/cn/overrides', {
		method: 'PUT',
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify(payload)
	});
	if (!response.ok) {
		throw await parseError(response, 'Failed to save holiday override');
	}
	return await response.json();
}

export async function deleteAdminCNHolidayOverride(date: string): Promise<void> {
	const response = await fetch(`/api/v1/admin/holidays/cn/overrides/${encodeURIComponent(date)}`, {
		method: 'DELETE',
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		}
	});
	if (!response.ok) {
		throw await parseError(response, 'Failed to delete holiday override');
	}
}

export async function importAdminCNHolidayJSON(file: File): Promise<{
	imported: CNHolidayYearResponse;
	overview: AdminCNHolidayOverview;
}> {
	const formData = new FormData();
	formData.append('file', file);
	const response = await fetch('/api/v1/admin/holidays/cn/import', {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`
		},
		body: formData
	});
	if (!response.ok) {
		throw await parseError(response, 'Failed to import holiday JSON');
	}
	return await response.json();
}
