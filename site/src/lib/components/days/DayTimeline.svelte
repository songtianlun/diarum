<script lang="ts">
	import type { Day, Milestone } from '$lib/api/days';
	import { diffDays, weekday } from '$lib/utils/days';
	import { t, locale } from '$lib/i18n';

	/**
	 * A day's milestones (beyond the day itself) as a vertical timeline, part
	 * of the exported image. On wider screens the date gets its own column
	 * to the left of the rail; on phones it sits under the name.
	 */
	let { day, milestones, today }: { day: Day; milestones: Milestone[]; today: string } = $props();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');
	let items = $derived([...milestones].sort((a, b) => a.date.localeCompare(b.date) || a.created.localeCompare(b.created)));
	let passedCount = $derived(items.filter((m) => diffDays(m.date, today) >= 0).length);
	// Where the "today" divider goes: before the first milestone still to come.
	let todayIndex = $derived.by(() => {
		const i = items.findIndex((m) => diffDays(today, m.date) > 0);
		return i === -1 ? items.length : i;
	});

	function offset(date: string): string {
		const n = diffDays(day.date, date);
		if (n === 0) return $t('days.milestones.offsetSame');
		return n > 0
			? $t('days.milestones.offsetAfter', { n: n.toLocaleString(intl) })
			: $t('days.milestones.offsetBefore', { n: (-n).toLocaleString(intl) });
	}

	function relative(date: string): string {
		const n = diffDays(today, date);
		if (n === 0) return $t('days.todayShort');
		return n > 0 ? $t('days.inDays', { n: n.toLocaleString(intl) }) : $t('days.agoDays', { n: (-n).toLocaleString(intl) });
	}

	function shortDate(date: string): { md: string; y: string } {
		const [y, m, d] = date.split('-');
		return { md: `${m}-${d}`, y };
	}
</script>

