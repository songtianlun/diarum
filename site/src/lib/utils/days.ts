import type { Day, DayRepeat } from '$lib/api/days';
import { getToday } from './date';

const MS_PER_DAY = 86_400_000;

interface Ymd {
	y: number;
	m: number; // 1-12
	d: number;
}

function parse(date: string): Ymd {
	const [y, m, d] = date.split('-').map(Number);
	return { y, m, d };
}

function format({ y, m, d }: Ymd): string {
	return `${String(y).padStart(4, '0')}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`;
}

function daysInMonth(y: number, m: number): number {
	return new Date(Date.UTC(y, m, 0)).getUTCDate();
}

/** Calendar days from a to b (both YYYY-MM-DD); positive when b is later. */
export function diffDays(a: string, b: string): number {
	const pa = parse(a);
	const pb = parse(b);
	return Math.round((Date.UTC(pb.y, pb.m - 1, pb.d) - Date.UTC(pa.y, pa.m - 1, pa.d)) / MS_PER_DAY);
}

export function addDays(date: string, n: number): string {
	const p = parse(date);
	const t = new Date(Date.UTC(p.y, p.m - 1, p.d) + n * MS_PER_DAY);
	return format({ y: t.getUTCFullYear(), m: t.getUTCMonth() + 1, d: t.getUTCDate() });
}

/** Adds whole months, keeping the base day where the month allows it (Jan 31 → Feb 28). */
function addMonths(base: Ymd, n: number): string {
	const total = base.y * 12 + (base.m - 1) + n;
	const y = Math.floor(total / 12);
	const m = (total % 12) + 1;
	return format({ y, m, d: Math.min(base.d, daysInMonth(y, m)) });
}

/** The next time a repeating day comes round, on or after today. */
export function nextOccurrence(date: string, repeat: DayRepeat, today = getToday()): string {
	if (!repeat || diffDays(date, today) <= 0) return date;
	const base = parse(date);
	if (repeat === 'week') {
		return addDays(date, Math.ceil(diffDays(date, today) / 7) * 7);
	}
	const step = repeat === 'year' ? 12 : 1;
	const t = parse(today);
	let n = Math.max(0, Math.floor(((t.y - base.y) * 12 + (t.m - base.m)) / step) - 1);
	let candidate = addMonths(base, n * step);
	while (diffDays(candidate, today) > 0) {
		n += 1;
		candidate = addMonths(base, n * step);
	}
	return candidate;
}

export type DayDirection = 'future' | 'past' | 'today';

export interface DayCount {
	/** The date being counted to or from (a repeating day's next occurrence). */
	target: string;
	/** Whole days, never negative. */
	days: number;
	direction: DayDirection;
	/** The date counted from: today, or the end date once it has passed. */
	ref: string;
	/** True once the day's end date has been reached and the count stopped. */
	ended: boolean;
}

/**
 * How far a day is from today, the way Days Matter counts it: days left until
 * a future (or repeating) day, days since a past one. With include_start the
 * start day itself counts, so a day that began today is "day 1".
 *
 * A day with an end date counts normally until then; from the end date on,
 * every count is taken as of the end date and stays there.
 */
export function countDay(
	day: Pick<Day, 'date' | 'repeat' | 'include_start'> & { end?: string },
	today = getToday()
): DayCount {
	const ended = !!day.end && diffDays(day.end, today) >= 0;
	const ref = ended ? day.end! : today;
	const target = nextOccurrence(day.date, day.repeat, ref);
	const diff = diffDays(ref, target);
	if (diff > 0) return { target, days: diff, direction: 'future', ref, ended };
	if (diff === 0 && !day.include_start) return { target, days: 0, direction: 'today', ref, ended };
	return { target, days: -diff + (day.include_start ? 1 : 0), direction: 'past', ref, ended };
}

export interface Span {
	years: number;
	months: number;
	days: number;
}

