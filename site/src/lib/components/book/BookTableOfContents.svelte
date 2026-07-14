<script lang="ts">
	/**
	 * Heading outline for the current diary entry, styled to sit on a book
	 * page (warm paper palette rather than the app's neutral theme tokens).
	 *
	 * Unlike the generic TableOfContents (which scrolls the window), the book's
	 * live editor lives inside its own internal `.content-scroll` container, so
	 * navigating a heading here uses `scrollIntoView`, which walks up whatever
	 * scrollable ancestor actually needs to move.
	 */
	export let content = '';
	/** called after a heading is clicked and scrolled to (e.g. to close a drawer) */
	export let onNavigate: (() => void) | undefined = undefined;

	interface TocItem {
		id: string;
		text: string;
		level: number;
	}

	$: headings = extractHeadings(content);

	function extractHeadings(html: string): TocItem[] {
		if (!html) return [];

		const items: TocItem[] = [];
		const regex = /<h([1-3])[^>]*>([^<]+)<\/h[1-3]>/gi;
		let match;
		let index = 0;

		while ((match = regex.exec(html)) !== null) {
			const level = parseInt(match[1]);
			const text = match[2].trim();
			const id = `heading-${index++}`;
			items.push({ id, text, level });
		}

		return items;
	}

	function scrollToHeading(id: string) {
		const headingIndex = parseInt(id.replace('heading-', ''));
		const editorEl = document.querySelector('.tiptap-editor-content');
		if (!editorEl) return;

		const headingEls = editorEl.querySelectorAll('h1, h2, h3');
		const targetEl = headingEls[headingIndex] as HTMLElement | undefined;

		if (targetEl) {
			targetEl.scrollIntoView({ behavior: 'smooth', block: 'start' });
			onNavigate?.();
		}
	}
</script>

{#if headings.length > 0}
	<nav class="book-toc">
		<ul>
			{#each headings as heading, i}
				<li style="animation-delay: {i * 30}ms" class="animate-fade-in opacity-0">
					<button
						class="toc-link level-{heading.level}"
						title={heading.text}
						on:click={() => scrollToHeading(heading.id)}
					>
						{heading.text}
					</button>
				</li>
			{/each}
		</ul>
	</nav>
{:else}
	<div class="book-toc-empty">
		<svg class="w-7 h-7" fill="none" stroke="currentColor" viewBox="0 0 24 24">
			<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 6h16M4 12h16M4 18h7" />
		</svg>
		<p>No headings yet</p>
		<p class="hint">Use # for headings to build an outline</p>
	</div>
{/if}

<style>
	.book-toc ul {
		display: flex;
		flex-direction: column;
		gap: 0.15rem;
	}

	.toc-link {
		display: block;
		width: 100%;
		text-align: left;
		padding: 0.4rem 0.6rem;
		border-radius: 0.5rem;
		font-size: 0.85rem;
		line-height: 1.3;
		color: hsl(30 20% 40%);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		transition: background-color 0.15s ease, color 0.15s ease, transform 0.15s ease;
	}
	.toc-link:hover {
		background: hsl(30 30% 60% / 0.14);
		color: hsl(25 35% 22%);
		transform: translateX(1px);
	}
	.toc-link.level-1 {
		font-weight: 600;
		color: hsl(25 35% 25%);
	}
	.toc-link.level-2 {
		padding-left: 1.15rem;
	}
	.toc-link.level-3 {
		padding-left: 1.65rem;
		font-size: 0.78rem;
	}
	:global(.dark) .toc-link {
		color: hsl(40 20% 70%);
	}
	:global(.dark) .toc-link:hover {
		background: hsl(40 25% 45% / 0.18);
		color: hsl(45 25% 92%);
	}
	:global(.dark) .toc-link.level-1 {
		color: hsl(45 25% 90%);
	}

	.book-toc-empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.4rem;
		padding: 2.25rem 1rem;
		text-align: center;
		color: hsl(30 20% 50% / 0.7);
	}
	.book-toc-empty svg {
		opacity: 0.5;
		margin-bottom: 0.15rem;
	}
	.book-toc-empty p {
		margin: 0;
		font-size: 0.85rem;
	}
	.book-toc-empty .hint {
		font-size: 0.72rem;
		opacity: 0.8;
	}
	:global(.dark) .book-toc-empty {
		color: hsl(40 20% 65% / 0.6);
	}
</style>
