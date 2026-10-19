<script lang="ts" module>
	export interface GanttItem {
		id: string;
		name: string;
		date: string;
		origin?: boolean;
		/** The day's end milestone. */
		end?: boolean;
	}
</script>

<script lang="ts">
	import { diffDays, timeDomain, axisTicks } from '$lib/utils/days';
	import { locale, t } from '$lib/i18n';

	/**
	 * The day and all its milestones on one shared track. The track is solid
	 * from the earliest moment up to today and dashed for what is still to
	 * come; every moment sits on it as a marker, with its label stacked above
	 * or below in as few lanes as it takes for none to overlap. A dashed line
	 * marks today.
	 */
	let {
		items,
		today,
		onselect
	}: { items: GanttItem[]; today: string; onselect?: (item: GanttItem) => void } = $props();

	const LABEL_H = 38;
	const LANE_H = 46;
	const STUB = 14;
	const TRACK_H = 28;
	const AXIS_H = 28;
	const GAP = 8;
	const MAX_W = 176;

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');
	let origin = $derived(items.find((item) => item.origin) ?? items[0]);
	let sorted = $derived([...items].sort((a, b) => a.date.localeCompare(b.date) || (a.origin ? -1 : b.origin ? 1 : 0)));

	let domain = $derived(timeDomain([...items.map((item) => item.date), today]));
	let ticks = $derived(axisTicks(domain, intl));
	let endItem = $derived(items.find((item) => item.end));

	let width = $state(0);
	let w = $derived(width || 640);

	function pos(date: string): number {
		return Math.min(100, Math.max(0, (diffDays(domain.start, date) / domain.total) * 100));
	}

	function offsetLabel(date: string): string {
		const n = diffDays(origin.date, date);
		if (n === 0) return $t('days.milestones.offsetSame');
		return n > 0
			? $t('days.milestones.offsetAfter', { n: n.toLocaleString(intl) })
			: $t('days.milestones.offsetBefore', { n: (-n).toLocaleString(intl) });
	}

	function sub(item: GanttItem): string {
		return item.origin ? item.date : `${item.date} · ${offsetLabel(item.date)}`;
	}

	/** A rough text width, enough to keep labels apart without measuring the DOM. */
	function textWidth(text: string, size: number): number {
		let total = 0;
		for (const ch of text) total += /[ᄀ-￿]/.test(ch) ? size : size * 0.6;
		return total;
	}

	type Side = 'up' | 'down';
	interface Placed {
		item: GanttItem;
		x: number;
		left: number;
		width: number;
		side: Side;
		lane: number;
		passed: boolean;
	}

	let placed: Placed[] = $derived.by(() => {
		const result: Placed[] = [];
		let prefer: Side = 'up';
		for (const item of sorted) {
			const x = (pos(item.date) / 100) * w;
			const nameW = textWidth(item.name, 12) + (item.origin ? 14 : 0) + (item.end ? 14 : 0);
			const width = Math.round(Math.min(MAX_W, Math.max(64, nameW, textWidth(sub(item), 11.5))) + 18);
			const left = Math.min(Math.max(x - width / 2, 0), Math.max(w - width, 0));
			const right = left + width;

			const fits = (side: Side, lane: number) =>
				result.every((b) => {
					if (b.side !== side) return true;
					// Same lane: the boxes must not touch.
					if (b.lane === lane) return right + GAP <= b.left || left >= b.left + b.width + GAP;
					// Leaders run through the lanes nearer the track; keep them clear.
					if (b.lane < lane) return x < b.left - 4 || x > b.left + b.width + 4;
					return b.x < left - 4 || b.x > right + 4;
				});
			const laneFor = (side: Side) => {
				const top = Math.max(-1, ...result.filter((b) => b.side === side).map((b) => b.lane));
				for (let lane = 0; lane <= top; lane++) if (fits(side, lane)) return lane;
				return top + 1;
			};

			const other: Side = prefer === 'up' ? 'down' : 'up';
			const a = laneFor(prefer);
			const b = laneFor(other);
			const side: Side = b < a ? other : prefer;
			result.push({ item, x, left, width, side, lane: Math.min(a, b), passed: diffDays(item.date, today) >= 0 });
			prefer = side === 'up' ? 'down' : 'up';
		}
		return result;
	});

	let upLanes = $derived(Math.max(1, ...placed.filter((p) => p.side === 'up').map((p) => p.lane + 1)));
	let downLanes = $derived(Math.max(0, ...placed.filter((p) => p.side === 'down').map((p) => p.lane + 1)));
	let trackTop = $derived(upLanes * LANE_H + STUB);
	let trackY = $derived(trackTop + TRACK_H / 2);
	let axisTop = $derived(trackTop + TRACK_H + (downLanes ? downLanes * LANE_H + STUB : 4));
	let height = $derived(axisTop + AXIS_H);

	function labelTop(p: Placed): number {
		return p.side === 'up'
			? trackTop - STUB - (p.lane + 1) * LANE_H + (LANE_H - LABEL_H)
			: trackTop + TRACK_H + STUB + p.lane * LANE_H;
	}

	// The track: solid over what has passed, dashed over what is to come.
	let first = $derived(sorted[0]?.date ?? today);
	let last = $derived(sorted[sorted.length - 1]?.date ?? today);
	let stop = $derived(endItem && diffDays(endItem.date, today) > 0 ? endItem.date : today);
	let doneTo = $derived(diffDays(stop, first) > 0 ? first : diffDays(last, stop) > 0 ? last : stop);
	let todayPos = $derived(pos(today));

	let hover = $state('');

	function select(item: GanttItem) {
		if (!item.origin) onselect?.(item);
	}
