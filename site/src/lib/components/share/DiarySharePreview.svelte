<script lang="ts">
	import { themes, type ShareOptions } from '$lib/utils/imageExport';
	import { formatDisplayDate, getDayOfWeek } from '$lib/utils/date';
	import { t } from '$lib/i18n';
	import Win95Icon from '$lib/components/win95/Win95Icon.svelte';
	import { marked } from 'marked';

	export let date: string;
	export let content: string;
	export let options: ShareOptions;
	export let mood: string = '';
	export let weather: string = '';
	export let tags: string[] = [];

	$: theme = themes[options.theme];
	$: isWin95 = theme.variant === 'win95';
	$: htmlContent = parseContent(content);

	// Same document name and taskbar caption the live skin uses
	// (Win95DiaryView's `docName` / `padTitle`), so the exported window is
	// labelled identically to the one on screen.
	$: docName = `${date}.txt`;
	$: padTitle = $t('win95.notepadTitle', { name: docName });

	// Win95 taskbar: the era's Notepad reported a character count, so the
	// exported window does too. Derived from the rendered text, not the markup.
	$: plainText = htmlContent
		.replace(/<[^>]*>/g, '')
		.replace(/&nbsp;/g, ' ')
		.replace(/&amp;/g, '&')
		.replace(/&lt;/g, '<')
		.replace(/&gt;/g, '>')
		.replace(/&quot;/g, '"')
		.trim();
	$: charCount = [...plainText].length;

	function parseContent(rawContent: string): string {
		if (!rawContent) return '';

		// Convert TipTap JSON to HTML if needed, or use marked for markdown
		try {
			const parsed = JSON.parse(rawContent);
			if (parsed.type === 'doc') {
				return convertTiptapToHtml(parsed);
			}
		} catch {
			// Not JSON, treat as markdown
			return marked.parse(rawContent) as string;
		}
		return rawContent;
	}

	function convertTiptapToHtml(doc: any): string {
		if (!doc.content) return '';
		return doc.content.map((node: any) => nodeToHtml(node)).join('');
	}

	function nodeToHtml(node: any): string {
		switch (node.type) {
			case 'paragraph':
				const pContent = node.content ? node.content.map((n: any) => nodeToHtml(n)).join('') : '';
				return `<p>${pContent}</p>`;
			case 'heading':
				const level = node.attrs?.level || 1;
				const hContent = node.content ? node.content.map((n: any) => nodeToHtml(n)).join('') : '';
				return `<h${level}>${hContent}</h${level}>`;
			case 'text':
				let text = node.text || '';
				if (node.marks) {
					for (const mark of node.marks) {
						switch (mark.type) {
							case 'bold':
								text = `<strong>${text}</strong>`;
								break;
							case 'italic':
								text = `<em>${text}</em>`;
								break;
							case 'underline':
								text = `<u>${text}</u>`;
								break;
							case 'strike':
								text = `<s>${text}</s>`;
								break;
							case 'code':
								text = `<code>${text}</code>`;
								break;
							case 'link':
								text = `<a href="${mark.attrs?.href || '#'}">${text}</a>`;
								break;
							case 'highlight':
								text = `<mark>${text}</mark>`;
								break;
						}
					}
				}
				return text;
			case 'bulletList':
				const ulContent = node.content ? node.content.map((n: any) => nodeToHtml(n)).join('') : '';
				return `<ul>${ulContent}</ul>`;
			case 'orderedList':
				const olContent = node.content ? node.content.map((n: any) => nodeToHtml(n)).join('') : '';
				return `<ol>${olContent}</ol>`;
			case 'listItem':
				const liContent = node.content ? node.content.map((n: any) => nodeToHtml(n)).join('') : '';
				return `<li>${liContent}</li>`;
			case 'blockquote':
				const bqContent = node.content ? node.content.map((n: any) => nodeToHtml(n)).join('') : '';
				return `<blockquote>${bqContent}</blockquote>`;
			case 'codeBlock':
				const codeContent = node.content ? node.content.map((n: any) => n.text || '').join('') : '';
				return `<pre><code>${codeContent}</code></pre>`;
			case 'horizontalRule':
				return '<hr />';
			case 'image':
				if (!options.showImages) return '';
				const src = node.attrs?.src || '';
				const alt = node.attrs?.alt || '';
				return `<img src="${src}" alt="${alt}" />`;
			case 'taskList':
				const tlContent = node.content ? node.content.map((n: any) => nodeToHtml(n)).join('') : '';
				return `<ul class="task-list">${tlContent}</ul>`;
			case 'taskItem':
				const checked = node.attrs?.checked ? 'checked' : '';
				const tiContent = node.content ? node.content.map((n: any) => nodeToHtml(n)).join('') : '';
				return `<li class="task-item"><input type="checkbox" ${checked} disabled />${tiContent}</li>`;
			case 'hardBreak':
				return '<br />';
			default:
				if (node.content) {
					return node.content.map((n: any) => nodeToHtml(n)).join('');
				}
				return '';
		}
	}
