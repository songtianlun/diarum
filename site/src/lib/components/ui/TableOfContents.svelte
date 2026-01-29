<script lang="ts">
	export let content = '';
	export let className = '';

	interface TocItem {
		id: string;
		text: string;
		level: number;
	}

	$: headings = extractHeadings(content);

	function extractHeadings(html: string): TocItem[] {
		if (!html) return [];

		const items: TocItem[] = [];
		// 匹配 HTML 标题标签
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
		const targetEl = headingEls[headingIndex];

		if (targetEl) {
			targetEl.scrollIntoView({ behavior: 'smooth', block: 'start' });
		}
	}
</script>

{#if headings.length > 0}
	<nav class="toc {className}">
		<div class="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-3 px-2">
			Contents
		</div>
		<ul class="space-y-1">
			{#each headings as heading, i}
				<li style="animation-delay: {i * 30}ms" class="animate-fade-in opacity-0">
					<button
						on:click={() => scrollToHeading(heading.id)}
						class="w-full text-left px-2 py-1 text-sm rounded-md
							   text-muted-foreground hover:text-foreground hover:bg-muted/50
							   transition-all duration-200 truncate
							   {heading.level === 1 ? 'font-medium' : ''}
							   {heading.level === 2 ? 'pl-4' : ''}
							   {heading.level === 3 ? 'pl-6 text-xs' : ''}"
					>
						{heading.text}
					</button>
				</li>
			{/each}
		</ul>
	</nav>
{:else}
	<div class="toc {className} text-center py-8">
		<div class="text-muted-foreground/50 text-sm">
			<svg class="w-8 h-8 mx-auto mb-2 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
					d="M4 6h16M4 12h16M4 18h7" />
			</svg>
			<p>No headings yet</p>
			<p class="text-xs mt-1">Use # for headings</p>
		</div>
	</div>
{/if}
