import { pb } from './client';

// ----- Types -----

export interface DailyCount {
	date: string;
	count: number;
}

export interface SystemStats {
	users: number;
	admins: number;
	new_users_30d: number;
	active_users_7d: number;
	diaries: number;
	diaries_today: number;
	diaries_7d: number;
	revisions: number;
	media: number;
	conversations: number;
	messages: number;
	database_bytes: number;
	daily_diaries: DailyCount[];
	daily_users: DailyCount[];
}

export interface Overview {
	stats: SystemStats;
	system: {
		version: string;
		go_version: string;
		started: string;
		uptime_seconds: number;
		server_timezone: string;
		goroutines: number;
	};
	audit: {
		enabled: boolean;
		files: number;
		bytes: number;
		writer: { written: number; dropped: number; queued: number };
	};
}

export type Role = 'user' | 'admin';

export interface AdminUser {
	id: string;
	username: string;
	email: string;
	name: string;
	role: Role;
	created: string;
	updated: string;
	diaries: number;
	media: number;
	conversations: number;
	last_diary_at: string;
}

export interface UserList {
	users: AdminUser[];
	total: number;
	admins: number;
}

export interface AuditEntry {
	time: string;
	user_id?: string;
	user?: string;
	source?: string;
	action?: string;
	target?: string;
	method?: string;
	route?: string;
	path?: string;
	status?: number;
	ms?: number;
	ip?: string;
	ua?: string;
	error?: string;
	detail?: Record<string, unknown>;
}

export interface Count {
	key: string;
	label?: string;
	count: number;
}

export interface IPCount {
	ip: string;
	count: number;
	errors: number;
	failed_logins: number;
	users: number;
	last_seen: string;
}

export interface RouteCount {
	method: string;
	route: string;
	count: number;
	errors: number;
	avg_ms: number;
	max_ms: number;
}

export interface AuditStats {
	total: number;
	users: number;
	ips: number;
	anonymous: number;
	client_errors: number;
	server_errors: number;
	login_ok: number;
	login_failed: number;
	denied: number;
	writes: number;
	avg_ms: number;
	p95_ms: number;
	max_ms: number;
	first?: string;
	last?: string;
	actions: Count[];
	status: Count[];
	methods: Count[];
	sources: Count[];
	top_users: Count[];
	top_ips: IPCount[];
	routes: RouteCount[];
	failed_logins: Count[];
	bucket: 'hour' | 'day';
	timeline: { start: string; count: number; errors: number }[];
}

export interface AuditResult {
	entries: AuditEntry[];
	total: number;
	files: number;
	bytes: number;
	took_ms: number;
	stats?: AuditStats;
}

export interface AuditQuery {
	start?: Date;
	end?: Date;
	user?: string;
	actions?: string[];
	status?: string;
	method?: string;
	source?: string;
	ip?: string;
	q?: string;
	pulled?: boolean;
	offset?: number;
	/** 0 returns statistics only. */
	limit?: number;
	stats?: boolean;
}

export interface S3Config {
	bucket: string;
	region: string;
	endpoint: string;
	access_key: string;
	/** Never returned by the server; leave empty to keep the stored one. */
	secret: string;
	force_path_style: boolean;
	prefix: string;
}

export interface RunReport {
	kind: 'archive' | 'cleanup';
	trigger: string;
	started: string;
	finished: string;
	archived: string[];
	removed_local: string[];
	removed_remote: string[];
	removed_pulled: string[];
	kept: string[];
	error?: string;
}

export interface ArchiveState {
	last_cleanup?: RunReport;
	last_archive?: RunReport;
	next_cleanup: string;
	next_retry?: string;
	running: boolean;
}

export interface AuditSettings {
	retention_days: number;
	archive: {
		enabled: boolean;
		s3: S3Config;
		retention_days: number;
	};
	secret_set: boolean;
	state: ArchiveState;
	server_timezone: string;
	enabled: boolean;
	limits: {
		retention_min: number;
		retention_max: number;
		retention_default: number;
		archive_retention_min: number;
		archive_retention_max: number;
		archive_retention_def: number;
	};
}

export interface AuditFile {
	date: string;
	size: number;
	pulled: boolean;
	pulled_at?: string;
}

export interface ArchiveInfo {
	date: string;
	key: string;
	size: number;
	local: boolean;
	pulled: boolean;
}

// ----- Requests -----

export class ApiError extends Error {
	constructor(
		message: string,
		public status: number
	) {
		super(message);
	}
}

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

