import { pb } from './client';
import { ApiError, type S3Config } from './admin';

// ----- Types -----

export interface VisitTotals {
	views: number;
	ok: number;
	failed: number;
	visitors: number;
	devices: number;
	anonymous: number;
	others: number;
	diaries: number;
	ips: number;
	pulled: number;
	first?: string;
	last?: string;
}

export interface Visit {
	id: string;
	time: string;
	owner_id?: string;
	diary_id?: string;
	diary_date?: string;
	visitor_id?: string;
	visitor?: string;
	self: boolean;
	source?: 'web' | 'api' | 'mcp';
	kind?: 'view' | 'revision' | 'range';
	route?: string;
	status: number;
	success: boolean;
	reason?: string;
	ip?: string;
	ua?: string;
	device?: string;
	pulled?: boolean;
}

export interface DiaryStat {
	owner?: string;
	diary_date: string;
	diary_id?: string;
	views: number;
	ok: number;
	failed: number;
	visitors: number;
	others: number;
	first: string;
	last: string;
}

export interface VisitorStat {
	key: string;
	visitor_id?: string;
	visitor?: string;
	self: boolean;
	views: number;
	ok: number;
	failed: number;
	diaries: number;
	devices: number;
	ips: number;
	last_ip?: string;
	last_ua?: string;
	device?: string;
	first: string;
	last: string;
}

export interface KeyCount {
	key: string;
	count: number;
}

export interface VisitSummary {
	totals: VisitTotals;
	timeline: { day: string; ok: number; failed: number }[];
	top_diaries: DiaryStat[];
	top_visitors: VisitorStat[];
	sources: KeyCount[];
	reasons: KeyCount[];
	recent_failures: Visit[];
}

export interface VisitFilter {
	diary?: string;
	visitor?: string;
	device?: string;
	result?: '' | 'ok' | 'failed';
	source?: string;
	others?: boolean;
	start?: string;
	end?: string;
	q?: string;
}

export interface VisitStatus {
	enabled: boolean;
	retention_days: number;
	dedupe_seconds: number;
}

/**
 * Where statistics are read from: the signed-in user's own diaries, or (in
 * the admin console) any owner: a user ID, '-' for attempts that could not
 * be tied to an owner, '*' for everyone.
 */
export type VisitScope = { kind: 'self' } | { kind: 'admin'; owner: string };

export interface RunReport {
	kind: 'archive' | 'cleanup';
	trigger: string;
	started: string;
	finished: string;
	archived: string[];
	removed_remote: string[];
	deleted: number;
	trimmed: number;
	unloaded: number;
	kept: string[];
	error?: string;
}

export interface PullReport {
	days: string[];
	skipped: string[];
	records: number;
	invalid: number;
	errors: string[];
}

export interface VisitArchiveState {
	last_cleanup?: RunReport;
	last_archive?: RunReport;
	last_pull?: PullReport;
	next_cleanup: string;
	next_retry?: string;
	running: boolean;
}

export interface VisitSettings {
	enabled: boolean;
	retention_days: number;
	dedupe_seconds: number;
	max_records: number;
	ip_limit_per_minute: number;
	global_failed_per_minute: number;
	owner_limit_per_minute: number;
	archive: {
		enabled: boolean;
		source: 'audit' | 'custom';
		s3: S3Config;
		prefix: string;
		retention_days: number;
	};
}

export interface VisitSettingsResponse extends VisitSettings {
	secret_set: boolean;
	shared_s3_available: boolean;
	shared_s3_bucket: string;
	state: VisitArchiveState;
	server_timezone: string;
	limits: Record<string, number>;
}

export interface VisitStorage {
	records: number;
	pulled: number;
	bytes: number;
	oldest?: string;
	newest?: string;
	suppressed: number;
	path: string;
}

export interface VisitOverview {
	enabled: boolean;
	storage: VisitStorage;
	max_records: number;
	writer: { written: number; dropped: number; deduped: number; suppressed: number; queued: number };
	state: VisitArchiveState;
}

export interface OwnerStat {
	owner: string;
	username: string;
	views: number;
	failed: number;
	others: number;
	visitors: number;
	diaries: number;
	last: string;
}

export interface VisitArchive {
	day: string;
	key: string;
	size: number;
	local: boolean;
	pulled: boolean;
}

// ----- Requests -----

