<script lang="ts">
	import { parseDate } from '$lib/utils/date';
	import MoodWeatherPicker from '$lib/components/book/MoodWeatherPicker.svelte';
	import BookTableOfContents from '$lib/components/book/BookTableOfContents.svelte';

	/**
	 * A single paper page of the diary book.
	 * kind:
	 *  - 'meta'    : decorative left page with big date, mood & weather
	 *  - 'content' : static (read-only) rendered diary content
	 *  - 'live'    : content page hosting the editor via <slot />
	 *  - 'blank'   : paper backside (used for the mobile leaf back face)
	 *  - 'toc'     : left page showing the date plus a contents outline
	 * side controls corner rounding & spine shading.
	 */
	export let kind: 'meta' | 'content' | 'live' | 'blank' | 'toc' = 'content';
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

	/** kind='toc' only: called after a heading is clicked and scrolled to */
	export let onNavigate: (() => void) | undefined = undefined;

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
			<!-- always reserve this row's space (interactive or not) so mood/weather
			     never pop in/out and shift the vertically-centered date block -->
			<div class="meta-emojis">
				<MoodWeatherPicker
					label="Mood"
					value={mood}
					options={moodPresets}
					size="lg"
					disabled={!interactive}
					onSelect={onMoodSelect}
				/>
				<MoodWeatherPicker
					label="Weather"
					value={weather}
					options={weatherPresets}
					align="right"
					size="lg"
					disabled={!interactive}
					onSelect={onWeatherSelect}
				/>
			</div>
			<div class="meta-flourish" aria-hidden="true">✦</div>
		</div>
		<div class="page-footer">{shortLabel}</div>
	{:else if kind === 'toc' && d}
		<div class="toc-page">
			<div class="toc-page-eyebrow">Contents</div>
			<div class="toc-page-date">
				<div class="toc-page-day">{d.getDate()}</div>
				<div class="toc-page-date-info">
					<div class="toc-page-month">{monthLabel} {d.getFullYear()}</div>
					<div class="toc-page-weekday">{weekdayLabel}</div>
				</div>
			</div>
			<div class="toc-page-scroll">
				<BookTableOfContents {content} {onNavigate} />
			</div>
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
					<!-- always reserve this row's space (interactive or not) so mood/weather
					     never pop in/out once the flip lands -->
					<div class="content-header-emojis">
						<MoodWeatherPicker
							label="Mood"
							value={mood}
							options={moodPresets}
							align="right"
							disabled={!interactive}
							onSelect={onMoodSelect}
						/>
						<MoodWeatherPicker
							label="Weather"
							value={weather}
							options={weatherPresets}
							align="right"
							disabled={!interactive}
							onSelect={onWeatherSelect}
						/>
					</div>
				</div>
			{/if}
			<div class="content-scroll" use:initScroll>
				{#if kind === 'live'}
					<slot />
				{:else if hasContent}
					<div class="tiptap-editor-content book-static">{@html content}</div>
				{:else}
					<!-- kind="content" is only ever a transient flip snapshot — either
					     still fetching, or genuinely blank but about to settle into the
					     live editor's own "Reflect on today..." placeholder a moment
					     later. Either way a confident "this page is blank" would just
					     flash and then get corrected, so show a soft, lively settling
					     animation instead of asserting anything. -->
					<div class="page-settling" aria-hidden="true">
						<svg class="settling-pen" viewBox="0 0 24 24" fill="none">
							<path
								d="M4.5 19.5l1-4.2L15.8 4.9a1.7 1.7 0 012.4 0l1 1a1.7 1.7 0 010 2.4L8.8 18.6l-4.3.9z"
								fill="hsl(35 45% 88%)"
								stroke="currentColor"
								stroke-width="1.3"
								stroke-linejoin="round"
							/>
							<path d="M14.5 6.2l3.3 3.3" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
						</svg>
						<svg class="settling-line" viewBox="0 0 64 10" preserveAspectRatio="none">
							<path
								d="M2 6c4-4 8 4 12 0s8-4 12 0 8 4 12 0 8-4 12 0 8 4 12 0"
								fill="none"
								stroke="currentColor"
								stroke-width="1.6"
								stroke-linecap="round"
								pathLength="100"
							/>
						</svg>
					</div>
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
		justify-content: center;
		gap: 0.4rem;
	}
	.meta-flourish {
		margin-top: 1.4rem;
		font-size: 0.8rem;
		opacity: 0.35;
	}

	/* ---------- toc (contents) page ---------- */
	.toc-page {
		flex: 1;
		display: flex;
		flex-direction: column;
		min-height: 0;
		padding: 1.6rem 1.4rem 0.5rem;
		position: relative;
		z-index: 2;
	}
	.toc-page-eyebrow {
		flex-shrink: 0;
		font-size: 0.7rem;
		letter-spacing: 0.28em;
		text-transform: uppercase;
		color: hsl(30 20% 55% / 0.75);
		margin-bottom: 0.6rem;
	}
	:global(.dark) .toc-page-eyebrow {
		color: hsl(40 20% 65% / 0.7);
	}
	.toc-page-date {
		flex-shrink: 0;
		display: flex;
		align-items: baseline;
		gap: 0.65rem;
		padding-bottom: 0.85rem;
		margin-bottom: 0.5rem;
		border-bottom: 1px solid hsl(30 30% 60% / 0.18);
	}
	.toc-page-day {
		font-family: ui-serif, Georgia, 'Times New Roman', serif;
		font-size: 2.2rem;
		font-weight: 600;
		line-height: 1;
	}
	.toc-page-date-info {
		display: flex;
		flex-direction: column;
		gap: 0.1rem;
	}
	.toc-page-month {
		font-size: 0.72rem;
		letter-spacing: 0.16em;
		text-transform: uppercase;
		color: hsl(30 20% 50%);
	}
	.toc-page-weekday {
		font-family: ui-serif, Georgia, serif;
		font-style: italic;
		font-size: 0.85rem;
		color: hsl(30 20% 48%);
	}
	:global(.dark) .toc-page-month,
	:global(.dark) .toc-page-weekday {
		color: hsl(40 20% 62%);
	}
	.toc-page-scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overflow-x: hidden;
		overscroll-behavior: contain;
		padding-top: 0.15rem;
		scrollbar-gutter: stable;
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
		display: flex;
		align-items: center;
		gap: 0.3rem;
		flex-shrink: 0;
	}
	.content-scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overflow-x: hidden;
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
	/* transient placeholder shown on a "content" snapshot that hasn't settled
	   yet — either still fetching, or genuinely blank a moment before the
	   live editor's own empty-state prompt takes over. Never asserts
	   anything, just a soft, lively hint that something is on its way. */
	.page-settling {
		height: 100%;
		min-height: 12rem;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.85rem;
		color: hsl(30 20% 50% / 0.5);
	}
	:global(.dark) .page-settling {
		color: hsl(40 20% 65% / 0.5);
	}
	.settling-pen {
		width: 1.9rem;
		height: 1.9rem;
		transform-origin: 75% 75%;
		animation: pen-write 1.1s ease-in-out infinite;
	}
	@keyframes pen-write {
		0%,
		100% {
			transform: rotate(-7deg) translate(-1px, 0.5px);
		}
		50% {
			transform: rotate(7deg) translate(1px, -0.5px);
		}
	}
	.settling-line {
		width: 3.6rem;
		height: 0.7rem;
		overflow: visible;
	}
	.settling-line path {
		stroke-dasharray: 100;
		animation: line-write 1.8s ease-in-out infinite;
	}
	@keyframes line-write {
		0% {
			stroke-dashoffset: 100;
			opacity: 0.3;
		}
		55% {
			stroke-dashoffset: 0;
			opacity: 0.9;
		}
		80% {
			stroke-dashoffset: 0;
			opacity: 0.9;
		}
		100% {
			stroke-dashoffset: -100;
			opacity: 0.3;
		}
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