</script>

<div class="overflow-x-auto overscroll-x-contain -mx-1 px-1 pb-1">
	<div class="min-w-[560px] px-2">
		<div class="relative" style="height: {height}px" bind:clientWidth={width}>
			<!-- Grid -->
			{#each ticks as tick (tick.date)}
				<span class="absolute top-0 w-px bg-border/40" style="left: {pos(tick.date)}%; height: {axisTop}px" aria-hidden="true"></span>
			{/each}

			<!-- After the end: no longer counting -->
			{#if endItem}
				<span
					class="absolute right-0 rounded-r-full bg-[repeating-linear-gradient(135deg,transparent_0_5px,hsl(var(--foreground)_/_0.12)_5px_7px)]"
					style="left: {pos(endItem.date)}%; top: {trackY - 5}px; height: 10px"
					aria-hidden="true"
				></span>
			{/if}

			<!-- Track -->
			<span class="absolute left-0 right-0 h-px bg-border" style="top: {trackY}px" aria-hidden="true"></span>
			{#if diffDays(first, doneTo) > 0}
				<span
					class="absolute h-2.5 rounded-full bg-gradient-to-r from-amber-500/60 to-primary/70"
					style="left: {pos(first)}%; width: {pos(doneTo) - pos(first)}%; top: {trackY - 5}px"
					aria-hidden="true"
				></span>
			{/if}
			{#if diffDays(doneTo, last) > 0}
				<span
					class="absolute h-2.5 rounded-full border border-dashed border-primary/50 bg-primary/10"
					style="left: {pos(doneTo)}%; width: {pos(last) - pos(doneTo)}%; top: {trackY - 5}px"
					aria-hidden="true"
				></span>
			{/if}

			<!-- Today -->
			<span class="absolute border-l-2 border-dashed border-amber-500/70 z-10 pointer-events-none" style="left: {todayPos}%; top: 0; height: {axisTop}px" aria-hidden="true"></span>

			<!-- Leaders -->
			{#each placed as p (p.item.id)}
				{@const top = p.side === 'up' ? labelTop(p) + LABEL_H : trackY + 8}
				{@const bottom = p.side === 'up' ? trackY - 8 : labelTop(p)}
				<span
					class="absolute w-px transition-colors {hover === p.item.id ? 'bg-primary' : p.item.origin ? 'bg-amber-500/60' : 'bg-border'}"
					style="left: {p.x}px; top: {top}px; height: {Math.max(bottom - top, 0)}px"
					aria-hidden="true"
				></span>
			{/each}

			<!-- Labels -->
			{#each placed as p (p.item.id)}
				{@const clickable = !!onselect && !p.item.origin}
				<button
					type="button"
					disabled={!clickable}
					onclick={() => select(p.item)}
					onmouseenter={() => (hover = p.item.id)}
					onmouseleave={() => (hover = '')}
					onfocus={() => (hover = p.item.id)}
					onblur={() => (hover = '')}
					class="absolute z-20 flex flex-col justify-center px-2 rounded-lg border text-left shadow-sm transition-colors {p.item.origin
						? 'bg-amber-50 dark:bg-amber-950/40 border-amber-500/40'
						: hover === p.item.id
							? 'bg-card border-primary/50'
							: p.passed
								? 'bg-card border-border/70'
								: 'bg-card border-dashed border-primary/40'} {clickable ? 'cursor-pointer' : 'cursor-default'}"
					style="left: {p.left}px; top: {labelTop(p)}px; width: {p.width}px; height: {LABEL_H}px"
					title="{p.item.name} · {p.item.date}"
				>
					<span class="flex items-center gap-1 min-w-0 text-xs leading-4 text-foreground {p.item.origin ? 'font-semibold' : 'font-medium'}">
						{#if p.item.origin}<span class="text-amber-500 text-[10px]">◆</span>{/if}
						{#if p.item.end}<span class="flex-shrink-0 w-2 h-2 rounded-[2px] bg-foreground/60"></span>{/if}
						<span class="truncate">{p.item.name}</span>
					</span>
					<span class="text-[10.5px] leading-4 tabular-nums truncate {p.passed ? 'text-muted-foreground' : 'text-amber-700 dark:text-amber-300'}">{sub(p.item)}</span>
				</button>
			{/each}

			<!-- Markers -->
			{#each placed as p (p.item.id)}
				{@const big = hover === p.item.id}
				<span
					class="absolute z-20 -translate-x-1/2 -translate-y-1/2 ring-[3px] ring-card shadow-sm transition-transform pointer-events-none {big ? 'scale-125' : ''} {p.item.origin
						? 'w-4 h-4 rotate-45 rounded-[3px] bg-amber-500'
						: p.item.end
							? `w-3.5 h-3.5 rounded-[3px] ${p.passed ? 'bg-foreground/70' : 'bg-card border-2 border-foreground/60'}`
							: `w-3 h-3 rounded-full ${p.passed ? 'bg-primary' : 'bg-card border-2 border-primary'}`}"
					style="left: {p.x}px; top: {trackY}px"
					aria-hidden="true"
				></span>
			{/each}

			<!-- Axis -->
			<div class="absolute left-0 right-0 border-t border-border/70" style="top: {axisTop}px; height: {AXIS_H}px">
				{#each ticks as tick (tick.date)}
					{#if Math.abs(((pos(tick.date) - todayPos) / 100) * w) > 36}
						<span class="absolute top-1.5 -translate-x-1/2 text-[10px] text-muted-foreground tabular-nums whitespace-nowrap" style="left: {pos(tick.date)}%">{tick.label}</span>
					{/if}
				{/each}
				<span
					class="absolute top-1 {todayPos > 94 ? '-translate-x-full' : todayPos < 6 ? '' : '-translate-x-1/2'} px-1.5 py-px rounded bg-amber-600 text-white text-[10px] leading-4 whitespace-nowrap shadow-sm z-10"
					style="left: {todayPos}%"
				>{$t('days.milestones.today')}</span>
			</div>
		</div>

		<!-- Legend -->
		<div class="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[11px] text-muted-foreground">
			<span class="inline-flex items-center gap-1.5"><span class="w-2.5 h-2.5 rotate-45 rounded-[2px] bg-amber-500"></span>{$t('days.milestones.origin')}</span>
			<span class="inline-flex items-center gap-1.5"><span class="w-2.5 h-2.5 rounded-full bg-primary"></span>{$t('days.milestones.passed')}</span>
			<span class="inline-flex items-center gap-1.5"><span class="w-2.5 h-2.5 rounded-full border-2 border-primary"></span>{$t('days.milestones.upcoming')}</span>
			<span class="inline-flex items-center gap-1.5"><span class="w-2.5 h-2.5 rounded-[2px] bg-foreground/70"></span>{$t('days.milestones.endLabel')}</span>
		</div>
	</div>
</div>
