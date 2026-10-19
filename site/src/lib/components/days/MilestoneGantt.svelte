<script lang="ts" module>
	export interface GanttItem {
		id: string;
		name: string;
		date: string;
		origin?: boolean;
	}
</script>

<script lang="ts">
	import { diffDays, addDays } from '$lib/utils/days';
	import { locale, t } from '$lib/i18n';

	/**
	 * Milestones on a time axis. Every bar starts at the day itself (the first
	 * row) and runs to its milestone, so the chart reads as "how far along"
	 * each moment is; a dashed line marks today.
	 */
	let {
		items,
		today,
		onselect
	}: { items: GanttItem[]; today: string; onselect?: (item: GanttItem) => void } = $props();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');
	let origin = $derived(items.find((item) => item.origin) ?? items[0]);

	let domain = $derived.by(() => {
		const dates = [...items.map((item) => item.date), today].sort();
		const first = dates[0];
		const last = dates[dates.length - 1];
		const span = Math.max(diffDays(first, last), 1);
		const pad = Math.max(2, Math.round(span * 0.05));
		const start = addDays(first, -pad);
		const end = addDays(last, pad);
		return { start, end, total: diffDays(start, end) };
	});

	function pos(date: string): number {
		return (diffDays(domain.start, date) / domain.total) * 100;
	}

	interface Tick {
		date: string;
		label: string;
	}

	let ticks = $derived.by((): Tick[] => {
		const { start, end, total } = domain;
		const result: Tick[] = [];
		if (total <= 90) {
			const step = [1, 2, 7, 14, 28].find((s) => total / s <= 8) ?? 28;
			for (let d = 0; d <= total; d += step) {
				const date = addDays(start, d);
				const [, m, day] = date.split('-').map(Number);
				result.push({ date, label: `${m}/${day}` });
			}
			return result;
		}
		const months = total / 30.44;
		const step = [1, 2, 3, 6, 12, 24, 60, 120].find((s) => months / s <= 7) ?? 120;
		let [y, m] = start.split('-').map(Number);
		// First month boundary after the start that falls on the step.
		m += 1;
		if (m > 12) { m = 1; y += 1; }
		while (((y * 12 + m - 1) % step) !== 0) {
			m += 1;
			if (m > 12) { m = 1; y += 1; }
		}
		for (;;) {
			const date = `${String(y).padStart(4, '0')}-${String(m).padStart(2, '0')}-01`;
			if (diffDays(date, end) < 0) break;
			const label = step >= 12 || m === 1
				? String(y)
				: new Date(y, m - 1, 1).toLocaleDateString(intl, { month: 'short' });
			result.push({ date, label });
			m += step;
			while (m > 12) { m -= 12; y += 1; }
		}
		return result;
	});

	function offsetLabel(date: string): string {
		const n = diffDays(origin.date, date);
		if (n === 0) return $t('days.milestones.offsetSame');
		return n > 0 ? $t('days.milestones.offsetAfter', { n }) : $t('days.milestones.offsetBefore', { n: -n });
	}
</script>

<div class="overflow-x-auto overscroll-x-contain -mx-1 px-1 pb-1">
	<div class="min-w-[600px]">
		<!-- Axis -->
		<div class="flex">
			<div class="w-36 sm:w-48 flex-shrink-0 sticky left-0 z-20 bg-card"></div>
			<div class="relative flex-1 h-7 border-b border-border/70">
				{#each ticks as tick (tick.date)}
					<span class="absolute bottom-1.5 -translate-x-1/2 text-[10px] text-muted-foreground tabular-nums whitespace-nowrap" style="left: {pos(tick.date)}%">{tick.label}</span>
				{/each}
			</div>
		</div>

		<div class="relative flex">
			<div class="w-36 sm:w-48 flex-shrink-0 sticky left-0 z-20 bg-card">
				{#each items as item (item.id)}
					<button
						type="button"
						onclick={() => onselect?.(item)}
						disabled={!onselect || item.origin}
						class="w-full h-12 pr-3 text-left flex flex-col justify-center border-b border-border/40 enabled:hover:bg-muted/30 rounded-l-md transition-colors"
					>
						<span class="flex items-center gap-1.5 text-sm text-foreground truncate {item.origin ? 'font-semibold' : ''}">
							{#if item.origin}<span class="text-amber-500 text-xs">◆</span>{/if}
							<span class="truncate">{item.name}</span>
						</span>
						<span class="text-[11px] text-muted-foreground tabular-nums truncate">{item.date}{#if !item.origin}<span class="ml-1">· {offsetLabel(item.date)}</span>{/if}</span>
					</button>
				{/each}
			</div>

			<div class="relative flex-1">
				<!-- Grid -->
				{#each ticks as tick (tick.date)}
					<span class="absolute inset-y-0 w-px bg-border/50" style="left: {pos(tick.date)}%" aria-hidden="true"></span>
				{/each}
				<!-- Today -->
				<span class="absolute inset-y-0 border-l-2 border-dashed border-rose-400/80 z-10" style="left: {pos(today)}%" aria-hidden="true">
					<span class="absolute -top-0.5 left-1 px-1.5 py-px rounded bg-rose-500 text-white text-[10px] leading-4 whitespace-nowrap shadow-sm">{$t('days.milestones.today')}</span>
				</span>

				{#each items as item (item.id)}
					{@const at = pos(item.date)}
					{@const from = pos(origin.date)}
					{@const passed = diffDays(item.date, today) >= 0}
					<div class="relative h-12 border-b border-border/40">
						{#if !item.origin}
							<span
								class="absolute top-1/2 -translate-y-1/2 h-2.5 rounded-full {passed ? 'bg-primary/45' : 'bg-primary/15 border border-dashed border-primary/40'}"
								style="left: {Math.min(at, from)}%; width: {Math.max(Math.abs(at - from), 0.4)}%"
							></span>
						{/if}
						<span
							class="absolute top-1/2 w-3.5 h-3.5 -translate-x-1/2 -translate-y-1/2 rotate-45 rounded-[3px] shadow-sm ring-2 ring-card {item.origin ? 'bg-amber-500 w-4 h-4' : passed ? 'bg-primary' : 'bg-card border-2 border-primary'}"
							style="left: {at}%"
							title="{item.name} · {item.date}"
						></span>
					</div>
				{/each}
			</div>
		</div>
	</div>
</div>
