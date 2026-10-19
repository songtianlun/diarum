<script lang="ts" module>
	export interface GanttItem {
		id: string;
		name: string;
		date: string;
		note?: string;
		origin?: boolean;
		/** The day's end milestone. */
		end?: boolean;
	}
</script>

<script lang="ts">
	import { countDay, diffDays, timeDomain, axisTicks, weekday } from '$lib/utils/days';
	import GanttTip, { type TipContent } from './GanttTip.svelte';
	import GanttViewport, { type OverviewRow } from './GanttViewport.svelte';
	import { locale, t } from '$lib/i18n';
	import { untrack } from 'svelte';

	/**
	 * The day and its milestones, one row each in date order. Every bar runs
	 * from the moment before it to its own, so the rows chain into one
	 * journey: solid where it has passed, dashed where it is still to come.
	 * Beside each marker sit the time since the previous moment and the
	 * count from today; hovering (or tapping) a marker shows its details.
	 */
	let {
		items,
		today,
		includeStart = false,
		onselect
	}: { items: GanttItem[]; today: string; includeStart?: boolean; onselect?: (item: GanttItem) => void } = $props();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');
	let origin = $derived(items.find((item) => item.origin) ?? items[0]);
	let endItem = $derived(items.find((item) => item.end));

	interface Row {
		item: GanttItem;
		prev: GanttItem | null;
		passed: boolean;
	}

	let rows: Row[] = $derived.by(() => {
		const sorted = [...items].sort((a, b) => a.date.localeCompare(b.date) || (a.origin ? -1 : b.origin ? 1 : 0));
		return sorted.map((item, i) => ({ item, prev: i > 0 ? sorted[i - 1] : null, passed: diffDays(item.date, today) >= 0 }));
	});

	let domain = $derived(timeDomain([...items.map((item) => item.date), today]));
	// The visible window, in days from the domain start; the slider moves it.
	let from = $state(0);
	let to = $state(0);
	let span = $derived(to - from || domain.total);
	let ticks = $derived(axisTicks(domain, intl, span).filter((tick) => pos(tick.date) >= 0 && pos(tick.date) <= 100));
	let todayPos = $derived(pos(today));

	let overview: OverviewRow[] = $derived(
		rows.map((row) => ({
			from: diffDays(domain.start, row.prev?.date ?? row.item.date),
			to: diffDays(domain.start, row.item.date),
			tone: row.passed ? 'up' : 'down',
			marks: [diffDays(domain.start, row.item.date)]
		}))
	);

	/** Where a date falls across the visible window, in percent; outside it runs past 0 or 100. */
	function pos(date: string): number {
		return ((diffDays(domain.start, date) - from) / span) * 100;
	}

	function later(a: string, b: string): string {
		return diffDays(a, b) > 0 ? b : a;
	}

	function earlier(a: string, b: string): string {
		return diffDays(a, b) > 0 ? a : b;
	}

	function signed(n: number): string {
		if (n === 0) return $t('days.milestones.offsetSame');
		return n > 0
			? $t('days.milestones.offsetAfter', { n: n.toLocaleString(intl) })
			: $t('days.milestones.offsetBefore', { n: (-n).toLocaleString(intl) });
	}

	/** Counting up from a past moment, down to a future one; the day itself counts the way its board does. */
	function relative(item: GanttItem): string {
		if (item.origin) {
			const count = countDay({ date: item.date, repeat: '', include_start: includeStart, end: endItem?.date }, today);
			const n = count.days.toLocaleString(intl);
			const label =
				count.direction === 'today'
					? $t('days.todayShort')
					: count.direction === 'future'
						? $t('days.inDays', { n })
						: $t('days.agoDays', { n });
			return count.ended ? `${label} · ${$t('days.endedShort')}` : label;
		}
		const n = diffDays(today, item.date);
		if (n === 0) return $t('days.todayShort');
		return n > 0 ? $t('days.inDays', { n: n.toLocaleString(intl) }) : $t('days.agoDays', { n: (-n).toLocaleString(intl) });
	}

	function describe(row: Row): TipContent {
		const { item, prev } = row;
		const kind = item.origin ? $t('days.milestones.origin') : item.end ? $t('days.gantt.legendEnd') : $t('days.gantt.legendMilestone');
		const lines: [string, string][] = [[$t('days.gantt.tip.date'), `${item.date} ${weekday(item.date, intl)}`]];
		if (prev) lines.push([$t('days.gantt.tip.fromPrev'), `${signed(diffDays(prev.date, item.date))} · ${prev.name}`]);
		if (!item.origin) lines.push([$t('days.gantt.tip.fromDay'), signed(diffDays(origin.date, item.date))]);
		lines.push([$t('days.gantt.tip.fromToday'), relative(item)]);
		return {
			title: item.name,
			kind,
			kindClass: item.origin
				? 'bg-amber-500/15 text-amber-700 dark:text-amber-300'
				: item.end
					? 'bg-foreground/10 text-foreground/80'
					: 'bg-primary/10 text-primary',
			rows: lines,
			note: item.note,
			action: onselect && !item.origin ? () => select(item) : undefined,
			linkLabel: $t('days.gantt.tip.edit')
		};
	}

	// The marker whose details are showing; pinned by a click or tap.
	let tip = $state<{ key: string; anchor: DOMRect; content: TipContent; pinned: boolean } | null>(null);

	function show(e: Event, row: Row, pin: boolean) {
		if (!pin && tip?.pinned) return;
		const anchor = (e.currentTarget as HTMLElement).getBoundingClientRect();
		tip = { key: row.item.id, anchor, content: describe(row), pinned: pin };
	}

	// A card left behind would point at a marker that has moved.
	$effect(() => {
		void from;
		void to;
		untrack(() => (tip = null));
	});

	function hide(key: string) {
		if (tip?.key === key && !tip.pinned) tip = null;
	}

	// Hovering shows the card on desktop; a click or tap pins it, and a second
	// click on the same marker closes it again.
	function toggle(e: MouseEvent, row: Row) {
		if (tip?.key === row.item.id && tip.pinned) tip = null;
		else show(e, row, true);
	}

	function select(item: GanttItem) {
		tip = null;
		if (!item.origin) onselect?.(item);
	}
