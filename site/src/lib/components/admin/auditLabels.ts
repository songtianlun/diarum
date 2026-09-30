import type { AuditEntry } from '$lib/api/admin';

/** Visual tone of an entry, used for its badge and icon. */
export type Tone = 'create' | 'edit' | 'danger' | 'read' | 'auth' | 'warn' | 'admin' | 'neutral';

export const TONE_CLASS: Record<Tone, string> = {
	create: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 ring-emerald-500/20',
	edit: 'bg-sky-500/10 text-sky-700 dark:text-sky-300 ring-sky-500/20',
	danger: 'bg-rose-500/10 text-rose-700 dark:text-rose-300 ring-rose-500/20',
	read: 'bg-slate-500/10 text-slate-600 dark:text-slate-300 ring-slate-500/20',
	auth: 'bg-violet-500/10 text-violet-700 dark:text-violet-300 ring-violet-500/20',
	warn: 'bg-amber-500/10 text-amber-700 dark:text-amber-300 ring-amber-500/20',
	admin: 'bg-fuchsia-500/10 text-fuchsia-700 dark:text-fuchsia-300 ring-fuchsia-500/20',
	neutral: 'bg-muted text-muted-foreground ring-border/60'
};

/** Categories offered as a filter; ids are comma-joined action prefixes. */
export const CATEGORIES: { id: string; key: string }[] = [
	{ id: '', key: 'admin.audit.categories.all' },
	{ id: 'diary.create,diary.update,diary.delete,diary.restore', key: 'admin.audit.categories.edits' },
	{ id: 'diary.view,diary.search', key: 'admin.audit.categories.reads' },
	{ id: 'auth', key: 'admin.audit.categories.auth' },
	{ id: 'auth.login_failed,auth.denied,auth.forbidden', key: 'admin.audit.categories.security' },
	{ id: 'media', key: 'admin.audit.categories.media' },
	{ id: 'data', key: 'admin.audit.categories.data' },
	{ id: 'settings,token,conversation', key: 'admin.audit.categories.settings' },
	{ id: 'admin', key: 'admin.audit.categories.admin' },
	{ id: '-', key: 'admin.audit.categories.other' }
];

export const SOURCES = ['web', 'api', 'mcp', 'memos', 'cli', 'system'];
export const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'];
export const STATUSES = ['2xx', '3xx', '4xx', '5xx', 'error'];

export function toneOf(entry: Pick<AuditEntry, 'action' | 'status'>): Tone {
	const action = entry.action || '';
	if (action === 'auth.login_failed' || action === 'auth.denied' || action === 'auth.forbidden') return 'warn';
	if ((entry.status ?? 0) >= 500) return 'danger';
	if (action.startsWith('admin.')) return 'admin';
	if (action.startsWith('auth.')) return 'auth';
	if (action.endsWith('.delete') || action === 'media.purge') return 'danger';
	if (action === 'media.trash') return 'warn';
	if (action.endsWith('.create') || action.endsWith('.upload') || action === 'data.import') return 'create';
	if (action.endsWith('.update') || action.endsWith('.restore') || action.startsWith('settings') || action.startsWith('token')) return 'edit';
	if (action.endsWith('.view') || action.endsWith('.search') || action === 'data.export') return 'read';
	if ((entry.status ?? 0) >= 400) return 'warn';
	return 'neutral';
}

/** i18n key for an action's label; unknown actions show their raw name. */
export function actionKey(action: string): string {
	return `admin.audit.actions.${action.replace(/\./g, '_')}`;
}

export function statusTone(status?: number): string {
	if (!status) return 'text-muted-foreground';
	if (status >= 500) return 'text-rose-600 dark:text-rose-400';
	if (status >= 400) return 'text-amber-600 dark:text-amber-400';
	if (status >= 300) return 'text-sky-600 dark:text-sky-400';
	return 'text-emerald-600 dark:text-emerald-400';
}

export function isDate(value: string | undefined): value is string {
	return !!value && /^\d{4}-\d{2}-\d{2}$/.test(value);
}
