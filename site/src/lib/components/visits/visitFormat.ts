import { get } from 'svelte/store';
import { getIntlLocale, t } from '$lib/i18n';
import { shortUA } from '$lib/api/admin';
import type { Visit, VisitScope, VisitorStat } from '$lib/api/visits';
import { deviceId } from '$lib/utils/device';

function tr(key: string, params?: Record<string, string | number>): string {
	return get(t)(key, params);
}

export function formatNumber(value: number | undefined): string {
	return (value ?? 0).toLocaleString(getIntlLocale());
}

/** "2026-09-28" as a readable date in the reader's language. */
export function dayLabel(day: string | undefined, withWeekday = false): string {
	if (!day) return tr('visits.unattributed');
	const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day);
	if (!match) return day;
	const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
	return date.toLocaleDateString(getIntlLocale(), withWeekday ? { year: 'numeric', month: 'short', day: 'numeric', weekday: 'short' } : { year: 'numeric', month: 'short', day: 'numeric' });
}

export function timeLabel(value: string | undefined): string {
	if (!value) return '—';
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return '—';
	return date.toLocaleString(getIntlLocale(), { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

/** "3 min ago", "yesterday", … falling back to a date past a week. */
export function relativeTime(value: string | undefined): string {
	if (!value) return '—';
	const date = new Date(value);
	const diff = Date.now() - date.getTime();
	if (Number.isNaN(diff)) return '—';
	const rtf = new Intl.RelativeTimeFormat(getIntlLocale(), { numeric: 'auto' });
	const minutes = Math.round(diff / 60000);
	if (Math.abs(minutes) < 60) return rtf.format(-minutes, 'minute');
	const hours = Math.round(minutes / 60);
	if (Math.abs(hours) < 24) return rtf.format(-hours, 'hour');
	const days = Math.round(hours / 24);
	if (Math.abs(days) < 7) return rtf.format(-days, 'day');
	return date.toLocaleDateString(getIntlLocale(), { year: 'numeric', month: 'short', day: 'numeric' });
}

/** Who a visit or visitor row was. */
export function visitorLabel(row: Pick<Visit, 'visitor_id' | 'visitor' | 'self'> | VisitorStat, scope: VisitScope): string {
	if (row.self) {
		const name = row.visitor || '';
		return scope.kind === 'self' ? tr('visits.you') : `${name || tr('visits.unknownUser')} · ${tr('visits.owner')}`;
	}
	if (!row.visitor_id) return tr('visits.anonymous');
	return row.visitor || `${tr('visits.unknownUser')} (${row.visitor_id.slice(0, 8)})`;
}

/** A short, stable name for a device. */
export function deviceLabel(device: string | undefined): string {
	if (!device) return '';
	if (device === `d:${deviceId()}`) return tr('visits.thisDevice');
	return tr('visits.device', { id: device.replace(/^[dh]:/, '').slice(0, 6) });
}

export function clientLabel(ua: string | undefined): string {
	return shortUA(ua) || '—';
}

export function reasonLabel(reason: string | undefined): string {
	if (!reason) return '';
	const key = `visits.reason.${reason}`;
	const text = tr(key);
	return text === key ? reason : text;
}

export function sourceLabel(source: string | undefined): string {
	if (!source) return '';
	const key = `visits.source.${source}`;
	const text = tr(key);
	return text === key ? source : text;
}

export function kindLabel(kind: string | undefined): string {
	if (!kind) return '';
	const key = `visits.kind.${kind}`;
	const text = tr(key);
	return text === key ? kind : text;
}

export const OK_BADGE = 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 ring-emerald-500/20';
export const FAIL_BADGE = 'bg-rose-500/10 text-rose-700 dark:text-rose-300 ring-rose-500/20';
export const NEUTRAL_BADGE = 'bg-muted text-muted-foreground ring-border/60';
export const PULLED_BADGE = 'bg-sky-500/10 text-sky-700 dark:text-sky-300 ring-sky-500/20';

/** Local midnight of a "YYYY-MM-DD" day as an ISO string, for charts. */
export function dayStartISO(day: string): string {
	const [y, m, d] = day.split('-').map(Number);
	return new Date(y, m - 1, d).toISOString();
}

/** RFC 3339 start of the period `days` back from now (0 = everything). */
export function periodStart(days: number): string | undefined {
	if (!days) return undefined;
	const now = new Date();
	const start = new Date(now.getFullYear(), now.getMonth(), now.getDate() - (days - 1));
	return start.toISOString().replace(/\.\d{3}Z$/, 'Z');
}
