<script lang="ts">
	/**
	 * The entry's heading outline, drawn with the same tree rails as the
	 * contents pane so the two panels read as one Explorer-style control.
	 *
	 * Headings come from the stored HTML (identical extraction to the classic
	 * TableOfContents); clicking one scrolls the Notepad client area rather
	 * than the document, because the win95 shell owns the viewport.
	 */
	import Win95Icon from './Win95Icon.svelte';
	import { t } from '$lib/i18n';

	export let content = '';
	/** Selector of the scrollable client area to move. */
	export let scrollSelector = '.w95-editor-scroll';
	export let onNavigate: (() => void) | undefined = undefined;

	interface Row {
		text: string;
		level: number;
		index: number;
		guides: boolean[];
		last: boolean;
	}

	$: rows = buildRows(content);

	function decode(raw: string): string {
		return raw
			.replace(/&nbsp;/g, ' ')
			.replace(/&lt;/g, '<')
			.replace(/&gt;/g, '>')
			.replace(/&quot;/g, '"')
			.replace(/&#39;/g, "'")
			.replace(/&amp;/g, '&')
			.trim();
	}

	function buildRows(html: string): Row[] {
		if (!html) return [];

		const flat: { text: string; level: number; index: number }[] = [];
		const re = /<h([1-3])[^>]*>([\s\S]*?)<\/h[1-3]>/gi;
		let match: RegExpExecArray | null;
		let index = 0;
		while ((match = re.exec(html)) !== null) {
			const text = decode(match[2].replace(/<[^>]*>/g, ''));
			flat.push({ text: text || ' ', level: Number(match[1]), index: index++ });
		}

		return flat.map((item, i) => {
			const guides: boolean[] = [];
			for (let col = 1; col < item.level; col++) {
				let carries = false;
				for (let k = i + 1; k < flat.length; k++) {
					if (flat[k].level < col) break;
					if (flat[k].level === col) {
						carries = true;
						break;
					}
				}
				guides.push(carries);
			}

			let last = true;
			for (let k = i + 1; k < flat.length; k++) {
				if (flat[k].level < item.level) break;
				if (flat[k].level === item.level) {
					last = false;
					break;
				}
			}

			return { ...item, guides, last };
		});
	}

	function jump(index: number) {
		const scroller = document.querySelector(scrollSelector) as HTMLElement | null;
		const editor = document.querySelector('.w95-root .tiptap-editor-content');
		if (!editor) return;

		const target = editor.querySelectorAll('h1, h2, h3')[index] as HTMLElement | undefined;
		if (!target) return;

		if (scroller) {
			const top =
				target.getBoundingClientRect().top -
				scroller.getBoundingClientRect().top +
				scroller.scrollTop -
				6;
			scroller.scrollTo({ top: Math.max(0, top), behavior: 'smooth' });
		} else {
			target.scrollIntoView({ block: 'start', behavior: 'smooth' });
		}
		onNavigate?.();
	}
</script>

<div class="outline-pane">
	<div class="w95-pane-caption muted">
		<Win95Icon name="outline" size={13} />
		<span>{$t('win95.outlineTitle')}</span>
	</div>

	<div class="w95-field outline-body">
		{#if rows.length === 0}
			<div class="w95-tree-empty">{$t('win95.outlineEmpty')}</div>
		{:else}
			<div class="w95-tree" role="tree" aria-label={$t('win95.outlineTitle')}>
				{#each rows as row (row.index)}
					<div class="w95-tree-row">
						{#each row.guides as guide}
							<span class="w95-rail" class:line={guide}></span>
						{/each}
						<span class="w95-knob">
							<span class="trunk" class:half={row.last}></span>
							<span class="elbow"></span>
						</span>
						<button type="button" class="w95-node" title={row.text} on:click={() => jump(row.index)}>
							<Win95Icon name="heading" size={13} />
							<span class="label" class:h1={row.level === 1}>{row.text}</span>
						</button>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<style>
	.outline-pane {
		display: flex;
		flex-direction: column;
		min-height: 82px;
		flex: 2 1 0;
	}

	.outline-body {
		flex: 1 1 auto;
		min-height: 60px;
		overflow: auto;
	}

	.label.h1 {
		font-weight: 700;
	}
</style>