</script>

{#if tip}
	<GanttTip anchor={tip.anchor} content={tip.content} pinned={tip.pinned} onclose={() => (tip = null)} />
{/if}

<GanttViewport
	start={domain.start}
	total={domain.total}
	today={diffDays(domain.start, today)}
	{overview}
	storageKey="diarum_milestone_gantt_range"
	gutter={12}
	labelClass="[--gantt-label:8rem] sm:[--gantt-label:12rem]"
	bind:from
	bind:to
>
	<!-- Axis -->
	<div class="flex">
		<div class="w-[var(--gantt-label)] flex-shrink-0 sticky left-0 z-20 bg-card"></div>
		<div class="relative flex-1 h-10 mr-3 overflow-hidden">
			{#if todayPos >= 0 && todayPos <= 100}<span
				class="absolute top-0 {todayPos > 92 ? '-translate-x-full' : todayPos < 8 ? '' : '-translate-x-1/2'} px-1.5 py-px rounded bg-amber-600 text-white text-[10px] leading-4 whitespace-nowrap shadow-sm z-10"
				style="left: {todayPos}%"
			>{$t('days.milestones.today')}</span>{/if}
			{#each ticks as tick (tick.date)}
				<span class="absolute bottom-1.5 {pos(tick.date) < 4 ? '' : pos(tick.date) > 96 ? '-translate-x-full' : '-translate-x-1/2'} text-[10px] text-muted-foreground tabular-nums whitespace-nowrap" style="left: {pos(tick.date)}%">{tick.label}</span>
			{/each}
		</div>
	</div>

	<div class="relative flex border-t border-border/60">
		<!-- Labels -->
		<div class="w-[var(--gantt-label)] flex-shrink-0 sticky left-0 z-20 bg-card">
			{#each rows as row (row.item.id)}
				{@const item = row.item}
				<button
					type="button"
					onclick={() => select(item)}
					disabled={!onselect || item.origin}
					class="w-full h-14 pl-1 pr-3 text-left flex flex-col justify-center border-b border-border/40 enabled:hover:bg-muted/40 transition-colors"
				>
					<span class="flex items-center gap-1.5 min-w-0 text-sm text-foreground {item.origin ? 'font-semibold' : ''}">
						{#if item.origin}<span class="text-amber-500 text-xs">◆</span>{/if}
						<span class="truncate">{item.name}</span>
						{#if item.end}<span class="flex-shrink-0 px-1 rounded text-[10px] bg-foreground/10 text-foreground/80">■ {$t('days.milestones.endLabel')}</span>{/if}
					</span>
					<span class="text-[11px] text-muted-foreground tabular-nums truncate">{item.date}</span>
				</button>
			{/each}
		</div>

		<!-- Chart -->
		<div data-gantt-track class="relative flex-1 mr-3 overflow-hidden">
			{#each ticks as tick (tick.date)}
				<span class="absolute inset-y-0 w-px bg-border/50" style="left: {pos(tick.date)}%" aria-hidden="true"></span>
			{/each}
			{#if endItem}
				<span class="absolute inset-y-0 right-0 bg-[repeating-linear-gradient(135deg,transparent_0_6px,hsl(var(--foreground)_/_0.05)_6px_8px)]" style="left: {pos(endItem.date)}%" aria-hidden="true"></span>
			{/if}
			<span class="absolute inset-y-0 border-l-2 border-dashed border-amber-500/70 z-10 pointer-events-none" style="left: {todayPos}%" aria-hidden="true"></span>

			{#each rows as row (row.item.id)}
				{@const item = row.item}
				{@const at = pos(item.date)}
				{@const flip = at > 62}
				{@const active = tip?.key === item.id}
				<div class="relative h-14 border-b border-border/40 transition-colors {active ? 'bg-muted/30' : ''}">
					{#if row.prev}
						{@const from = row.prev.date}
						<!-- Link down from the previous marker -->
						<span class="absolute w-px border-l border-dotted border-muted-foreground/40" style="left: {pos(from)}%; top: -50%; height: 100%" aria-hidden="true"></span>
						<!-- Passed part of the stretch -->
						{#if diffDays(from, today) > 0}
							<span
								class="absolute top-1/2 -translate-y-1/2 h-3 rounded-full bg-primary/50"
								style="left: {pos(from)}%; width: {Math.max(pos(earlier(item.date, today)) - pos(from), 0.5)}%"
							></span>
						{/if}
						<!-- Part still to come -->
						{#if diffDays(today, item.date) > 0}
							<span
								class="absolute top-1/2 -translate-y-1/2 h-3 rounded-full border border-dashed border-amber-500/70 bg-amber-500/15"
								style="left: {pos(later(from, today))}%; width: {Math.max(pos(item.date) - pos(later(from, today)), 0.5)}%"
							></span>
						{/if}
					{/if}

					<!-- Counts beside the marker; near the right edge they sit above the bar instead -->
					<span
						class="absolute flex items-center gap-1 whitespace-nowrap pointer-events-none text-[10.5px] tabular-nums {flip ? 'top-1 -translate-x-full pr-1' : 'top-1/2 -translate-y-1/2 pl-4'}"
						style="left: {at}%"
					>
						{#if row.prev}
							<span class="px-1.5 py-px rounded bg-card/90 border border-primary/20 text-primary">{signed(diffDays(row.prev.date, item.date))}</span>
						{/if}
						<span class="px-1.5 py-px rounded border {row.passed ? 'bg-card/90 border-border text-muted-foreground' : 'bg-amber-50 dark:bg-amber-950/60 border-amber-500/30 text-amber-700 dark:text-amber-300'}">{relative(item)}</span>
					</span>

					<button
						type="button"
						data-gantt-marker
						class="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 w-7 h-7 z-10 flex items-center justify-center rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
						style="left: {at}%"
						aria-label="{item.name} · {item.date} · {relative(item)}"
						aria-expanded={active}
						onmouseenter={(e) => show(e, row, false)}
						onmouseleave={() => hide(item.id)}
						onfocus={(e) => show(e, row, false)}
						onblur={() => hide(item.id)}
						onclick={(e) => toggle(e, row)}
					>
						<span
							class="ring-[3px] ring-card shadow-sm transition-transform {active ? 'scale-125' : ''} {item.origin
								? 'w-4 h-4 rotate-45 rounded-[3px] bg-amber-500'
								: item.end
									? `w-3.5 h-3.5 rounded-[3px] ${row.passed ? 'bg-foreground/70' : 'bg-card border-2 border-foreground/60'}`
									: `w-3 h-3 rounded-full ${row.passed ? 'bg-primary' : 'bg-card border-2 border-primary'}`}"
						></span>
					</button>
				</div>
			{/each}
		</div>
	</div>
</GanttViewport>

<!-- Legend -->
<div class="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[11px] text-muted-foreground">
	<span class="inline-flex items-center gap-1.5"><span class="w-2.5 h-2.5 rotate-45 rounded-[2px] bg-amber-500"></span>{$t('days.milestones.origin')}</span>
	<span class="inline-flex items-center gap-1.5"><span class="w-5 h-2 rounded-full bg-primary/50"></span>{$t('days.milestones.passed')}</span>
	<span class="inline-flex items-center gap-1.5"><span class="w-5 h-2 rounded-full border border-dashed border-amber-500/70 bg-amber-500/15"></span>{$t('days.milestones.upcoming')}</span>
	<span class="inline-flex items-center gap-1.5"><span class="px-1 rounded border border-primary/20 text-primary text-[10px]">+n</span>{$t('days.gantt.tip.fromPrev')}</span>
	<span class="inline-flex items-center gap-1.5"><span class="w-2.5 h-2.5 rounded-[2px] bg-foreground/70"></span>{$t('days.milestones.endLabel')}</span>
</div>
