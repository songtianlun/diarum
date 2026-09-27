import { pb } from './client';

export interface AuditEntry {
	time: string;
	user: string;
	actor?: string;
	action: string;
	target?: string;
	source?: string;
	ip?: string;
	ua?: string;
	detail?: Record<string, unknown>;
}

export interface AuditFile {
	date: string;
	size: number;
	entries: number;
}

export interface AuditSettings {
	retention_days: number;
	default: number;
	min: number;
	max: number;
	enabled: boolean;
}

export interface AuditQuery {
	q?: string;
	action?: string;
	/** Inclusive bounds as Date objects; sent as RFC 3339 instants. */
	start?: Date;
	end?: Date;
	limit?: number;
}

export interface AuditResult {
	entries: AuditEntry[];
	truncated: boolean;
	scanned: number;
}

export const DEFAULT_AUDIT_RETENTION = 7;
export const MIN_AUDIT_RETENTION = 1;
export const MAX_AUDIT_RETENTION = 365;

export function sanitizeAuditRetention(value: unknown): number {
	const parsed = typeof value === 'number' ? value : Number.parseInt(String(value ?? ''), 10);
	if (!Number.isFinite(parsed)) return DEFAULT_AUDIT_RETENTION;
	return Math.min(MAX_AUDIT_RETENTION, Math.max(MIN_AUDIT_RETENTION, Math.trunc(parsed)));
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
		throw new Error(data.message || `HTTP ${response.status}`);
	}
	return (await response.json()) as T;
}

/** Drop sub-second precision: the server parses plain RFC 3339. */
function toRFC3339(date: Date): string {
	return date.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

export async function getAuditSettings(): Promise<AuditSettings> {
	return request<AuditSettings>('/api/v1/audit/settings');
}

export async function saveAuditSettings(retentionDays: number): Promise<AuditSettings> {
	return request<AuditSettings>('/api/v1/audit/settings', {
		method: 'PUT',
		body: JSON.stringify({ retention_days: sanitizeAuditRetention(retentionDays) })
	});
}

export async function listAuditFiles(): Promise<AuditFile[]> {
	const data = await request<{ files: AuditFile[] }>('/api/v1/audit/files');
	return data.files || [];
}

export async function queryAuditEntries(query: AuditQuery = {}): Promise<AuditResult> {
	const params = new URLSearchParams();
	if (query.q?.trim()) params.set('q', query.q.trim());
	if (query.action) params.set('action', query.action);
	if (query.start) params.set('start', toRFC3339(query.start));
	if (query.end) {
		// Round the end up so the last second of the range is included.
		params.set('end', toRFC3339(new Date(Math.ceil(query.end.getTime() / 1000) * 1000)));
	}
	if (query.limit) params.set('limit', String(query.limit));
	const qs = params.toString();
	const data = await request<AuditResult>(`/api/v1/audit/entries${qs ? `?${qs}` : ''}`);
	return { entries: data.entries || [], truncated: !!data.truncated, scanned: data.scanned || 0 };
}

/** Download one raw daily log file (JSON lines). */
export async function downloadAuditFile(date: string): Promise<void> {
	const response = await fetch(`/api/v1/audit/files/${encodeURIComponent(date)}`, {
		headers: { Authorization: `Bearer ${pb.authStore.token}` }
	});
	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new Error(data.message || `HTTP ${response.status}`);
	}
	const blob = await response.blob();
	const url = URL.createObjectURL(blob);
	const link = document.createElement('a');
	link.href = url;
	link.download = `diarum-audit-${date}.log`;
	document.body.appendChild(link);
	link.click();
	link.remove();
	setTimeout(() => URL.revokeObjectURL(url), 1000);
}