{#snippet todayRow(last: boolean)}
	<li class="relative flex items-center gap-3 sm:gap-4 py-1.5" aria-label={$t('days.milestones.today')}>
		<span class="hidden sm:block w-20 flex-shrink-0 text-right text-[11px] font-medium text-amber-700 dark:text-amber-400 tabular-nums">{today}</span>
		<span class="relative w-5 flex-shrink-0 flex justify-center">
			<span class="absolute -top-2 w-px bg-border {last ? 'h-1/2' : '-bottom-2'}"></span>
			<span class="relative w-2.5 h-2.5 rounded-full bg-amber-600 ring-4 ring-amber-500/20"></span>
		</span>
		<span class="flex-1 border-t border-dashed border-amber-500/50"></span>
		<span class="text-[11px] font-medium text-amber-700 dark:text-amber-400">{$t('days.milestones.today')}<span class="sm:hidden tabular-nums">&nbsp;· {today}</span></span>
	</li>
{/snippet}

<div class="rounded-2xl bg-card border border-border/60 shadow-sm overflow-hidden">
	<div class="px-5 py-3 flex items-center justify-between gap-3 border-b border-border/60 bg-muted/30">
		<!-- The title opens the milestone page -->
		<a href="/days/{day.id}/milestones" class="group -ml-1.5 px-1.5 py-0.5 rounded-lg flex items-center gap-2 text-sm font-medium text-foreground hover:bg-muted/70 transition-colors">
			<svg class="w-4 h-4 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 21V4m0 0h11l-1.5 4L15 12H4" /></svg>
			{$t('days.milestones.title')}
			<span class="px-1.5 rounded-md bg-primary/10 text-primary text-xs tabular-nums">{items.length}</span>
			<svg class="w-3.5 h-3.5 text-muted-foreground group-hover:text-foreground group-hover:translate-x-0.5 transition-transform" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
		</a>
		<div class="flex items-center gap-2 text-[11px] text-muted-foreground tabular-nums">
			<span>{passedCount}/{items.length}</span>
			<span class="w-16 h-1.5 rounded-full bg-muted overflow-hidden">
				<span class="block h-full rounded-full bg-gradient-to-r from-amber-500 to-primary" style="width: {(passedCount / Math.max(items.length, 1)) * 100}%"></span>
			</span>
		</div>
	</div>

	<ol class="px-4 sm:px-5 py-4">
		<!-- The day itself starts the line -->
		<li class="relative flex items-start gap-3 sm:gap-4">
			<span class="hidden sm:block w-20 flex-shrink-0 pt-0.5 text-right tabular-nums">
				<span class="block text-xs font-medium text-foreground/80">{shortDate(day.date).md}</span>
				<span class="block text-[10px] text-muted-foreground">{shortDate(day.date).y}</span>
			</span>
			<span class="relative w-5 flex-shrink-0 self-stretch flex justify-center">
				<span class="absolute top-3 bottom-0 w-px bg-gradient-to-b from-amber-500/60 to-border"></span>
				<span class="relative mt-1 w-3.5 h-3.5 rotate-45 rounded-[3px] bg-amber-500 ring-4 ring-amber-500/15"></span>
			</span>
			<div class="flex-1 min-w-0 pb-4">
				<div class="flex items-center gap-2 flex-wrap">
					<span class="text-sm font-semibold text-foreground break-words">{day.emoji ? `${day.emoji} ` : ''}{day.name}</span>
					<span class="px-1.5 py-px rounded text-[10px] bg-amber-500/15 text-amber-700 dark:text-amber-300">{$t('days.milestones.origin')}</span>
				</div>
				<div class="mt-0.5 text-xs text-muted-foreground tabular-nums">
					<span class="sm:hidden">{day.date} ·&nbsp;</span>{weekday(day.date, intl)}
				</div>
			</div>
		</li>

		{#each items as m, index (m.id)}
			{#if index === todayIndex}{@render todayRow(false)}{/if}
			{@const passed = diffDays(m.date, today) >= 0}
			{@const lastRow = index === items.length - 1 && todayIndex !== items.length}
			<li class="relative flex items-start gap-3 sm:gap-4">
				<span class="hidden sm:block w-20 flex-shrink-0 pt-2.5 text-right tabular-nums">
					<span class="block text-xs font-medium {passed ? 'text-foreground/80' : 'text-amber-700 dark:text-amber-300'}">{shortDate(m.date).md}</span>
					<span class="block text-[10px] text-muted-foreground">{shortDate(m.date).y}</span>
				</span>
				<span class="relative w-5 flex-shrink-0 self-stretch flex justify-center">
					<span class="absolute top-0 w-px bg-border {lastRow ? 'h-4' : 'bottom-0'}"></span>
					{#if m.end}
						<span class="relative mt-3 w-3 h-3 rounded-[3px] {passed ? 'bg-foreground/70' : 'bg-card border-2 border-foreground/50'} ring-4 ring-card"></span>
					{:else}
						<span class="relative mt-3 w-3 h-3 rounded-full {passed ? 'bg-primary' : 'bg-card border-2 border-primary/70'} ring-4 ring-card"></span>
					{/if}
				</span>
				<a
					href="/days/{day.id}/milestones#m-{m.id}"
					class="flex-1 min-w-0 my-1 mb-2 px-3 py-2 rounded-xl border transition-colors {passed
						? 'bg-muted/30 border-transparent hover:border-border'
						: 'bg-card border-dashed border-primary/30 hover:border-primary/60'}"
				>
					<div class="flex items-start justify-between gap-2">
						<div class="flex items-center gap-1.5 flex-wrap min-w-0">
							<span class="text-sm font-medium text-foreground break-words">{m.name}</span>
							{#if m.end}
								<span class="px-1.5 py-px rounded text-[10px] bg-foreground/10 text-foreground/80">■ {$t('days.milestones.endLabel')}</span>
							{/if}
						</div>
						<span class="flex-shrink-0 px-1.5 py-px rounded text-[10px] bg-primary/10 text-primary tabular-nums">{offset(m.date)}</span>
					</div>
					<div class="mt-0.5 text-xs text-muted-foreground tabular-nums">
						<span class="sm:hidden">{m.date} ·&nbsp;</span>{weekday(m.date, intl)} · <span class={passed ? '' : 'text-amber-700 dark:text-amber-300'}>{relative(m.date)}</span>
					</div>
					{#if m.note}
						<p class="mt-1 text-xs text-muted-foreground leading-relaxed whitespace-pre-line break-words line-clamp-3">{m.note}</p>
					{/if}
				</a>
			</li>
		{/each}
		{#if todayIndex === items.length}{@render todayRow(true)}{/if}
	</ol>
</div>
