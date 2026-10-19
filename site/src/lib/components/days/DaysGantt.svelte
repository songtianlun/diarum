<script lang="ts">
	import type { Day, Milestone } from '$lib/api/days';
	import { countDay, diffDays, timeDomain, axisTicks, weekday, type DayCount } from '$lib/utils/days';
	import GanttTip, { type TipContent } from './GanttTip.svelte';
	import GanttViewport from './GanttViewport.svelte';
	import { goto } from '$app/navigation';
	import { t, locale } from '$lib/i18n';

	/**
	 * Every day on one time axis. A day counting up is a solid bar from its
	 * start to today (or to its end, once stopped); a day counting down is a
	 * dashed bar from today to its target. Milestones sit on the row as
	 * diamonds, the end as a square, and a dashed line marks today.
	 */
	let { days, milestones, today }: { days: Day[]; milestones: Milestone[]; today: string } = $props();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');

	interface Row {
		day: Day;
		count: DayCount;
		/** Where the bar starts and ends. */
		from: string;
		to: string;
		kind: 'up' | 'down';
		/** The day's own date, drawn when it is on the axis (not for repeating days). */
		origin: string | null;
		marks: Milestone[];
	}

	let endByDay = $derived(new Map(milestones.filter((m) => m.end).map((m) => [m.day, m])));

	let byDay = $derived(
		milestones.reduce((map, m) => {
			if (!m.end) map.set(m.day, [...(map.get(m.day) ?? []), m]);
			return map;
		}, new Map<string, Milestone[]>())
	);

	let rows: Row[] = $derived(
		days
			.map((day) => {
				const count = countDay(day, today);
				const up = count.direction !== 'future';
				return {
					day,
					count,
					from: up ? count.target : count.ref,
					to: up ? count.ref : count.target,
					kind: up ? 'up' : 'down',
					origin: day.repeat ? null : day.date,
					marks: byDay.get(day.id) ?? []
				} satisfies Row;
			})
			.sort((a, b) => a.from.localeCompare(b.from) || a.day.name.localeCompare(b.day.name))
	);

	let domain = $derived(
		timeDomain([
			today,
			...rows.flatMap((row) => [row.from, row.to, ...(row.origin ? [row.origin] : []), ...row.marks.map((m) => m.date), ...(row.day.end ? [row.day.end] : [])])
		])
	);
	// Days visible at once; narrower when a range is picked.
	let span = $state(0);
	let ticks = $derived(axisTicks(domain, intl, span || domain.total));

	function pos(date: string): number {
		return Math.min(100, Math.max(0, (diffDays(domain.start, date) / domain.total) * 100));
	}

	// The marker whose details are showing; pinned by a click or tap.
	let tip = $state<{ key: string; anchor: DOMRect; content: TipContent; pinned: boolean } | null>(null);

	function relative(date: string): string {
		const n = diffDays(today, date);
		if (n === 0) return $t('days.todayShort');
		return n > 0 ? $t('days.inDays', { n: n.toLocaleString(intl) }) : $t('days.agoDays', { n: (-n).toLocaleString(intl) });
	}

	function offset(day: Day, date: string): string {
		const n = diffDays(day.date, date);
		if (n === 0) return $t('days.milestones.offsetSame');
		return n > 0 ? $t('days.milestones.offsetAfter', { n: n.toLocaleString(intl) }) : $t('days.milestones.offsetBefore', { n: (-n).toLocaleString(intl) });
	}

	type Marker = 'start' | 'target' | 'milestone' | 'end';

	function describe(row: Row, kind: Marker, m?: Milestone): TipContent {
		const date = kind === 'start' ? row.day.date : kind === 'target' ? row.to : (m?.date ?? row.day.end);
		const dateRow: [string, string] = [$t('days.gantt.tip.date'), `${date} ${weekday(date, intl)}`];
		const base = { href: markerHref(row, kind, m), linkLabel: kind === 'milestone' ? $t('days.gantt.tip.openMilestone') : $t('days.gantt.tip.open') };
		if (kind === 'start' || kind === 'target') {
			return {
				...base,
				title: row.day.name,
				kind: kind === 'start' ? $t('days.startDate') : $t('days.targetDate'),
				kindClass: kind === 'start' ? 'bg-primary/10 text-primary' : 'bg-amber-500/15 text-amber-700 dark:text-amber-300',
				rows: [dateRow, [$t('days.gantt.tip.fromToday'), relative(date)], [$t('days.gantt.tip.count'), countLabel(row.count)]],
				note: row.day.note
			};
		}
		return {
			...base,
			title: m?.name ?? $t('days.gantt.legendEnd'),
			kind: kind === 'end' ? $t('days.gantt.legendEnd') : $t('days.gantt.legendMilestone'),
			kindClass: kind === 'end' ? 'bg-foreground/10 text-foreground/80' : 'bg-amber-500/15 text-amber-700 dark:text-amber-300',
			rows: [
				dateRow,
				[$t('days.gantt.tip.fromDay'), offset(row.day, date)],
				[$t('days.gantt.tip.fromToday'), relative(date)],
				[$t('days.gantt.tip.day'), `${row.day.emoji || ''} ${row.day.name}`.trim()]
			],
			note: m?.note
		};
	}

	/** The day's ends (start, target, end) open the day; milestones open their spot on the milestone page. */
	function markerHref(row: Row, kind: Marker, m?: Milestone): string {
		return kind === 'milestone' && m ? `/days/${row.day.id}/milestones#m-${m.id}` : `/days/${row.day.id}`;
	}

	function show(e: Event, key: string, content: () => TipContent, pin: boolean) {
		if (!pin && tip?.pinned) return;
		const anchor = (e.currentTarget as HTMLElement).getBoundingClientRect();
		tip = { key, anchor, content: content(), pinned: pin };
	}

	// How the last press happened: a mouse (or keyboard) click goes straight
	// to the marker's page, since hovering already showed the card; on touch
	// the first tap pins the card and a second tap on the marker goes there.
	let pointerType = 'mouse';

	function activate(e: MouseEvent, row: Row, kind: Marker, key: string, m?: Milestone) {
		const href = markerHref(row, kind, m);
		if (pointerType !== 'touch' && pointerType !== 'pen') {
			goto(href);
		} else if (tip?.key === key && tip.pinned) {
			goto(href);
		} else {
			show(e, key, () => describe(row, kind, m), true);
		}
	}

	function hide(key: string) {
		if (tip?.key === key && !tip.pinned) tip = null;
	}

	function countLabel(count: DayCount): string {
		const n = count.days.toLocaleString(intl);
		const text =
			count.direction === 'today'
				? $t('days.todayShort')
				: count.direction === 'future'
					? $t('days.inDays', { n })
					: $t('days.agoDays', { n });
		return count.ended ? `${$t('days.endedShort')} · ${text}` : text;
	}
