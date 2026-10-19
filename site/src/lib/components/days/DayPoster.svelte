<script lang="ts">
	import type { Day, DayTag, Milestone } from '$lib/api/days';
	import TagChip from './TagChip.svelte';
	import { countDay, diffDays, spanBetween, weekday, formatCount } from '$lib/utils/days';
	import { t, locale } from '$lib/i18n';

	/**
	 * The image a day exports as: a fixed-width poster with the count on top
	 * and the milestones below as a timeline, closed by a small signature.
	 * It is rendered off screen only while an image is being made, so it can
	 * be laid out for the picture rather than for the page.
	 */
	let {
		day,
		tags = [],
		milestones = [],
		today,
		element = $bindable()
	}: { day: Day; tags?: DayTag[]; milestones?: Milestone[]; today: string; element?: HTMLDivElement } = $props();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');
	let count = $derived(countDay(day, today));
	let span = $derived(spanBetween(count.ref, count.target));
	let title = $derived(
		count.direction === 'future'
			? $t('days.until', { name: day.name })
			: count.direction === 'past'
				? $t('days.since', { name: day.name })
				: $t('days.isToday', { name: day.name })
	);
	let spanText = $derived(
		span.years > 0
			? $t('days.span', { y: span.years, m: span.months, d: span.days })
			: span.months > 0
				? $t('days.spanMonths', { m: span.months, d: span.days })
				: ''
	);
	let digits = $derived(formatCount(count.days, intl));
	let numberSize = $derived(digits.length > 6 ? 'text-[64px]' : digits.length > 4 ? 'text-[76px]' : 'text-[92px]');
	let future = $derived(count.direction === 'future');

	let items = $derived([...milestones].sort((a, b) => a.date.localeCompare(b.date) || a.created.localeCompare(b.created)));
	let reached = $derived(items.filter((m) => diffDays(m.date, today) >= 0).length);
	let todayIndex = $derived.by(() => {
		const i = items.findIndex((m) => diffDays(today, m.date) > 0);
		return i === -1 ? items.length : i;
	});

	function signed(n: number): string {
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

	function split(date: string): { md: string; y: string } {
		const [y, m, d] = date.split('-');
		return { md: `${m}.${d}`, y };
	}
</script>

{#snippet todayRow()}
	<div class="flex items-center gap-3 py-1">
		<span class="w-12 flex-shrink-0 text-right text-[10px] font-semibold tracking-wide text-amber-700 dark:text-amber-400 tabular-nums">{split(today).md}</span>
		<span class="relative w-4 flex-shrink-0 flex justify-center self-stretch">
			<span class="absolute inset-y-0 w-px bg-border"></span>
			<span class="relative my-auto w-2 h-2 rounded-full bg-amber-500 ring-[3px] ring-amber-500/20"></span>
		</span>
		<span class="flex-1 border-t border-dashed border-amber-500/50"></span>
		<span class="text-[10px] font-medium text-amber-700 dark:text-amber-400">{$t('days.milestones.today')}</span>
	</div>
{/snippet}

<div
	bind:this={element}
	class="w-[440px] px-5 pt-6 pb-4 text-foreground"
	style="background:
		radial-gradient(120% 60% at 0% 0%, hsl(var(--primary) / 0.14), transparent 60%),
		radial-gradient(90% 50% at 100% 100%, rgb(245 158 11 / 0.14), transparent 60%),
		hsl(var(--background));"
>
	<div class="rounded-[28px] bg-card border border-border/50 shadow-[0_12px_40px_-16px_rgb(0_0_0/0.25)] overflow-hidden">
		<!-- Count -->
		<div class="relative px-7 pt-7 pb-6 text-center">
			<div
				class="absolute inset-x-0 top-0 h-28 {future ? 'bg-gradient-to-b from-amber-500/10' : 'bg-gradient-to-b from-primary/10'} to-transparent"
				aria-hidden="true"
			></div>
			<div class="relative mx-auto w-14 h-14 rounded-2xl bg-card border border-border/60 shadow-sm flex items-center justify-center text-[28px]">{day.emoji || '📅'}</div>
			<div class="relative mt-3 text-[15px] font-semibold tracking-tight break-words">{title}</div>

			<div class="mt-4 flex items-baseline justify-center gap-2">
				{#if count.direction === 'today'}
					<span class="text-[64px] font-semibold tracking-tight leading-none">{$t('days.todayBig')}</span>
				{:else}
					<span class="{numberSize} font-semibold tracking-tighter tabular-nums leading-none {future ? 'text-amber-700 dark:text-amber-300' : 'text-primary'}">{digits}</span>
					<span class="text-lg text-muted-foreground">{count.days === 1 ? $t('days.unitOne') : $t('days.unit')}</span>
				{/if}
			</div>
			{#if spanText}
				<div class="mt-2.5 text-xs text-muted-foreground tracking-wide">≈ {spanText}</div>
			{/if}

			<div class="mt-5 inline-flex items-center gap-2 px-3 py-1.5 rounded-full bg-muted/60 text-[11px] text-muted-foreground tabular-nums">
				<span>{count.direction === 'past' ? $t('days.startDate') : $t('days.targetDate')}</span>
				<span class="font-medium text-foreground/80">{count.target}</span>
				<span>{weekday(count.target, intl)}</span>
				{#if day.repeat}<span class="pl-2 border-l border-border">↻ {$t(`days.repeats.${day.repeat}`)}</span>{/if}
			</div>
			{#if day.end}
				<div class="mt-2 text-[11px] text-muted-foreground">
					■ {count.ended ? $t('days.endedOn', { date: day.end }) : $t('days.endsOn', { date: day.end })}
				</div>
			{/if}
			{#if tags.length}
				<div class="mt-3 flex flex-wrap justify-center gap-1.5">
					{#each tags as tag (tag.id)}<TagChip {tag} />{/each}
				</div>
			{/if}
			{#if day.note}
				<p class="mt-3 text-[13px] text-muted-foreground leading-relaxed whitespace-pre-line break-words">{day.note}</p>
			{/if}
		</div>

		<!-- Milestones -->
		{#if items.length}
			<div class="relative px-5 pt-5 pb-5 border-t border-dashed border-border">
				<!-- Ticket notches -->
				<span class="absolute -left-3 -top-3 w-6 h-6 rounded-full" style="background: hsl(var(--background))" aria-hidden="true"></span>
				<span class="absolute -right-3 -top-3 w-6 h-6 rounded-full" style="background: hsl(var(--background))" aria-hidden="true"></span>

				<div class="flex items-center justify-between gap-3 mb-3.5">
					<div class="flex items-center gap-1.5 text-[13px] font-semibold">
						<svg class="w-3.5 h-3.5 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M4 21V4m0 0h11l-1.5 4L15 12H4" /></svg>
						{$t('days.milestones.title')}
					</div>
					<div class="flex items-center gap-2 text-[10px] text-muted-foreground tabular-nums">
						{$t('days.poster.reached', { done: reached, total: items.length })}
						<span class="w-14 h-1 rounded-full bg-muted overflow-hidden">
							<span class="block h-full rounded-full bg-gradient-to-r from-amber-500 to-primary" style="width: {(reached / items.length) * 100}%"></span>
						</span>
					</div>
				</div>

				<!-- The day itself -->
				<div class="flex gap-3">
					<span class="w-12 flex-shrink-0 text-right tabular-nums leading-tight pt-0.5">
						<span class="block text-[11px] font-semibold text-foreground/80">{split(day.date).md}</span>
						<span class="block text-[9px] text-muted-foreground">{split(day.date).y}</span>
					</span>
					<span class="relative w-4 flex-shrink-0 flex justify-center">
						<span class="absolute top-3 bottom-0 w-px bg-gradient-to-b from-amber-500/70 to-border"></span>
						<span class="relative mt-1 w-3 h-3 rotate-45 rounded-[2px] bg-amber-500 ring-[3px] ring-amber-500/20"></span>
					</span>
					<div class="flex-1 min-w-0 pb-3">
						<div class="flex items-center gap-1.5">
							<span class="text-[13px] font-semibold truncate">{day.name}</span>
							<span class="flex-shrink-0 px-1.5 rounded text-[9px] leading-4 bg-amber-500/15 text-amber-700 dark:text-amber-300">{$t('days.milestones.origin')}</span>
						</div>
						<div class="text-[10px] text-muted-foreground">{weekday(day.date, intl)}</div>
					</div>
				</div>

				{#each items as m, index (m.id)}
					{#if index === todayIndex}{@render todayRow()}{/if}
					{@const passed = diffDays(m.date, today) >= 0}
					{@const last = index === items.length - 1 && todayIndex !== items.length}
					<div class="flex gap-3">
						<span class="w-12 flex-shrink-0 text-right tabular-nums leading-tight pt-2">
							<span class="block text-[11px] font-semibold {passed ? 'text-foreground/80' : 'text-amber-700 dark:text-amber-300'}">{split(m.date).md}</span>
							<span class="block text-[9px] text-muted-foreground">{split(m.date).y}</span>
						</span>
						<span class="relative w-4 flex-shrink-0 flex justify-center">
							<span class="absolute top-0 w-px bg-border {last ? 'h-3' : 'bottom-0'}"></span>
							{#if m.end}
								<span class="relative mt-2.5 w-2.5 h-2.5 rounded-[2px] {passed ? 'bg-foreground/70' : 'bg-card border-[1.5px] border-foreground/50'} ring-[3px] ring-card"></span>
							{:else}
								<span class="relative mt-2.5 w-2.5 h-2.5 rounded-full {passed ? 'bg-primary' : 'bg-card border-[1.5px] border-primary'} ring-[3px] ring-card"></span>
							{/if}
						</span>
						<div class="flex-1 min-w-0 my-1 px-3 py-2 rounded-xl {passed ? 'bg-muted/40' : 'border border-dashed border-amber-500/40 bg-amber-500/[0.04]'}">
							<div class="flex items-start justify-between gap-2">
								<span class="text-[13px] font-medium leading-snug break-words min-w-0">
									{#if m.end}<span class="text-foreground/60">■ </span>{/if}{m.name}
								</span>
								<span class="flex-shrink-0 px-1.5 rounded text-[9.5px] leading-4 bg-primary/10 text-primary tabular-nums">{signed(diffDays(day.date, m.date))}</span>
							</div>
							<div class="mt-0.5 text-[10.5px] tabular-nums {passed ? 'text-muted-foreground' : 'text-amber-700 dark:text-amber-300'}">{relative(m.date)}</div>
							{#if m.note}
								<p class="mt-1 text-[10.5px] text-muted-foreground leading-relaxed whitespace-pre-line break-words line-clamp-2">{m.note}</p>
							{/if}
						</div>
					</div>
				{/each}
				{#if todayIndex === items.length}{@render todayRow()}{/if}
			</div>
		{/if}
	</div>

	<!-- Signature -->
	<div class="mt-3.5 px-2 flex items-center justify-between text-[10px] text-muted-foreground">
		<span class="inline-flex items-center gap-1.5">
			<img src="/logo-64.png" alt="" class="w-4 h-4 rounded" />
			<span class="font-medium tracking-wide">Diarum</span>
		</span>
		<span class="tabular-nums">{today}</span>
	</div>
</div>
