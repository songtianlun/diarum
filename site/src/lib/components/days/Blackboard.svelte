<script lang="ts">
	import type { Day } from '$lib/api/days';
	import { countDay, spanBetween, weekday, formatCount } from '$lib/utils/days';
	import { getToday } from '$lib/utils/date';
	import { t, locale } from '$lib/i18n';

	/**
	 * A day written in chalk on a plain board with a thin wood rim. The rim
	 * is the element that gets exported as an image, so everything it shows
	 * lives inside it and uses fixed colours rather than theme tokens.
	 */
	let { day, element = $bindable() }: { day: Day; element?: HTMLDivElement } = $props();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');
	let today = getToday();
	let count = $derived(countDay(day, today));
	let span = $derived(spanBetween(today, count.target));
	let headline = $derived(
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
	let numberSize = $derived(digits.length > 6 ? 'text-[4.25rem] sm:text-[5.5rem]' : digits.length > 4 ? 'text-[5rem] sm:text-[6.5rem]' : 'text-[5.75rem] sm:text-[7.5rem]');
</script>

<div bind:this={element} class="frame" data-direction={count.direction}>
	<div class="slate">
		<div class="relative flex flex-col items-center text-center px-6 sm:px-10 pt-8 pb-7 sm:pt-10 sm:pb-9">
			<div class="chalk headline text-lg sm:text-xl leading-snug max-w-full break-words">
				{#if day.emoji}<span class="mr-1.5 not-chalk">{day.emoji}</span>{/if}{headline}
			</div>

			<div class="flex items-end justify-center gap-2 mt-3 sm:mt-4">
				{#if count.direction === 'today'}
					<span class="chalk number text-[4.5rem] sm:text-[6rem] leading-none">{$t('days.todayBig')}</span>
				{:else}
					<span class="chalk number {numberSize} leading-none tabular-nums">{digits}</span>
					<span class="chalk unit text-xl sm:text-2xl mb-2 sm:mb-3">{count.days === 1 ? $t('days.unitOne') : $t('days.unit')}</span>
				{/if}
			</div>
			<span class="rule" aria-hidden="true"></span>

			<div class="chalk meta mt-4 text-sm sm:text-base">
				<span class="opacity-70">{count.direction === 'past' ? $t('days.startDate') : $t('days.targetDate')}</span>
				<span class="ml-1.5 tabular-nums">{count.target}</span>
				<span class="ml-1.5">{weekday(count.target, intl)}</span>
			</div>
			{#if spanText}
				<div class="chalk meta-soft mt-1 text-xs sm:text-sm">≈ {spanText}</div>
			{/if}
			{#if day.repeat}
				<div class="chalk tag mt-3 text-xs">↻ {$t(`days.repeats.${day.repeat}`)}</div>
			{/if}
			{#if day.note}
				<div class="chalk note mt-5 text-sm sm:text-[0.95rem] leading-relaxed max-w-md whitespace-pre-line break-words">{day.note}</div>
			{/if}
		</div>
		<div class="brand">Diarum</div>
	</div>
</div>

<style>
	.frame {
		padding: 8px;
		border-radius: 18px;
		background: linear-gradient(160deg, #a87545 0%, #8a5a30 55%, #774b26 100%);
		box-shadow:
			inset 0 1px 0 rgba(255, 228, 190, 0.35),
			0 18px 40px -20px rgba(60, 32, 10, 0.45),
			0 2px 6px rgba(60, 32, 10, 0.12);
	}
	.slate {
		position: relative;
		overflow: hidden;
		border-radius: 11px;
		min-height: 280px;
		background: radial-gradient(130% 100% at 50% 0%, #34463a 0%, #2a392f 60%, #24312a 100%);
		box-shadow: inset 0 1px 8px rgba(0, 0, 0, 0.35);
	}

	.chalk {
		color: rgba(246, 244, 236, 0.92);
		text-shadow: 0 0 1px rgba(255, 255, 255, 0.35);
		font-family: 'Chalkboard SE', 'Chalkduster', 'Segoe Print', 'Bradley Hand', 'Kaiti SC', 'STKaiti', 'KaiTi', 'Comic Sans MS', cursive;
		letter-spacing: 0.02em;
	}
	.not-chalk {
		text-shadow: none;
	}
	.headline {
		color: #f5e3a8;
	}
	[data-direction='past'] .headline {
		color: #f1d2ab;
	}
	.number {
		font-family: 'Chalkduster', 'Chalkboard SE', 'Bradley Hand', Georgia, 'Times New Roman', serif;
		font-weight: 400;
		color: #fbfaf4;
		text-shadow: 0 0 1px rgba(255, 255, 255, 0.5), 0 0 18px rgba(255, 255, 255, 0.08);
	}
	.unit {
		opacity: 0.8;
	}
	.rule {
		display: block;
		width: 56px;
		height: 2px;
		margin-top: 14px;
		border-radius: 2px;
		background: rgba(245, 227, 168, 0.55);
	}
	[data-direction='past'] .rule {
		background: rgba(241, 210, 171, 0.55);
	}
	.meta {
		opacity: 0.85;
	}
	.meta-soft {
		opacity: 0.55;
	}
	.tag {
		padding: 2px 10px;
		border: 1px solid rgba(246, 244, 236, 0.3);
		border-radius: 999px;
		opacity: 0.75;
	}
	.note {
		opacity: 0.75;
	}
	.brand {
		position: absolute;
		right: 14px;
		bottom: 10px;
		font-size: 10px;
		letter-spacing: 0.18em;
		text-transform: uppercase;
		color: rgba(246, 244, 236, 0.25);
	}
</style>
