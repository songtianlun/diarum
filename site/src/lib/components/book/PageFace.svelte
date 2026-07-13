<script lang="ts">
	import { parseDate } from '$lib/utils/date';
	import MoodWeatherPicker from '$lib/components/book/MoodWeatherPicker.svelte';

	/**
	 * A single paper page of the diary book.
	 * kind:
	 *  - 'meta'    : decorative left page with big date, mood & weather
	 *  - 'content' : static (read-only) rendered diary content
	 *  - 'live'    : content page hosting the editor via <slot />
	 *  - 'blank'   : paper backside (used for the mobile leaf back face)
	 * side controls corner rounding & spine shading.
	 */
	export let kind: 'meta' | 'content' | 'live' | 'blank' = 'content';
	export let side: 'left' | 'right' | 'single' = 'single';
	export let date = '';
	export let content = '';
	export let mood = '';
	export let weather = '';
	/** show compact date header on content pages (mobile single-page mode) */
	export let header = false;
	/** initial scroll offset for static content snapshots (matches the live page) */
	export let scrollTop = 0;

	/** when true, mood/weather become clickable pickers instead of static text */
	export let interactive = false;
	export let moodPresets: string[] = [];
	export let weatherPresets: string[] = [];
	export let onMoodSelect: (emoji: string) => void = () => {};
	export let onWeatherSelect: (emoji: string) => void = () => {};

	function initScroll(node: HTMLElement) {
		if (scrollTop > 0) node.scrollTop = scrollTop;
	}

	$: d = date ? parseDate(date) : null;
	$: monthLabel = d ? d.toLocaleDateString('en-US', { month: 'long' }) : '';
	$: weekdayLabel = d ? d.toLocaleDateString('en-US', { weekday: 'long' }) : '';
	$: shortLabel = d
		? d.toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })
		: '';
	$: hasContent = !!content && content.replace(/<[^>]*>/g, '').trim().length > 0
		|| /<(img|video|audio|iframe)\b/i.test(content || '');
</script>

