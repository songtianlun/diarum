import { pb } from './client';
import { ApiError } from './admin';

export type DayRepeat = '' | 'week' | 'month' | 'year';

export interface Day {
	id: string;
	name: string;
	/** YYYY-MM-DD */
	date: string;
	emoji: string;
	note: string;
	repeat: DayRepeat;
	include_start: boolean;
	show_on_home: boolean;
	/** Number of milestones the day has. */
	milestones: number;
	/** IDs of the day's tags. */
	tags: string[];
	/** Date of the end milestone ("" for none): the count stops there. */
	end: string;
	created: string;
	updated: string;
}

export type DayInput = Pick<Day, 'name' | 'date' | 'emoji' | 'note' | 'repeat' | 'include_start' | 'show_on_home' | 'tags'>;

export interface DayTag {
	id: string;
	name: string;
	/** A colour from TAG_COLORS (any CSS colour works). */
	color: string;
	/** How many days carry the tag. */
	days: number;
	created: string;
	updated: string;
}

export type DayTagInput = Pick<DayTag, 'name' | 'color'>;

export interface Milestone {
	id: string;
	day: string;
	name: string;
	date: string;
	note: string;
	/** The day stops counting on this milestone's date; at most one per day. */
	end: boolean;
	created: string;
	updated: string;
}

export type MilestoneInput = Pick<Milestone, 'name' | 'date' | 'note' | 'end'>;

async function request<T>(url: string, init: RequestInit = {}): Promise<T> {
	const response = await fetch(url, {
		...init,
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`,
			...(init.body ? { 'Content-Type': 'application/json' } : {}),
			...(init.headers || {})
		}
	});
	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new ApiError(data.message || `HTTP ${response.status}`, response.status);
	}
	if (response.status === 204) return undefined as T;
	return (await response.json()) as T;
}

const base = '/api/v1/days';
const dayUrl = (id: string) => `${base}/${encodeURIComponent(id)}`;

export async function listDays(): Promise<Day[]> {
	const data = await request<{ days: Day[] }>(base);
	return data.days ?? [];
}

export function getDay(id: string): Promise<{ day: Day; milestones: Milestone[] }> {
	return request(dayUrl(id));
}

export function createDay(input: DayInput): Promise<Day> {
	return request(base, { method: 'POST', body: JSON.stringify(input) });
}

export function updateDay(id: string, input: DayInput): Promise<Day> {
	return request(dayUrl(id), { method: 'PUT', body: JSON.stringify(input) });
}

export function deleteDay(id: string): Promise<void> {
	return request(dayUrl(id), { method: 'DELETE' });
}

export function createMilestone(dayId: string, input: MilestoneInput): Promise<Milestone> {
	return request(`${dayUrl(dayId)}/milestones`, { method: 'POST', body: JSON.stringify(input) });
}

export function updateMilestone(dayId: string, id: string, input: MilestoneInput): Promise<Milestone> {
	return request(`${dayUrl(dayId)}/milestones/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(input) });
}

export function deleteMilestone(dayId: string, id: string): Promise<void> {
	return request(`${dayUrl(dayId)}/milestones/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

const tagsUrl = `${base}/tags`;

export async function listDayTags(): Promise<DayTag[]> {
	const data = await request<{ tags: DayTag[] }>(tagsUrl);
	return data.tags ?? [];
}

export function createDayTag(input: DayTagInput): Promise<DayTag> {
	return request(tagsUrl, { method: 'POST', body: JSON.stringify(input) });
}

export function updateDayTag(id: string, input: DayTagInput): Promise<DayTag> {
	return request(`${tagsUrl}/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(input) });
}

export function deleteDayTag(id: string): Promise<void> {
	return request(`${tagsUrl}/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

/** Every milestone of every day (for the overview Gantt chart). */
export async function listAllMilestones(): Promise<Milestone[]> {
	const data = await request<{ milestones: Milestone[] }>(`${base}/milestones`);
	return data.milestones ?? [];
}