/** Drop sub-second precision: the server parses plain RFC 3339. */
function toRFC3339(date: Date): string {
	return date.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

export function getOverview(): Promise<Overview> {
	return request<Overview>('/api/v1/admin/overview');
}

export function listUsers(params: { q?: string; role?: string; sort?: string; limit?: number; offset?: number } = {}): Promise<UserList> {
	const qs = new URLSearchParams();
	if (params.q?.trim()) qs.set('q', params.q.trim());
	if (params.role) qs.set('role', params.role);
	if (params.sort) qs.set('sort', params.sort);
	if (params.limit) qs.set('limit', String(params.limit));
	if (params.offset) qs.set('offset', String(params.offset));
	return request<UserList>(`/api/v1/admin/users?${qs}`);
}

export function setUserRole(id: string, role: Role): Promise<AdminUser> {
	return request(`/api/v1/admin/users/${encodeURIComponent(id)}/role`, {
		method: 'PUT',
		body: JSON.stringify({ role })
	});
}

export function queryAudit(query: AuditQuery): Promise<AuditResult> {
	const qs = new URLSearchParams();
	if (query.start) qs.set('start', toRFC3339(query.start));
	if (query.end) qs.set('end', toRFC3339(new Date(Math.ceil(query.end.getTime() / 1000) * 1000)));
	if (query.user) qs.set('user', query.user);
	if (query.actions?.length) qs.set('action', query.actions.join(','));
	if (query.status) qs.set('status', query.status);
	if (query.method) qs.set('method', query.method);
	if (query.source) qs.set('source', query.source);
	if (query.ip?.trim()) qs.set('ip', query.ip.trim());
	if (query.q?.trim()) qs.set('q', query.q.trim());
	if (query.pulled) qs.set('pulled', '1');
	if (query.offset) qs.set('offset', String(query.offset));
	if (query.limit !== undefined) qs.set('limit', String(query.limit));
	if (query.stats) qs.set('stats', '1');
	try {
		qs.set('tz', Intl.DateTimeFormat().resolvedOptions().timeZone);
	} catch {
		// The server falls back to its own timezone.
	}
	return request<AuditResult>(`/api/v1/admin/audit/logs?${qs}`);
}

export function getAuditSettings(): Promise<AuditSettings> {
	return request<AuditSettings>('/api/v1/admin/audit/settings');
}

export function saveAuditSettings(settings: Pick<AuditSettings, 'retention_days' | 'archive'>): Promise<AuditSettings> {
	return request<AuditSettings>('/api/v1/admin/audit/settings', {
		method: 'PUT',
		body: JSON.stringify(settings)
	});
}

export function testAuditArchive(s3: S3Config): Promise<{ ok: boolean }> {
	return request('/api/v1/admin/audit/settings/test', { method: 'POST', body: JSON.stringify(s3) });
}

export function listAuditFiles(): Promise<{ files: AuditFile[]; state: ArchiveState }> {
	return request('/api/v1/admin/audit/files');
}

export function listArchives(): Promise<{ enabled: boolean; archives: ArchiveInfo[] }> {
	return request('/api/v1/admin/audit/archives');
}

export function runArchive(): Promise<{ report: RunReport }> {
	return request('/api/v1/admin/audit/archives/run', { method: 'POST' });
}

export function runCleanup(): Promise<{ report: RunReport }> {
	return request('/api/v1/admin/audit/cleanup', { method: 'POST' });
}

export function pullArchive(date: string): Promise<{ file: AuditFile }> {
	return request(`/api/v1/admin/audit/archives/${encodeURIComponent(date)}/pull`, { method: 'POST' });
}

export function removePulled(date: string): Promise<void> {
	return request(`/api/v1/admin/audit/pulled/${encodeURIComponent(date)}`, { method: 'DELETE' });
}

/** Download one raw daily log file (JSON lines). */
export async function downloadAuditFile(date: string, pulled: boolean): Promise<void> {
	const response = await fetch(`/api/v1/admin/audit/files/${encodeURIComponent(date)}${pulled ? '?pulled=1' : ''}`, {
		headers: { Authorization: `Bearer ${pb.authStore.token}` }
	});
	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new ApiError(data.message || `HTTP ${response.status}`, response.status);
	}
	const blob = await response.blob();
	const url = URL.createObjectURL(blob);
	const link = document.createElement('a');
	link.href = url;
	link.download = `diarum-system-audit-${date}${pulled ? '-pulled' : ''}.log`;
	document.body.appendChild(link);
	link.click();
	link.remove();
	setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// ----- Formatting helpers shared by the admin views -----

export function formatBytes(size: number): string {
	if (size < 1024) return `${size} B`;
	if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
	if (size < 1024 * 1024 * 1024) return `${(size / 1024 / 1024).toFixed(1)} MB`;
	return `${(size / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

export function shortUA(ua?: string): string {
	if (!ua) return '';
	const browser = /Edg\//.test(ua) ? 'Edge' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : /curl\//i.test(ua) ? 'curl' : '';
	const os = /iPhone|iPad/.test(ua) ? 'iOS' : /Android/.test(ua) ? 'Android' : /Mac OS X/.test(ua) ? 'macOS' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : '';
	const label = [browser, os].filter(Boolean).join(' · ');
	return label || ua.slice(0, 40);
}

/** Stored timestamps look like "2026-09-28 07:21:02.000Z"; make them parseable. */
export function parseStoredTime(value: string): Date | null {
	if (!value) return null;
	const date = new Date(value.replace(' ', 'T'));
	return Number.isNaN(date.getTime()) ? null : date;
}