<div class="page-face side-{side}" class:is-blank={kind === 'blank'}>
	{#if kind === 'meta' && d}
		<div class="meta-page">
			<div class="meta-month">{monthLabel} {d.getFullYear()}</div>
			<div class="meta-day">{d.getDate()}</div>
			<div class="meta-weekday">{weekdayLabel}</div>
			{#if interactive}
				<div class="meta-emojis interactive">
					<MoodWeatherPicker label="Mood" value={mood} options={moodPresets} onSelect={onMoodSelect} />
					<MoodWeatherPicker
						label="Weather"
						value={weather}
						options={weatherPresets}
						align="right"
						onSelect={onWeatherSelect}
					/>
				</div>
			{:else if mood || weather}
				<div class="meta-emojis">
					{#if mood}<span title="Mood">{mood}</span>{/if}
					{#if weather}<span title="Weather">{weather}</span>{/if}
				</div>
			{/if}
			<div class="meta-flourish" aria-hidden="true">✦</div>
		</div>
		<div class="page-footer">{shortLabel}</div>
	{:else if kind === 'content' || kind === 'live'}
		<div class="content-page">
			{#if header && d}
				<div class="content-header">
					<div class="content-header-date">
						<span class="content-header-day">{d.getDate()}</span>
						<span class="content-header-rest">{monthLabel} {d.getFullYear()} · {weekdayLabel}</span>
					</div>
					{#if interactive}
						<div class="content-header-emojis interactive">
							<MoodWeatherPicker
								label="Mood"
								value={mood}
								options={moodPresets}
								align="right"
								onSelect={onMoodSelect}
							/>
							<MoodWeatherPicker
								label="Weather"
								value={weather}
								options={weatherPresets}
								align="right"
								onSelect={onWeatherSelect}
							/>
						</div>
					{:else if mood || weather}
						<div class="content-header-emojis">{mood} {weather}</div>
					{/if}
				</div>
			{/if}
			<div class="content-scroll" use:initScroll>
				{#if kind === 'live'}
					<slot />
				{:else if hasContent}
					<div class="tiptap-editor-content book-static">{@html content}</div>
				{:else}
					<div class="empty-page">This page is blank</div>
				{/if}
			</div>
		</div>
		<div class="page-footer">{shortLabel}</div>
	{/if}
</div>

<style>
	.page-face {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		overflow: hidden;
		/* always rasterize on the GPU so text antialiasing doesn't visibly
		   switch when the page becomes part of a 3D-transformed flip leaf */
		transform: translateZ(0);
		backface-visibility: hidden;
		-webkit-backface-visibility: hidden;
		background:
			linear-gradient(120deg, hsl(45 42% 97% / 0.9), hsl(43 38% 94% / 0.9)),
			hsl(44 40% 96%);
		color: hsl(25 35% 25%);
	}

	:global(.dark) .page-face {
		background:
			linear-gradient(120deg, hsl(28 22% 15% / 0.92), hsl(26 20% 12% / 0.92)),
			hsl(27 22% 13%);
		color: hsl(45 25% 88%);
	}

	/* paper grain */
	.page-face::before {
		content: '';
		position: absolute;
		inset: 0;
		pointer-events: none;
		background-image: repeating-linear-gradient(
			0deg,
			transparent 0 2px,
			hsl(30 30% 40% / 0.014) 2px 4px
		);
	}

	/* spine shading */
	.page-face::after {
		content: '';
		position: absolute;
		inset: 0;
		pointer-events: none;
		z-index: 1;
	}
	.side-left {
		border-radius: 10px 0 0 10px;
	}
	.side-left::after {
		background: linear-gradient(to left, hsl(25 40% 15% / 0.16), transparent 9%);
	}
	.side-right {
		border-radius: 0 10px 10px 0;
	}
	.side-right::after {
		background: linear-gradient(to right, hsl(25 40% 15% / 0.16), transparent 9%);
	}
	.side-single {
		border-radius: 10px;
	}
	.side-single::after {
		background: linear-gradient(to right, hsl(25 40% 15% / 0.12), transparent 6%);
	}
	:global(.dark) .page-face::after {
		opacity: 0.9;
	}

	.is-blank::before {
		opacity: 0.6;
	}

	/* ---------- meta (left) page ---------- */
	.meta-page {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.35rem;
		padding: 2rem;
		text-align: center;
		position: relative;
		z-index: 2;
	}
	.meta-month {
		font-size: 0.85rem;
		letter-spacing: 0.28em;
		text-transform: uppercase;
		color: hsl(30 20% 50%);
	}
	:global(.dark) .meta-month {
		color: hsl(40 20% 62%);
	}
	.meta-day {
		font-family: ui-serif, Georgia, 'Times New Roman', serif;
		font-size: clamp(4rem, 9vw, 6.5rem);
		line-height: 1;
		font-weight: 600;
	}
	.meta-weekday {
		font-family: ui-serif, Georgia, serif;
		font-style: italic;
		font-size: 1.05rem;
		color: hsl(30 20% 48%);
	}
	:global(.dark) .meta-weekday {
		color: hsl(40 20% 62%);
	}
	.meta-emojis {
		margin-top: 1.1rem;
		display: flex;
		align-items: center;
		gap: 0.75rem;
		font-size: 1.6rem;
	}
	.meta-emojis.interactive {
		gap: 0.4rem;
	}
	.meta-flourish {
		margin-top: 1.4rem;
		font-size: 0.8rem;
		opacity: 0.35;
	}

	/* ---------- content page ---------- */
	.content-page {
		flex: 1;
		display: flex;
		flex-direction: column;
		min-height: 0;
		position: relative;
		z-index: 2;
	}
	.content-header {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 0.75rem;
		padding: 0.9rem 1.5rem 0.5rem;
		border-bottom: 1px solid hsl(30 30% 60% / 0.18);
		flex-shrink: 0;
	}
	.content-header-day {
		font-family: ui-serif, Georgia, serif;
		font-size: 1.6rem;
		font-weight: 600;
		margin-right: 0.5rem;
	}
	.content-header-rest {
		font-size: 0.8rem;
		color: hsl(30 20% 50%);
	}
	:global(.dark) .content-header-rest {
		color: hsl(40 20% 62%);
	}
	.content-header-emojis {
		font-size: 1.1rem;
		flex-shrink: 0;
	}
	.content-header-emojis.interactive {
		display: flex;
		align-items: center;
		gap: 0.3rem;
	}
	.content-scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		/* keep text width identical whether or not a scrollbar is present,
		   so lines never re-wrap when swapping live page <-> flip snapshot */
		scrollbar-gutter: stable;
	}
	.content-scroll :global(.book-static) {
		min-height: 0;
		/* match ProseMirror's whitespace handling exactly — otherwise the
		   static snapshot can wrap lines differently than the live editor */
		word-wrap: break-word;
		white-space: pre-wrap;
		white-space: break-spaces;
	}
	.empty-page {
		height: 100%;
		min-height: 12rem;
		display: flex;
		align-items: center;
		justify-content: center;
		font-family: ui-serif, Georgia, serif;
		font-style: italic;
		font-size: 0.9rem;
		color: hsl(30 20% 55% / 0.55);
	}

	.page-footer {
		flex-shrink: 0;
		padding: 0.45rem 0;
		text-align: center;
		font-size: 0.68rem;
		letter-spacing: 0.12em;
		color: hsl(30 20% 52% / 0.75);
		position: relative;
		z-index: 2;
	}
	:global(.dark) .page-footer {
		color: hsl(40 18% 60% / 0.7);
	}
</style>
