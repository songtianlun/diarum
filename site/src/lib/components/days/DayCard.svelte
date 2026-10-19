<script lang="ts">
	import type { Day, DayTag } from '$lib/api/days';
	import TagChip from './TagChip.svelte';
	import { countDay, spanBetween, weekday, formatCount } from '$lib/utils/days';
	import { getToday } from '$lib/utils/date';
	import { t, locale } from '$lib/i18n';

	/**
	 * A day as a plain card: a title band, the count, and the date. The card
	 * is also the element exported as an image, so it carries its own
	 * background and draws everything it shows.
	 */
	let { day, tags = [], element = $bindable() }: { day: Day; tags?: DayTag[]; element?: HTMLDivElement } = $props();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');
	let today = getToday();
	let count = $derived(countDay(day, today));
	let span = $derived(spanBetween(today, count.target));
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
	let numberSize = $derived(digits.length > 6 ? 'text-6xl' : digits.length > 4 ? 'text-7xl' : 'text-8xl');
</script>

<div bind:this={element} class="rounded-2xl overflow-hidden bg-card border border-border/60 shadow-sm">
	<div class="px-5 py-3 text-center text-sm font-medium {count.direction === 'future' ? 'bg-primary text-primary-foreground' : 'bg-secondary text-secondary-foreground'}">
		<span class="inline-flex items-center gap-1.5 max-w-full">
			{#if day.emoji}<span>{day.emoji}</span>{/if}
			<span class="truncate">{title}</span>
		</span>
	</div>

	<div class="px-6 pt-9 pb-7 text-center">
		<div class="flex items-baseline justify-center gap-2 text-foreground">
			{#if count.direction === 'today'}
				<span class="text-6xl font-semibold tracking-tight">{$t('days.todayBig')}</span>
			{:else}
				<span class="{numberSize} font-semibold tracking-tight tabular-nums leading-none">{digits}</span>
				<span class="text-lg text-muted-foreground">{count.days === 1 ? $t('days.unitOne') : $t('days.unit')}</span>
			{/if}
		</div>
		{#if spanText}
			<div class="mt-3 text-xs text-muted-foreground">≈ {spanText}</div>
		{/if}

		<div class="mt-7 pt-4 border-t border-border/60 text-xs text-muted-foreground tabular-nums">
			{count.direction === 'past' ? $t('days.startDate') : $t('days.targetDate')}
			<span class="ml-1.5 text-foreground/80">{count.target}</span>
			<span class="ml-1">{weekday(count.target, intl)}</span>
			{#if day.repeat}<span class="ml-2 px-1.5 py-0.5 rounded bg-muted">↻ {$t(`days.repeats.${day.repeat}`)}</span>{/if}
		</div>
		{#if tags.length}
			<div class="mt-3 flex flex-wrap justify-center gap-1.5">
				{#each tags as tag (tag.id)}<TagChip {tag} />{/each}
			</div>
		{/if}
		{#if day.note}
			<p class="mt-3 text-sm text-muted-foreground leading-relaxed whitespace-pre-line break-words">{day.note}</p>
		{/if}
	</div>
</div>