</script>

{#snippet marker(row: Row, kind: Marker, key: string, at: number, shape: string, m?: Milestone)}
	<button
		type="button"
		data-gantt-marker
		class="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 w-6 h-6 z-10 flex items-center justify-center rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
		style="left: {at}%"
		aria-label={m?.name ?? row.day.name}
		aria-expanded={tip?.key === key}
		onmouseenter={(e) => show(e, key, () => describe(row, kind, m), false)}
		onmouseleave={() => hide(key)}
		onfocus={(e) => show(e, key, () => describe(row, kind, m), false)}
		onblur={() => hide(key)}
		onpointerdown={(e) => (pointerType = e.pointerType)}
		onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && (pointerType = 'keyboard')}
		onclick={(e) => activate(e, row, kind, key, m)}
	>
		<span class="ring-2 ring-card transition-transform {tip?.key === key ? 'scale-125' : ''} {shape}"></span>
	</button>
{/snippet}

{#if tip}
	<GanttTip anchor={tip.anchor} content={tip.content} pinned={tip.pinned} onclose={() => (tip = null)} />
{/if}

{#if rows.length === 0}
	<div class="py-14 text-center text-sm text-muted-foreground">{$t('days.gantt.empty')}</div>
{:else}
	<GanttViewport
		total={domain.total}
		focus={pos(today)}
		storageKey="diarum_days_gantt_range"
		minWidth={640}
		gutter={16}
		labelClass="[--gantt-label:8rem] sm:[--gantt-label:13rem]"
		bind:span
	>
		<!-- Axis -->
		<div class="flex">
			<div class="w-[var(--gantt-label)] flex-shrink-0 sticky left-0 z-20 bg-card"></div>
			<div class="relative flex-1 h-11 mr-4">
				<span class="absolute top-0 {pos(today) > 92 ? '-translate-x-full' : pos(today) < 8 ? '' : '-translate-x-1/2'} px-1.5 py-px rounded bg-amber-600 text-white text-[10px] leading-4 whitespace-nowrap shadow-sm z-10" style="left: {pos(today)}%">{$t('days.milestones.today')}</span>
				{#each ticks as tick (tick.date)}
					<span class="absolute bottom-1.5 -translate-x-1/2 text-[10px] text-muted-foreground tabular-nums whitespace-nowrap" style="left: {pos(tick.date)}%">{tick.label}</span>
				{/each}
			</div>
		</div>

		<div class="relative flex border-t border-border/60">
			<!-- Labels -->
			<div class="w-[var(--gantt-label)] flex-shrink-0 sticky left-0 z-20 bg-card">
				{#each rows as row (row.day.id)}
					<a
						href="/days/{row.day.id}"
						class="h-14 pl-1 pr-2 sm:pr-3 flex items-center gap-2.5 border-b border-border/40 hover:bg-muted/40 transition-colors"
					>
						<span class="hidden sm:flex w-8 h-8 flex-shrink-0 rounded-lg bg-accent/60 items-center justify-center text-base">{row.day.emoji || '📅'}</span>
						<span class="min-w-0">
							<span class="block text-sm text-foreground truncate">{row.day.name}</span>
							<span
								class="block text-[11px] tabular-nums truncate {row.count.ended
									? 'text-muted-foreground'
									: row.kind === 'down'
										? 'text-amber-700 dark:text-amber-300'
										: 'text-primary'}"
							>{countLabel(row.count)}</span>
						</span>
					</a>
				{/each}
			</div>

			<!-- Chart -->
			<div data-gantt-track class="relative flex-1 mr-4">
				{#each ticks as tick (tick.date)}
					<span class="absolute inset-y-0 w-px bg-border/50" style="left: {pos(tick.date)}%" aria-hidden="true"></span>
				{/each}
				<span class="absolute inset-y-0 border-l-2 border-dashed border-amber-500/70 z-10 pointer-events-none" style="left: {pos(today)}%" aria-hidden="true"></span>

				{#each rows as row (row.day.id)}
					{@const left = pos(row.from)}
					{@const width = Math.max(pos(row.to) - left, 0.6)}
					<div class="group relative h-14 border-b border-border/40 hover:bg-muted/30 transition-colors">
						<a href="/days/{row.day.id}" class="absolute inset-0" aria-label="{row.day.name} · {countLabel(row.count)}"></a>
						{#if row.kind === 'up'}
							<span
								class="absolute top-1/2 -translate-y-1/2 h-3 rounded-full pointer-events-none {row.count.ended ? 'bg-muted-foreground/30' : 'bg-primary/55 group-hover:bg-primary/70'} transition-colors"
								style="left: {left}%; width: {width}%"
							></span>
						{:else}
							<span
								class="absolute top-1/2 -translate-y-1/2 h-3 rounded-full pointer-events-none border border-dashed {row.count.ended ? 'border-muted-foreground/40 bg-muted-foreground/10' : 'border-amber-500/70 bg-amber-500/15 group-hover:bg-amber-500/25'} transition-colors"
								style="left: {left}%; width: {width}%"
							></span>
							{@render marker(row, 'target', `${row.day.id}:target`, pos(row.to), `w-3 h-3 rounded-full ${row.count.ended ? 'bg-muted-foreground/50' : 'bg-amber-500'}`)}
						{/if}
						{#if row.origin}
							{@render marker(row, 'start', `${row.day.id}:start`, pos(row.origin), `w-3 h-3 rotate-45 rounded-[2px] ${row.count.ended ? 'bg-muted-foreground/60' : 'bg-primary'}`)}
						{/if}
						{#each row.marks as m (m.id)}
							{@render marker(row, 'milestone', m.id, pos(m.date), `w-2.5 h-2.5 rotate-45 rounded-[2px] ${diffDays(m.date, today) >= 0 ? 'bg-amber-600' : 'bg-card border-2 border-amber-600'}`, m)}
						{/each}
						{#if row.day.end}
							{@render marker(row, 'end', `${row.day.id}:end`, pos(row.day.end), `w-3.5 h-3.5 rounded-[3px] ${row.count.ended ? 'bg-foreground/70' : 'bg-card border-2 border-foreground/60'}`, endByDay.get(row.day.id))}
						{/if}
					</div>
				{/each}
			</div>
		</div>
	</GanttViewport>

	<!-- Legend -->
	<div class="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[11px] text-muted-foreground">
		<span class="inline-flex items-center gap-1.5"><span class="w-5 h-2 rounded-full bg-primary/55"></span>{$t('days.gantt.legendRunning')}</span>
		<span class="inline-flex items-center gap-1.5"><span class="w-5 h-2 rounded-full border border-dashed border-amber-500/70 bg-amber-500/15"></span>{$t('days.gantt.legendUpcoming')}</span>
		<span class="inline-flex items-center gap-1.5"><span class="w-5 h-2 rounded-full bg-muted-foreground/30"></span>{$t('days.gantt.legendStopped')}</span>
		<span class="inline-flex items-center gap-1.5"><span class="w-2 h-2 rotate-45 rounded-[1px] bg-amber-600"></span>{$t('days.gantt.legendMilestone')}</span>
		<span class="inline-flex items-center gap-1.5"><span class="w-2.5 h-2.5 rounded-[2px] bg-foreground/70"></span>{$t('days.gantt.legendEnd')}</span>
	</div>
{/if}
