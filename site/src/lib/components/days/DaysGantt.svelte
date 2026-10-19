<script lang="ts">
	import type { Day, Milestone } from '$lib/api/days';
	import { countDay, diffDays, timeDomain, axisTicks, type DayCount } from '$lib/utils/days';
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
	let ticks = $derived(axisTicks(domain, intl));

	function pos(date: string): number {
		return Math.min(100, Math.max(0, (diffDays(domain.start, date) / domain.total) * 100));
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

{#if rows.length === 0}
	<div class="py-14 text-center text-sm text-muted-foreground">{$t('days.gantt.empty')}</div>
{:else}
	<div class="overflow-x-auto overscroll-x-contain">
		<div class="min-w-[640px]">
			<!-- Axis -->
			<div class="flex">
				<div class="w-32 sm:w-52 flex-shrink-0 sticky left-0 z-20 bg-card"></div>
				<div class="relative flex-1 h-11 mr-4">
					<span class="absolute top-0 {pos(today) > 92 ? '-translate-x-full' : pos(today) < 8 ? '' : '-translate-x-1/2'} px-1.5 py-px rounded bg-amber-600 text-white text-[10px] leading-4 whitespace-nowrap shadow-sm z-10" style="left: {pos(today)}%">{$t('days.milestones.today')}</span>
					{#each ticks as tick (tick.date)}
						<span class="absolute bottom-1.5 -translate-x-1/2 text-[10px] text-muted-foreground tabular-nums whitespace-nowrap" style="left: {pos(tick.date)}%">{tick.label}</span>
					{/each}
				</div>
			</div>

			<div class="relative flex border-t border-border/60">
				<!-- Labels -->
				<div class="w-32 sm:w-52 flex-shrink-0 sticky left-0 z-20 bg-card">
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
				<div class="relative flex-1 mr-4">
					{#each ticks as tick (tick.date)}
						<span class="absolute inset-y-0 w-px bg-border/50" style="left: {pos(tick.date)}%" aria-hidden="true"></span>
					{/each}
					<span class="absolute inset-y-0 border-l-2 border-dashed border-amber-500/70 z-10 pointer-events-none" style="left: {pos(today)}%" aria-hidden="true"></span>

					{#each rows as row (row.day.id)}
						{@const left = pos(row.from)}
						{@const width = Math.max(pos(row.to) - left, 0.6)}
						<a href="/days/{row.day.id}" class="group relative block h-14 border-b border-border/40 hover:bg-muted/30 transition-colors" title="{row.day.name} · {countLabel(row.count)}">
							{#if row.kind === 'up'}
								<span
									class="absolute top-1/2 -translate-y-1/2 h-3 rounded-full {row.count.ended ? 'bg-muted-foreground/30' : 'bg-primary/55 group-hover:bg-primary/70'} transition-colors"
									style="left: {left}%; width: {width}%"
								></span>
							{:else}
								<span
									class="absolute top-1/2 -translate-y-1/2 h-3 rounded-full border border-dashed {row.count.ended ? 'border-muted-foreground/40 bg-muted-foreground/10' : 'border-amber-500/70 bg-amber-500/15 group-hover:bg-amber-500/25'} transition-colors"
									style="left: {left}%; width: {width}%"
								></span>
								<!-- Target -->
								<span
									class="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 w-3 h-3 rounded-full ring-2 ring-card {row.count.ended ? 'bg-muted-foreground/50' : 'bg-amber-500'}"
									style="left: {pos(row.to)}%"
								></span>
							{/if}
							{#if row.origin}
								<span
									class="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 w-3 h-3 rotate-45 rounded-[2px] ring-2 ring-card {row.count.ended ? 'bg-muted-foreground/60' : 'bg-primary'}"
									style="left: {pos(row.origin)}%"
								></span>
							{/if}
							{#each row.marks as m (m.id)}
								<span
									class="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 w-2.5 h-2.5 rotate-45 rounded-[2px] ring-2 ring-card {diffDays(m.date, today) >= 0 ? 'bg-amber-600' : 'bg-card border-2 border-amber-600'}"
									style="left: {pos(m.date)}%"
									title="{m.name} · {m.date}"
								></span>
							{/each}
							{#if row.day.end}
								<span
									class="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 w-3.5 h-3.5 rounded-[3px] ring-2 ring-card {row.count.ended ? 'bg-foreground/70' : 'bg-card border-2 border-foreground/60'}"
									style="left: {pos(row.day.end)}%"
									title="{$t('days.gantt.legendEnd')} · {row.day.end}"
								></span>
							{/if}
						</a>
					{/each}
				</div>
			</div>
		</div>
	</div>

	<!-- Legend -->
	<div class="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[11px] text-muted-foreground">
		<span class="inline-flex items-center gap-1.5"><span class="w-5 h-2 rounded-full bg-primary/55"></span>{$t('days.gantt.legendRunning')}</span>
		<span class="inline-flex items-center gap-1.5"><span class="w-5 h-2 rounded-full border border-dashed border-amber-500/70 bg-amber-500/15"></span>{$t('days.gantt.legendUpcoming')}</span>
		<span class="inline-flex items-center gap-1.5"><span class="w-5 h-2 rounded-full bg-muted-foreground/30"></span>{$t('days.gantt.legendStopped')}</span>
		<span class="inline-flex items-center gap-1.5"><span class="w-2 h-2 rotate-45 rounded-[1px] bg-amber-600"></span>{$t('days.gantt.legendMilestone')}</span>
		<span class="inline-flex items-center gap-1.5"><span class="w-2.5 h-2.5 rounded-[2px] bg-foreground/70"></span>{$t('days.gantt.legendEnd')}</span>
	</div>
{/if}
