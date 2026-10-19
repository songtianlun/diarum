<script lang="ts">
	import type { Day } from '$lib/api/days';
	import { countDay, spanBetween, weekday, formatCount } from '$lib/utils/days';
	import { getToday } from '$lib/utils/date';
	import { t, locale } from '$lib/i18n';

	/**
	 * A day as a portrait chalkboard card: name on top, the count in the
	 * middle, dates along the bottom. The card is the element exported as an
	 * image, so everything it shows lives inside it and uses fixed colours
	 * rather than theme tokens.
	 */
	let { day, element = $bindable() }: { day: Day; element?: HTMLDivElement } = $props();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');
	let today = getToday();
	let count = $derived(countDay(day, today));
	let span = $derived(spanBetween(today, count.target));
	let spanText = $derived(
		span.years > 0
			? $t('days.span', { y: span.years, m: span.months, d: span.days })
			: span.months > 0
				? $t('days.spanMonths', { m: span.months, d: span.days })
				: ''
	);
	let digits = $derived(formatCount(count.days, intl));
	let numberSize = $derived(digits.length > 6 ? 'text-[3.5rem]' : digits.length > 4 ? 'text-[4.25rem]' : 'text-[5.25rem]');
	let label = $derived(
		count.direction === 'future' ? $t('days.prefixFuture') : count.direction === 'past' ? $t('days.prefixPast') : ''
	);
</script>

<div bind:this={element} class="card" data-direction={count.direction}>
	<div class="board">
		<header class="flex items-center gap-2.5">
			<span class="icon">{day.emoji || '📅'}</span>
			<span class="chalk name min-w-0 flex-1 truncate">{day.name}</span>
			{#if day.repeat}
				<span class="chalk chip">↻ {$t(`days.repeats.${day.repeat}`)}</span>
			{/if}
		</header>

		<div class="flex-1 flex flex-col items-center justify-center py-8">
			{#if count.direction === 'today'}
				<span class="chalk number text-[4rem] leading-none">{$t('days.todayBig')}</span>
			{:else}
				<span class="chalk eyebrow">{label}</span>
				<span class="flex items-baseline gap-1.5 mt-1">
					<span class="chalk number {numberSize} leading-none tabular-nums">{digits}</span>
					<span class="chalk unit">{count.days === 1 ? $t('days.unitOne') : $t('days.unit')}</span>
				</span>
				{#if spanText}
					<span class="chalk soft mt-3 text-xs">≈ {spanText}</span>
				{/if}
			{/if}
		</div>

		{#if day.note}
			<p class="chalk note">{day.note}</p>
		{/if}

		<footer class="chalk footer">
			<span class="soft">{count.direction === 'past' ? $t('days.startDate') : $t('days.targetDate')}</span>
			<span class="tabular-nums">{count.target} · {weekday(count.target, intl)}</span>
		</footer>
	</div>
</div>

<style>
	.card {
		padding: 6px;
		border-radius: 24px;
		background: linear-gradient(160deg, #a87545 0%, #8a5a30 60%, #774b26 100%);
		box-shadow:
			inset 0 1px 0 rgba(255, 228, 190, 0.35),
			0 24px 48px -24px rgba(60, 32, 10, 0.5),
			0 2px 6px rgba(60, 32, 10, 0.1);
	}
	.board {
		display: flex;
		flex-direction: column;
		/* Portrait 4:5; min-height auto lets a long note make it taller. */
		aspect-ratio: 4 / 5;
		padding: 22px 22px 18px;
		border-radius: 18px;
		background: radial-gradient(120% 80% at 50% 0%, #364a3d 0%, #2b3b31 60%, #25332b 100%);
		box-shadow: inset 0 1px 6px rgba(0, 0, 0, 0.3);
	}

	.chalk {
		color: rgba(246, 244, 236, 0.92);
		font-family: 'Chalkboard SE', 'Chalkduster', 'Segoe Print', 'Bradley Hand', 'Kaiti SC', 'STKaiti', 'KaiTi', 'Comic Sans MS', cursive;
		letter-spacing: 0.02em;
	}
	.icon {
		width: 34px;
		height: 34px;
		flex-shrink: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: 10px;
		font-size: 18px;
		background: rgba(255, 255, 255, 0.08);
	}
	.name {
		font-size: 16px;
	}
	.chip {
		flex-shrink: 0;
		padding: 2px 9px;
		font-size: 11px;
		border-radius: 999px;
		background: rgba(255, 255, 255, 0.08);
		color: rgba(246, 244, 236, 0.75);
	}
	.eyebrow {
		font-size: 13px;
		letter-spacing: 0.2em;
		color: #f5e3a8;
	}
	[data-direction='past'] .eyebrow {
		color: #f1d2ab;
	}
	.number {
		font-family: 'Chalkboard SE', 'Chalkduster', ui-rounded, 'SF Pro Rounded', system-ui, -apple-system, 'Segoe UI', sans-serif;
		font-weight: 300;
		letter-spacing: -0.01em;
		color: #fbfaf4;
		text-shadow: 0 0 20px rgba(255, 255, 255, 0.08);
	}
	.unit {
		font-size: 18px;
		color: rgba(246, 244, 236, 0.7);
	}
	.soft {
		color: rgba(246, 244, 236, 0.5);
	}
	.note {
		margin-bottom: 14px;
		font-size: 13px;
		line-height: 1.65;
		text-align: center;
		white-space: pre-line;
		overflow-wrap: anywhere;
		color: rgba(246, 244, 236, 0.7);
	}
	.footer {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding-top: 12px;
		font-size: 12px;
		border-top: 1px solid rgba(246, 244, 236, 0.12);
	}
</style>