async function request<T>(url: string, init: RequestInit = {}): Promise<T> {
	const response = await fetch(url, {
		...init,
		headers: {
			Authorization: `Bearer ${pb.authStore.token}`,
			...(init.body ? { 'Content-Type': 'application/json' } : {}),
			...(init.headers || {})
		}
	});
	const data = await response.json().catch(() => ({}));
	if (!response.ok) {
		throw new ApiError(data.message || `HTTP ${response.status}`, response.status);
	}
	return data as T;
}

function base(scope: VisitScope): string {
	return scope.kind === 'admin' ? '/api/v1/admin/visits' : '/api/v1/visits';
}

function query(scope: VisitScope, params: Record<string, string | number | boolean | undefined>): string {
	const qs = new URLSearchParams();
	if (scope.kind === 'admin') qs.set('owner', scope.owner);
	for (const [key, value] of Object.entries(params)) {
		if (value === undefined || value === '' || value === false) continue;
		qs.set(key, value === true ? '1' : String(value));
	}
	const text = qs.toString();
	return text ? `?${text}` : '';
}

/** Minutes east of UTC, so daily buckets follow the reader's clock. */
function tzOffset(): number {
	return -new Date().getTimezoneOffset();
}

export function getVisitStatus(): Promise<VisitStatus> {
	return request('/api/v1/visits/status');
}

export function getVisitSummary(scope: VisitScope, days: number, filter: VisitFilter = {}): Promise<VisitSummary> {
	return request(`${base(scope)}/summary${query(scope, { ...filter, days, tz_offset: tzOffset() })}`);
}

export function listVisitDiaries(scope: VisitScope, params: VisitFilter & { sort?: string; limit?: number; offset?: number }): Promise<{ diaries: DiaryStat[]; total: number }> {
	return request(`${base(scope)}/diaries${query(scope, { ...params })}`);
}

export function getVisitDiary(scope: VisitScope, date: string, filter: VisitFilter = {}): Promise<{ date: string; stats: DiaryStat | null; visitors: VisitorStat[]; visitors_total: number }> {
	return request(`${base(scope)}/diaries/${encodeURIComponent(date)}${query(scope, { ...filter })}`);
}

export function listVisitors(scope: VisitScope, params: VisitFilter & { limit?: number; offset?: number }): Promise<{ visitors: VisitorStat[]; total: number }> {
	return request(`${base(scope)}/visitors${query(scope, { ...params })}`);
}

export function listVisitLogs(scope: VisitScope, params: VisitFilter & { limit?: number; offset?: number }): Promise<{ visits: Visit[]; total: number }> {
	return request(`${base(scope)}/logs${query(scope, { ...params })}`);
}

// ----- Admin -----

export function getVisitSettings(): Promise<VisitSettingsResponse> {
	return request('/api/v1/admin/visits/settings');
}

export function saveVisitSettings(settings: VisitSettings): Promise<VisitSettingsResponse> {
	return request('/api/v1/admin/visits/settings', { method: 'PUT', body: JSON.stringify(settings) });
}

export function testVisitArchive(source: string, s3: S3Config, prefix: string): Promise<{ ok: boolean }> {
	return request('/api/v1/admin/visits/settings/test', { method: 'POST', body: JSON.stringify({ source, s3, prefix }) });
}

export function getVisitOverview(): Promise<VisitOverview> {
	return request('/api/v1/admin/visits/overview');
}

export function listVisitOwners(params: { sort?: string; limit?: number; offset?: number } = {}): Promise<{ owners: OwnerStat[]; total: number }> {
	const qs = new URLSearchParams();
	if (params.sort) qs.set('sort', params.sort);
	if (params.limit) qs.set('limit', String(params.limit));
	if (params.offset) qs.set('offset', String(params.offset));
	return request(`/api/v1/admin/visits/owners?${qs}`);
}

export function listVisitArchives(): Promise<{ enabled: boolean; archives: VisitArchive[] }> {
	return request('/api/v1/admin/visits/archives');
}

export function runVisitArchive(): Promise<{ report: RunReport }> {
	return request('/api/v1/admin/visits/archives/run', { method: 'POST' });
}

export function runVisitCleanup(): Promise<{ report: RunReport }> {
	return request('/api/v1/admin/visits/cleanup', { method: 'POST' });
}

export function pullVisitArchives(params: { days?: string[]; start?: string; end?: string }): Promise<{ report: PullReport }> {
	return request('/api/v1/admin/visits/archives/pull', { method: 'POST', body: JSON.stringify(params) });
}

export function unloadPulledVisits(day = ''): Promise<{ removed: number }> {
	return request(`/api/v1/admin/visits/pulled${day ? `?day=${encodeURIComponent(day)}` : ''}`, { method: 'DELETE' });
}
