import { pb } from './client';
import type { ExportStats, ImportStats } from './exportImport';

export interface BackupS3Config {
	bucket: string;
	region: string;
	endpoint: string;
	access_key: string;
	secret: string;
	force_path_style: boolean;
	prefix: string;
}

export interface BackupSettings {
	enabled: boolean;
	s3: BackupS3Config;
	auto_enabled: boolean;
	schedule: string;
	timezone: string;
	keep: number;
}

export interface BackupSettingsResponse extends BackupSettings {
	next_run?: string;
	server_timezone: string;
}

export type BackupStatus = 'success' | 'failed' | 'missing';

export interface BackupEntry {
	id: string;
	created_at: string;
	status: BackupStatus;
	trigger?: 'manual' | 'scheduled';
	size: number;
	has_archive: boolean;
	has_log: boolean;
	duration_ms: number;
	error?: string;
	diaries: number;
	media: number;
	conversations: number;
}

export interface BackupLogEntry {
	time: string;
	level: 'info' | 'warn' | 'error';
	message: string;
}

export interface BackupLog {
	id: string;
	trigger: string;
	status: 'success' | 'failed';
	started_at: string;
	finished_at: string;
	duration_ms: number;
	archive?: string;
	size: number;
	sha256?: string;
	app_version?: string;
	stats?: ExportStats;
	error?: string;
	entries: BackupLogEntry[];
}

export interface BackupJob {
	kind: 'backup' | 'restore';
	backup_id: string;
	trigger?: string;
	status: 'running' | 'success' | 'failed';
	stage: string;
	started_at: string;
	finished_at?: string;
	error?: string;
	import_stats?: ImportStats;
}

export interface SchedulePreview {
	valid: boolean;
	error?: string;
	timezone?: string;
	next_runs: string[];
}

export const DEFAULT_BACKUP_KEEP = 3;
export const MIN_BACKUP_KEEP = 1;
export const MAX_BACKUP_KEEP = 100;

export const defaultBackupSettings: BackupSettings = {
	enabled: false,
	s3: { bucket: '', region: '', endpoint: '', access_key: '', secret: '', force_path_style: false, prefix: 'diarum-backups' },
	auto_enabled: false,
	schedule: '0 2 * * *',
	timezone: '',
	keep: DEFAULT_BACKUP_KEEP
};

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
	const headers: Record<string, string> = { Authorization: `Bearer ${pb.authStore.token}` };
	if (init.body) headers['Content-Type'] = 'application/json';
	const response = await fetch(`/api/v1/backup${path}`, { ...init, headers });
	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new Error((data as { message?: string }).message || `Request failed (${response.status})`);
	}
	return (await response.json()) as T;
}

export function getBackupSettings(): Promise<BackupSettingsResponse> {
	return request('/settings');
}

export async function saveBackupSettings(settings: BackupSettings): Promise<BackupSettingsResponse> {
	const result = await request<{ settings: BackupSettingsResponse }>('/settings', {
		method: 'PUT',
		body: JSON.stringify(settings)
	});
	return result.settings;
}

export function testBackupDestination(s3: BackupS3Config): Promise<{ success: boolean; message: string }> {
	return request('/test', { method: 'POST', body: JSON.stringify(s3) });
}

export function previewSchedule(schedule: string, timezone: string): Promise<SchedulePreview> {
	return request('/schedule/preview', { method: 'POST', body: JSON.stringify({ schedule, timezone }) });
}

export function getBackupStatus(): Promise<{ job: BackupJob | null; next_run?: string }> {
	return request('/status');
}

export async function listBackups(): Promise<BackupEntry[]> {
	const result = await request<{ backups: BackupEntry[] }>('/backups');
	return result.backups;
}

export async function startBackup(): Promise<BackupJob> {
	const result = await request<{ job: BackupJob }>('/backups', { method: 'POST' });
	return result.job;
}

export function getBackupLog(id: string): Promise<BackupLog> {
	return request(`/backups/${encodeURIComponent(id)}/log`);
}

export async function deleteBackup(id: string): Promise<void> {
	await request(`/backups/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export async function restoreBackup(id: string): Promise<BackupJob> {
	const result = await request<{ job: BackupJob }>(`/backups/${encodeURIComponent(id)}/restore`, { method: 'POST' });
	return result.job;
}

/** Download a backup archive through the server and save it. */
export async function downloadBackup(id: string): Promise<void> {
	const response = await fetch(`/api/v1/backup/backups/${encodeURIComponent(id)}/download`, {
		headers: { Authorization: `Bearer ${pb.authStore.token}` }
	});
	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new Error((data as { message?: string }).message || 'Download failed');
	}
	const url = URL.createObjectURL(await response.blob());
	const a = document.createElement('a');
	a.href = url;
	a.download = `diarum-backup-${id}.zip`;
	document.body.appendChild(a);
	a.click();
	document.body.removeChild(a);
	URL.revokeObjectURL(url);
}