</script>

{#if isWin95}
	<!--
		Windows 95 variant: the entry is exported as a Notepad window sitting on
		the teal desktop. Every surface is a flat colour with 2px bevels drawn as
		inset box-shadows — no gradients (except the title bar), no radii, no soft
		shadows. html-to-image rasterises box-shadows fine, so the bevels survive
		the export.
	-->
	<div
		class="share-preview w95"
		style="width: {options.width}px; font-family: {theme.fontFamily};"
	>
		<div class="w95-window">
			<!-- Title bar -->
			<div class="w95-titlebar">
				<Win95Icon name="notepad" size={14} />
				<span class="w95-title-text">{docName}</span>
				<div class="w95-title-buttons">
					<span class="w95-title-btn">
						<svg width="6" height="6" viewBox="0 0 6 6" shape-rendering="crispEdges">
							<path d="M0 4h6v1H0z" fill="#000" />
						</svg>
					</span>
					<span class="w95-title-btn">
						<svg width="8" height="7" viewBox="0 0 8 7" shape-rendering="crispEdges">
							<path d="M0 0h8v7H0V0zm1 2v4h6V2H1z" fill="#000" />
						</svg>
					</span>
					<span class="w95-title-btn">
						<svg width="8" height="7" viewBox="0 0 8 7" shape-rendering="crispEdges">
							<path
								d="M0 0h2v1h1v1h2V1h1V0h2v1H6v1H5v1h1v1h1v1h1v1H6V5H5V4H3v1H2v1H0V5h1V4h1V3h1V2H1V1H0z"
								fill="#000"
							/>
						</svg>
					</span>
				</div>
			</div>

			<!-- Client area -->
			<div class="w95-field">
				{#if options.showDate}
					<div class="w95-doc-head">
						<span class="w95-doc-date">{formatDisplayDate(date)}</span>
						<span class="w95-doc-dow">{getDayOfWeek(date)}</span>
						{#if (options.showWeather && weather) || (options.showMood && mood)}
							<span class="w95-doc-meta">
								{#if options.showWeather && weather}<span>{weather}</span>{/if}
								{#if options.showMood && mood}<span>{mood}</span>{/if}
							</span>
						{/if}
					</div>
				{/if}

				<div class="w95-doc-body">
					{@html htmlContent}
				</div>

				{#if options.showTags && tags.length > 0}
					<div class="w95-doc-tags">
						{#each tags as tag}
							<span class="w95-tag">{tag}</span>
						{/each}
					</div>
				{/if}
			</div>

		</div>

		<!--
			Taskbar. Sits on the desktop below the window, not inside its frame,
			exactly as the real one does: raised Start button, a groove
			separator, then the sunken tray pushed to the right edge.
		-->
		<div class="w95-taskbar">
			<!-- The Start button carries the branding in this variant. -->
			{#if options.showBranding}
				<span class="w95-start">
					<Win95Icon name="diarum" size={15} />
					<span>Diarum</span>
				</span>
				<span class="w95-vsep"></span>
			{/if}
			<span class="w95-task">
				<Win95Icon name="notepad" size={14} />
				<span>{padTitle}</span>
			</span>
			<span class="w95-tray">{$t('win95.shareChars', { count: String(charCount) })}</span>
		</div>
	</div>
{:else}
	<div
		class="share-preview"
		style="
			width: {options.width}px;
			background-color: {theme.background};
			color: {theme.foreground};
			font-family: {theme.fontFamily};
			padding: 32px;
			box-sizing: border-box;
		"
	>
	<!-- Branding -->
	{#if options.showBranding}
		<div
			class="branding"
			style="
				display: flex;
				align-items: center;
				gap: 8px;
				padding-bottom: 16px;
				margin-bottom: 16px;
				border-bottom: 1px solid {theme.border};
			"
		>
			<img src="/logo.png" alt="Diarum" style="width: 24px; height: 24px;" />
			<span style="font-size: 18px; font-weight: 600;">Diarum</span>
		</div>
	{/if}

	<!-- Date -->
	{#if options.showDate}
		<div
			class="date-section"
			style="margin-bottom: 16px;"
		>
			<div style="font-size: 20px; font-weight: 600;">
				{formatDisplayDate(date)}
			</div>
			<div style="font-size: 14px; color: {theme.mutedForeground};">
				{getDayOfWeek(date)}
			</div>
		</div>
	{/if}

	<!-- Mood & Weather -->
	{#if (options.showMood && mood) || (options.showWeather && weather)}
		<div
			class="meta-section"
			style="
				display: flex;
				gap: 16px;
				margin-bottom: 16px;
				font-size: 14px;
				color: {theme.mutedForeground};
			"
		>
			{#if options.showWeather && weather}
				<span>{weather}</span>
			{/if}
			{#if options.showMood && mood}
				<span>{mood}</span>
			{/if}
		</div>
	{/if}

	<!-- Content -->
	<div
		class="content-section"
		style="
			line-height: 1.8;
			font-size: 16px;
		"
	>
		{@html htmlContent}
	</div>

	<!-- Tags -->
	{#if options.showTags && tags.length > 0}
		<div
			class="tags-section"
			style="
				margin-top: 24px;
				padding-top: 16px;
				border-top: 1px solid {theme.border};
				display: flex;
				flex-wrap: wrap;
				gap: 8px;
			"
		>
			{#each tags as tag}
				<span
					style="
						font-size: 12px;
						padding: 4px 12px;
						background-color: {theme.accent}20;
						color: {theme.accent};
						border-radius: 16px;
					"
				>
					#{tag}
				</span>
			{/each}
		</div>
	{/if}
	</div>
{/if}

<style>
	.share-preview :global(p) {
		margin-bottom: 1em;
	}

	.share-preview :global(h1) {
		font-size: 1.75em;
		font-weight: 700;
		margin-top: 1.5em;
		margin-bottom: 0.5em;
	}

	.share-preview :global(h2) {
		font-size: 1.5em;
		font-weight: 600;
		margin-top: 1.25em;
		margin-bottom: 0.5em;
	}

	.share-preview :global(h3) {
		font-size: 1.25em;
		font-weight: 600;
		margin-top: 1em;
		margin-bottom: 0.5em;
	}

	.share-preview :global(ul),
	.share-preview :global(ol) {
		margin-left: 1.5em;
		margin-bottom: 1em;
	}

	.share-preview :global(ul) {
		list-style-type: disc;
	}

	.share-preview :global(ol) {
		list-style-type: decimal;
	}

	.share-preview :global(li) {
		margin-bottom: 0.35em;
	}

	.share-preview :global(blockquote) {
		border-left: 3px solid currentColor;
		padding-left: 1em;
		margin: 1em 0;
		opacity: 0.8;
		font-style: italic;
	}

	.share-preview :global(code) {
		background-color: rgba(0, 0, 0, 0.05);
		padding: 0.2em 0.4em;
		border-radius: 4px;
		font-family: ui-monospace, monospace;
		font-size: 0.9em;
	}

	.share-preview :global(pre) {
		background-color: rgba(0, 0, 0, 0.05);
		padding: 1em;
		border-radius: 8px;
		overflow-x: auto;
		margin: 1em 0;
	}

	.share-preview :global(pre code) {
		background-color: transparent;
		padding: 0;
	}

	/* Entry images only. Left unscoped this also caught the win95 chrome's
	   logo, whose explicit height it overrode with `auto`. */
	.share-preview .content-section :global(img),
	.share-preview .w95-doc-body :global(img) {
		max-width: 100%;
		height: auto;
		border-radius: 8px;
		margin: 1em 0;
	}

	.share-preview :global(hr) {
		border: none;
		border-top: 1px solid currentColor;
		opacity: 0.2;
		margin: 1.5em 0;
	}

	.share-preview :global(mark) {
		background-color: rgba(255, 235, 59, 0.4);
		padding: 0.1em 0.2em;
		border-radius: 2px;
	}

	.share-preview :global(a) {
		color: inherit;
		text-decoration: underline;
	}

	.share-preview :global(.task-list) {
		list-style: none;
		margin-left: 0;
		padding-left: 0;
	}

	.share-preview :global(.task-item) {
		display: flex;
		align-items: flex-start;
		gap: 0.5em;
	}

	.share-preview :global(.task-item input) {
		margin-top: 0.3em;
	}

	/* ------------------------------------------------------------ win95 skin
	 *
	 * A self-contained copy of the primitives from
	 * `$lib/components/win95/win95.css`. It is duplicated rather than imported
	 * because that file is scoped under `.w95-root` and takes over the viewport
	 * (`position: fixed`), which would wreck the share preview. The values are
	 * the same four flat colours and 2px bevels.
	 */

	.share-preview.w95 {
		--w95-face: #c0c0c0;
		--w95-light: #ffffff;
		--w95-hilite: #dfdfdf;
		--w95-shadow: #808080;
		--w95-dark: #0a0a0a;
		--w95-window: #ffffff;
		--w95-desktop: #008080;
		--w95-select: #000080;

		box-sizing: border-box;
		/* Just enough desktop to frame the window and seat the taskbar. */
		padding: 12px 12px 0;
		color: #000;
		font-size: 13px;
		line-height: 1.35;
		/* Aliased text is the single biggest tell of the era, and unlike the
		   live skin the export is rasterised at a fixed size, so it is safe to
		   ask for it unconditionally. */
		-webkit-font-smoothing: none;
		/* The teal desktop, with the era's 50% dither so the window reads as
		   floating on a real Win95 screen rather than a flat swatch. */
		background-color: var(--w95-desktop);
		background-image:
			linear-gradient(45deg, rgba(255, 255, 255, 0.045) 25%, transparent 25%, transparent 75%, rgba(255, 255, 255, 0.045) 75%),
			linear-gradient(45deg, rgba(255, 255, 255, 0.045) 25%, transparent 25%, transparent 75%, rgba(255, 255, 255, 0.045) 75%);
		background-size: 4px 4px;
		background-position: 0 0, 2px 2px;
	}

	.share-preview.w95 * {
		box-sizing: border-box;
		border-radius: 0 !important;
	}

	.share-preview.w95 .w95-window {
		display: flex;
		flex-direction: column;
		padding: 3px;
		background: var(--w95-face);
		box-shadow:
			inset -1px -1px 0 0 var(--w95-dark),
			inset 1px 1px 0 0 var(--w95-light),
			inset -2px -2px 0 0 var(--w95-shadow),
			inset 2px 2px 0 0 var(--w95-hilite),
			/* The hard, offset drop shadow a real window casts on the desktop. */
			4px 4px 0 0 rgba(0, 0, 0, 0.28);
	}

	/* title bar */

	.share-preview.w95 .w95-titlebar {
		display: flex;
		align-items: center;
		gap: 4px;
		height: 22px;
		padding: 0 2px 0 3px;
		background: linear-gradient(90deg, #000080, #1084d0);
		color: #fff;
		font-weight: 700;
		font-size: 13px;
	}

	.share-preview.w95 .w95-titlebar :global(svg) {
		flex-shrink: 0;
		display: block;
	}

	.share-preview.w95 .w95-title-text {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.share-preview.w95 .w95-title-buttons {
		flex-shrink: 0;
		display: flex;
		gap: 2px;
	}

	/* The third button (close) sits 2px clear of the other two, as it did. */
	.share-preview.w95 .w95-title-buttons > :last-child {
		margin-left: 2px;
	}

	.share-preview.w95 .w95-title-btn {
		width: 17px;
		height: 15px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: var(--w95-face);
		box-shadow:
			inset -1px -1px 0 0 var(--w95-dark),
			inset 1px 1px 0 0 var(--w95-light),
			inset -2px -2px 0 0 var(--w95-shadow),
			inset 2px 2px 0 0 var(--w95-hilite);
	}

	.share-preview.w95 .w95-title-btn svg {
		display: block;
		shape-rendering: crispEdges;
	}

	/* client area */

	.share-preview.w95 .w95-field {
		margin-top: 2px;
		padding: 14px 16px;
		background: var(--w95-window);
		box-shadow:
			inset -1px -1px 0 0 var(--w95-light),
			inset 1px 1px 0 0 var(--w95-shadow),
			inset -2px -2px 0 0 var(--w95-hilite),
			inset 2px 2px 0 0 var(--w95-dark);
	}

	.share-preview.w95 .w95-doc-head {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 8px;
		margin-bottom: 12px;
		padding-bottom: 8px;
		/* The 2px groove used everywhere else in the skin, not a flat rule. */
		border-bottom: 1px solid var(--w95-shadow);
		box-shadow: 0 1px 0 0 var(--w95-light);
	}

	.share-preview.w95 .w95-doc-date {
		font-size: 15px;
		font-weight: 700;
	}

	.share-preview.w95 .w95-doc-dow,
	.share-preview.w95 .w95-doc-meta {
		font-size: 12px;
		color: #404040;
	}

	.share-preview.w95 .w95-doc-meta {
		margin-left: auto;
		display: flex;
		gap: 8px;
	}

	/* Matches `.w95-root .tiptap-editor-content` in win95.css so the exported
	   image and the live editor render an entry identically. */
	.share-preview.w95 .w95-doc-body {
		font-family: 'Lucida Console', 'Courier New', Consolas, 'Nimbus Mono PS', 'SimSun',
			'Songti SC', monospace;
		font-size: 13px;
		line-height: 1.55;
	}

	.share-preview.w95 .w95-doc-tags {
		display: flex;
		flex-wrap: wrap;
		gap: 5px;
		margin-top: 16px;
		padding-top: 10px;
		border-top: 1px solid var(--w95-shadow);
		box-shadow: inset 0 1px 0 0 var(--w95-light);
	}

	/* Tags read as the era's list-box selection chips rather than pills. */
	.share-preview.w95 .w95-tag {
		padding: 1px 7px;
		font-size: 12px;
		background: var(--w95-select);
		color: #fff;
	}

	/* taskbar
	 *
	 * Mirrors `.w95-taskbar` / `.w95-tray` in win95.css, but a few px shorter —
	 * the live one is a touch target, this one only has to look right.
	 */

	.share-preview.w95 .w95-taskbar {
		display: flex;
		align-items: center;
		gap: 4px;
		height: 26px;
		margin: 12px -12px 0;
		padding: 2px 4px;
		background: var(--w95-face);
		border-top: 1px solid var(--w95-light);
		box-shadow: inset 0 1px 0 0 var(--w95-hilite);
	}

	/* Raised, like the real Start button. */
	.share-preview.w95 .w95-start {
		display: flex;
		align-items: center;
		gap: 5px;
		flex-shrink: 0;
		height: 20px;
		padding: 0 7px;
		font-size: 12px;
		font-weight: 700;
		background: var(--w95-face);
		box-shadow:
			inset -1px -1px 0 0 var(--w95-dark),
			inset 1px 1px 0 0 var(--w95-light),
			inset -2px -2px 0 0 var(--w95-shadow),
			inset 2px 2px 0 0 var(--w95-hilite);
	}

	.share-preview.w95 .w95-vsep {
		width: 2px;
		align-self: stretch;
		margin: 1px 2px;
		border-left: 1px solid var(--w95-shadow);
		border-right: 1px solid var(--w95-light);
	}

	/* The entry's own taskbar button — pressed, since its window has focus.
	   Sized like `.task` in Win95DiaryView so it reads the same on both. */
	.share-preview.w95 .w95-task {
		display: flex;
		align-items: center;
		gap: 5px;
		flex: 0 1 240px;
		min-width: 0;
		height: 20px;
		padding: 0 7px;
		font-size: 11px;
		overflow: hidden;
		background: var(--w95-face);
		box-shadow:
			inset -1px -1px 0 0 var(--w95-light),
			inset 1px 1px 0 0 var(--w95-dark),
			inset -2px -2px 0 0 var(--w95-hilite),
			inset 2px 2px 0 0 var(--w95-shadow);
	}

	.share-preview.w95 .w95-task > span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	/* Win95Icon renders an inline <svg>; keep it from picking up a baseline gap. */
	.share-preview.w95 .w95-start :global(svg),
	.share-preview.w95 .w95-task :global(svg) {
		display: block;
		flex-shrink: 0;
	}

	.share-preview.w95 .w95-tray {
		display: flex;
		align-items: center;
		height: 20px;
		margin-left: auto;
		padding: 0 7px;
		font-size: 11px;
		white-space: nowrap;
		box-shadow:
			inset -1px -1px 0 0 var(--w95-light),
			inset 1px 1px 0 0 var(--w95-shadow);
	}

	/* ------------------------------------------- win95 rich-text overrides
	 * The generic `.share-preview :global(...)` rules above assume the soft
	 * card look (rounded code blocks, tinted panels). Squared-off equivalents
	 * follow — same specificity race is avoided by the extra `.w95` class.
	 */

	.share-preview.w95 :global(p) {
		margin-bottom: 0.7em;
	}

	.share-preview.w95 :global(h1),
	.share-preview.w95 :global(h2),
	.share-preview.w95 :global(h3) {
		font-family: inherit;
		font-weight: 700;
		color: #000;
		letter-spacing: 0;
		margin-top: 1em;
		margin-bottom: 0.35em;
	}

	.share-preview.w95 :global(h1) {
		font-size: 1.55em;
		border-bottom: 2px groove var(--w95-face);
		padding-bottom: 2px;
	}

	.share-preview.w95 :global(h2) {
		font-size: 1.28em;
	}

	.share-preview.w95 :global(h3) {
		font-size: 1.1em;
	}

	.share-preview.w95 :global(ul),
	.share-preview.w95 :global(ol) {
		margin-left: 1.4em;
		margin-bottom: 0.7em;
	}

	.share-preview.w95 :global(blockquote) {
		border-left: 4px solid var(--w95-shadow);
		background: #efefef;
		padding: 4px 10px;
		margin: 0.7em 0;
		color: #000;
		font-style: normal;
		opacity: 1;
	}

	.share-preview.w95 :global(code) {
		background: var(--w95-face);
		color: #000;
		padding: 0 2px;
		font-family: inherit;
		box-shadow:
			inset -1px -1px 0 0 var(--w95-light),
			inset 1px 1px 0 0 var(--w95-shadow);
	}

	/* The era's console colours — light-on-navy — for code blocks. */
	.share-preview.w95 :global(pre) {
		background: #000080;
		color: var(--w95-face);
		font-family: inherit;
		padding: 8px 10px;
		margin: 0.7em 0;
		box-shadow:
			inset -1px -1px 0 0 var(--w95-light),
			inset 1px 1px 0 0 var(--w95-shadow);
	}

	.share-preview.w95 :global(pre code) {
		background: transparent;
		box-shadow: none;
		color: inherit;
		padding: 0;
	}

	/* Entry images only — scoped to the document body so it never frames the
	   logo in the title bar or taskbar. */
	.share-preview.w95 .w95-doc-body :global(img) {
		border: 1px solid var(--w95-shadow);
		margin: 0.6em 0;
		box-shadow:
			inset -1px -1px 0 0 var(--w95-light),
			inset 1px 1px 0 0 var(--w95-shadow);
	}

	.share-preview.w95 :global(hr) {
		border: 0;
		border-top: 1px solid var(--w95-shadow);
		border-bottom: 1px solid var(--w95-light);
		height: 0;
		margin: 1em 0;
		opacity: 1;
	}

	.share-preview.w95 :global(mark) {
		background: #ffff00;
		color: #000;
		padding: 0;
	}

	.share-preview.w95 :global(a) {
		color: #0000c0;
		text-decoration: underline;
	}

	.share-preview.w95 :global(.task-item input) {
		accent-color: var(--w95-select);
	}
</style>
