<script lang="ts">
	import type { Day } from '$lib/api/days';
	import { countDay, spanBetween, weekday, formatCount } from '$lib/utils/days';
	import { getToday } from '$lib/utils/date';
	import { t, locale } from '$lib/i18n';

	/**
	 * A day drawn as chalk on a slate board in a solid wood frame. The frame
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
	// Textures are inline SVG rather than CSS data-URI backgrounds, which
	// html-to-image drops when exporting; ids are per instance.
	const uid = Math.random().toString(36).slice(2, 8);
	let numberSize = $derived(digits.length > 6 ? 'text-[4.25rem] sm:text-[5.5rem]' : digits.length > 4 ? 'text-[5rem] sm:text-[6.5rem]' : 'text-[5.75rem] sm:text-[7.5rem]');
</script>

<div bind:this={element} class="frame" data-direction={count.direction}>
	<svg class="texture grain" aria-hidden="true">
		<filter id="wood-{uid}" x="0" y="0" width="100%" height="100%">
			<feTurbulence type="fractalNoise" baseFrequency="0.005 0.11" numOctaves="3" seed="7" />
			<feColorMatrix values="0 0 0 0 0.22  0 0 0 0 0.11  0 0 0 0 0.03  0 0 0 0.9 -0.12" />
		</filter>
		<rect width="100%" height="100%" filter="url(#wood-{uid})" />
	</svg>
	<span class="nail tl"></span><span class="nail tr"></span><span class="nail bl"></span><span class="nail br"></span>
	<div class="slate">
		<svg class="texture" aria-hidden="true">
			<filter id="dust-{uid}" x="0" y="0" width="100%" height="100%">
				<feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="2" />
				<feColorMatrix values="0 0 0 0 1  0 0 0 0 1  0 0 0 0 1  0 0 0 0.07 0" />
			</filter>
			<rect width="100%" height="100%" filter="url(#dust-{uid})" />
		</svg>
		<div class="smudge"></div>
		<div class="relative flex flex-col items-center text-center px-6 sm:px-10 pt-7 pb-6 sm:pt-9 sm:pb-8">
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
			<svg class="underline-stroke" viewBox="0 0 300 12" preserveAspectRatio="none" aria-hidden="true">
				<path d="M4 7 C 60 2, 120 11, 180 6 S 270 4, 296 7" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
			</svg>

			<div class="chalk meta mt-3 text-sm sm:text-base">
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
				<div class="chalk note mt-4 text-sm sm:text-[0.95rem] leading-relaxed max-w-md whitespace-pre-line break-words">{day.note}</div>
			{/if}
		</div>
		<div class="brand">Diarum</div>
	</div>
	<div class="ledge">
		<span class="stick white"></span>
		<span class="stick yellow"></span>
		<span class="stick pink"></span>
		<span class="eraser"></span>
	</div>
</div>

<style>
	/* ---- Wood frame ---- */
	.frame {
		--wood-dark: #5b3416;
		--wood: #8a5528;
		--wood-light: #b07a45;
		position: relative;
		padding: 16px 16px 30px;
		border-radius: 14px;
		background: linear-gradient(180deg, var(--wood-light) 0%, var(--wood) 45%, var(--wood-dark) 100%);
		box-shadow:
			inset 0 1px 0 rgba(255, 230, 190, 0.45),
			inset 0 -2px 0 rgba(0, 0, 0, 0.25),
			inset 2px 0 0 rgba(255, 220, 170, 0.15),
			inset -2px 0 0 rgba(0, 0, 0, 0.18),
			0 24px 48px -18px rgba(60, 30, 8, 0.55),
			0 4px 10px rgba(60, 30, 8, 0.18);
	}
	.texture {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		pointer-events: none;
	}
	.grain {
		border-radius: 14px;
	}
	.nail {
		position: absolute;
		width: 7px;
		height: 7px;
		border-radius: 999px;
		background: radial-gradient(circle at 35% 35%, #f3e3c3 0%, #a68a5d 45%, #4d3a1f 100%);
		box-shadow: 0 1px 1px rgba(0, 0, 0, 0.45);
		z-index: 2;
	}
	.nail.tl { top: 5px; left: 5px; }
	.nail.tr { top: 5px; right: 5px; }
	.nail.bl { bottom: 19px; left: 5px; }
	.nail.br { bottom: 19px; right: 5px; }

	/* ---- Slate ---- */
	.slate {
		position: relative;
		overflow: hidden;
		border-radius: 6px;
		min-height: 300px;
		background: radial-gradient(120% 90% at 50% 0%, #34473b 0%, #2a3a30 55%, #223028 100%);
		box-shadow:
			inset 0 3px 8px rgba(0, 0, 0, 0.55),
			inset 0 0 50px rgba(0, 0, 0, 0.4),
			0 0 0 2px rgba(40, 20, 6, 0.55);
	}
	/* Half-erased chalk haze, the way a used board never comes clean. */
	.smudge {
		position: absolute;
		inset: 0;
		pointer-events: none;
		background:
			radial-gradient(40% 22% at 22% 26%, rgba(255, 255, 255, 0.07), transparent 70%),
			radial-gradient(35% 18% at 78% 72%, rgba(255, 255, 255, 0.05), transparent 70%),
			radial-gradient(30% 14% at 60% 18%, rgba(255, 255, 255, 0.035), transparent 70%);
	}

	/* ---- Chalk ---- */
	.chalk {
		color: rgba(246, 244, 236, 0.93);
		text-shadow:
			0 0 1px rgba(255, 255, 255, 0.55),
			0 0 8px rgba(255, 255, 255, 0.12),
			1px 1px 0 rgba(0, 0, 0, 0.12);
		font-family: 'Chalkboard SE', 'Chalkduster', 'Segoe Print', 'Bradley Hand', 'Kaiti SC', 'STKaiti', 'KaiTi', 'Comic Sans MS', cursive;
		letter-spacing: 0.02em;
	}
	.not-chalk {
		text-shadow: none;
	}
	.headline {
		color: #f8e9a1;
	}
	[data-direction='past'] .headline {
		color: #f3cfa2;
	}
	[data-direction='today'] .headline {
		color: #fbe3b0;
	}
	.number {
		font-family: 'Chalkduster', 'Chalkboard SE', 'Bradley Hand', Georgia, 'Times New Roman', serif;
		font-weight: 400;
		letter-spacing: 0.01em;
		color: #fbfaf4;
		text-shadow:
			0 0 1px rgba(255, 255, 255, 0.8),
			0 0 14px rgba(255, 255, 255, 0.18),
			2px 2px 0 rgba(0, 0, 0, 0.12);
	}
	.unit {
		opacity: 0.85;
	}
	.underline-stroke {
		width: min(62%, 280px);
		height: 12px;
		margin-top: 2px;
		color: rgba(248, 233, 161, 0.75);
	}
	[data-direction='past'] .underline-stroke {
		color: rgba(243, 207, 162, 0.75);
	}
	[data-direction='today'] .underline-stroke {
		color: rgba(251, 227, 176, 0.75);
	}
	.meta {
		opacity: 0.88;
	}
	.meta-soft {
		opacity: 0.62;
	}
	.tag {
		padding: 2px 10px;
		border: 1.5px dashed rgba(246, 244, 236, 0.45);
		border-radius: 999px;
		opacity: 0.8;
	}
	.note {
		position: relative;
		padding-top: 14px;
		opacity: 0.8;
	}
	.note::before {
		content: '';
		position: absolute;
		top: 0;
		left: 50%;
		width: 48px;
		margin-left: -24px;
		border-top: 1.5px dashed rgba(246, 244, 236, 0.35);
	}
	.brand {
		position: absolute;
		right: 12px;
		bottom: 8px;
		font-size: 10px;
		letter-spacing: 0.18em;
		text-transform: uppercase;
		color: rgba(246, 244, 236, 0.32);
		font-family: 'Chalkboard SE', 'Segoe Print', cursive;
	}

	/* ---- Chalk ledge ---- */
	.ledge {
		position: absolute;
		left: 10px;
		right: 10px;
		bottom: 8px;
		height: 12px;
		border-radius: 3px;
		background: linear-gradient(180deg, #c48d55 0%, #9a6532 55%, #6e4220 100%);
		box-shadow:
			inset 0 1px 0 rgba(255, 235, 200, 0.5),
			0 2px 3px rgba(0, 0, 0, 0.35);
	}
	.stick {
		position: absolute;
		bottom: 7px;
		height: 6px;
		border-radius: 3px;
		box-shadow: 0 1px 1px rgba(0, 0, 0, 0.3);
	}
	.stick.white { left: 18%; width: 34px; background: linear-gradient(180deg, #ffffff, #dedad0); }
	.stick.yellow { left: calc(18% + 42px); width: 22px; background: linear-gradient(180deg, #fff2a8, #e2cf6d); }
	.stick.pink { left: calc(18% + 70px); width: 16px; background: linear-gradient(180deg, #ffd0db, #e39aac); }
	.eraser {
		position: absolute;
		right: 16%;
		bottom: 6px;
		width: 54px;
		height: 13px;
		border-radius: 3px;
		background:
			linear-gradient(180deg, #d9b07a 0 45%, #3f4447 45% 100%);
		box-shadow: 0 2px 2px rgba(0, 0, 0, 0.35), inset 0 1px 0 rgba(255, 240, 210, 0.6);
	}
</style>