/** The calendar span between two dates as years, months and days. */
export function spanBetween(a: string, b: string): Span {
	let from = parse(a);
	let to = parse(b);
	if (diffDays(a, b) < 0) [from, to] = [to, from];
	let years = to.y - from.y;
	let months = to.m - from.m;
	let days = to.d - from.d;
	if (days < 0) {
		months -= 1;
		const pm = to.m === 1 ? 12 : to.m - 1;
		const py = to.m === 1 ? to.y - 1 : to.y;
		days += daysInMonth(py, pm);
	}
	if (months < 0) {
		years -= 1;
		months += 12;
	}
	return { years, months, days };
}

/** Days sorted the way a countdown list reads best: upcoming first, nearest on top, then past ones, most recent on top. */
export function sortDays<T extends Pick<Day, 'date' | 'repeat' | 'include_start'> & { end?: string }>(days: T[], today = getToday()): T[] {
	const keyed = days.map((day) => ({ day, count: countDay(day, today) }));
	// Running days first (today, then upcoming, then counting up), stopped ones last.
	const rank = (c: DayCount) => (c.ended ? 3 : c.direction === 'today' ? 0 : c.direction === 'future' ? 1 : 2);
	keyed.sort((a, b) => rank(a.count) - rank(b.count) || a.count.days - b.count.days);
	return keyed.map((k) => k.day);
}

export function weekday(date: string, locale: string): string {
	const p = parse(date);
	return new Date(p.y, p.m - 1, p.d).toLocaleDateString(locale, { weekday: 'long' });
}

export function formatCount(n: number, locale: string): string {
	return n.toLocaleString(locale);
}

/** Muted tag colours that sit well on the app's warm palette. */
export const TAG_COLORS = ['#b07a4f', '#c9a227', '#7f9a65', '#5f8f8b', '#8a7fa8', '#c47a7a', '#9c8f80', '#6b5d52'];

/** The colour a new tag gets: the first one not in use yet, cycling after that. */
export function nextTagColor(used: string[]): string {
	return TAG_COLORS.find((c) => !used.includes(c)) ?? TAG_COLORS[used.length % TAG_COLORS.length];
}

export interface TimeDomain {
	start: string;
	end: string;
	/** Days from start to end. */
	total: number;
}

/** A chart's time range: the given dates plus a little padding either side. */
export function timeDomain(dates: string[]): TimeDomain {
	const sorted = [...dates].sort();
	const first = sorted[0];
	const last = sorted[sorted.length - 1];
	const span = Math.max(diffDays(first, last), 1);
	const pad = Math.max(2, Math.round(span * 0.05));
	const start = addDays(first, -pad);
	const end = addDays(last, pad);
	return { start, end, total: diffDays(start, end) };
}

export interface AxisTick {
	date: string;
	label: string;
}

/**
 * Evenly spaced labels across the domain: days, months or years depending on
 * the span. The span is how many days are visible at once (the whole domain
 * unless the chart is zoomed in), and about eight labels fit in it.
 */
export function axisTicks({ start, end, total }: TimeDomain, locale: string, span = total): AxisTick[] {
	const result: AxisTick[] = [];
	if (span <= 90) {
		const step = [1, 2, 7, 14, 28].find((s) => span / s <= 8) ?? 28;
		for (let d = 0; d <= total; d += step) {
			const date = addDays(start, d);
			const [, m, day] = date.split('-').map(Number);
			result.push({ date, label: `${m}/${day}` });
		}
		return result;
	}
	const months = span / 30.44;
	const step = [1, 2, 3, 6, 12, 24, 60, 120].find((s) => months / s <= 7) ?? 120;
	let [y, m] = start.split('-').map(Number);
	const advance = (n: number) => {
		m += n;
		while (m > 12) {
			m -= 12;
			y += 1;
		}
	};
	// First month boundary after the start that falls on the step.
	advance(1);
	while ((y * 12 + m - 1) % step !== 0) advance(1);
	for (;;) {
		const date = format({ y, m, d: 1 });
		if (diffDays(date, end) < 0) break;
		const label = step >= 12 || m === 1 ? String(y) : new Date(y, m - 1, 1).toLocaleDateString(locale, { month: 'short' });
		result.push({ date, label });
		advance(step);
	}
	return result;
}
