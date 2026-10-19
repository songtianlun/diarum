<script lang="ts" module>
	export interface TipContent {
		title: string;
		kind: string;
		/** Tailwind classes for the kind chip. */
		kindClass: string;
		rows: [string, string][];
		note?: string;
		href?: string;
		linkLabel?: string;
		/** A button in place of the link, e.g. to edit the marker. */
		action?: () => void;
	}
</script>

<script lang="ts">
	import { onMount } from 'svelte';
	import { portal } from '$lib/utils/portal';

	/**
	 * A small card anchored to a chart marker. It lives on <body> with fixed
	 * positioning so the chart's scroll container cannot clip it; it sits
	 * above the marker, or below when there is no room. Pinned cards take
	 * clicks (for their link) and close on outside click, Escape or scroll.
	 */
	let {
		anchor,
		content,
		pinned = false,
		onclose
	}: { anchor: DOMRect; content: TipContent; pinned?: boolean; onclose: () => void } = $props();

	let card: HTMLDivElement | undefined = $state();
	let left = $state(0);
	let top = $state(0);
	let below = $state(false);

	$effect(() => {
		if (!card) return;
		const margin = 8;
		const width = card.offsetWidth;
		const height = card.offsetHeight;
		const center = anchor.left + anchor.width / 2;
		left = Math.min(Math.max(center - width / 2, margin), window.innerWidth - width - margin);
		below = anchor.top - height - 10 < margin;
		top = below ? anchor.bottom + 10 : anchor.top - height - 10;
	});

	onMount(() => {
		const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onclose();
		const onScroll = () => onclose();
		// Outside clicks close a pinned card; markers handle their own clicks.
		const onPointer = (e: PointerEvent) => {
			const target = e.target as Element | null;
			if (pinned && !card?.contains(target) && !target?.closest('[data-gantt-marker]')) onclose();
		};
		window.addEventListener('pointerdown', onPointer);
		window.addEventListener('keydown', onKey);
		window.addEventListener('scroll', onScroll, true);
		window.addEventListener('resize', onScroll);
		return () => {
			window.removeEventListener('keydown', onKey);
			window.removeEventListener('scroll', onScroll, true);
			window.removeEventListener('resize', onScroll);
			window.removeEventListener('pointerdown', onPointer);
		};
	});
</script>

<div
	use:portal
	bind:this={card}
	role="tooltip"
	class="fixed z-[60] w-60 rounded-xl bg-card border border-border/70 shadow-lg p-3 text-left animate-fade-in-only {pinned ? '' : 'pointer-events-none'}"
	style="left: {left}px; top: {top}px"
>
	<div class="flex items-start justify-between gap-2">
		<div class="text-sm font-medium text-foreground break-words min-w-0">{content.title}</div>
		<span class="flex-shrink-0 px-1.5 py-px rounded text-[10px] {content.kindClass}">{content.kind}</span>
	</div>
	<dl class="mt-2 space-y-1 text-xs">
		{#each content.rows as [label, value]}
			<div class="flex justify-between gap-3">
				<dt class="text-muted-foreground flex-shrink-0">{label}</dt>
				<dd class="text-foreground tabular-nums text-right min-w-0 truncate">{value}</dd>
			</div>
		{/each}
	</dl>
	{#if content.note}
		<p class="mt-2 pt-2 border-t border-border/60 text-xs text-muted-foreground leading-relaxed whitespace-pre-line break-words line-clamp-4">{content.note}</p>
	{/if}
	{#if pinned && content.action}
		<button type="button" onclick={content.action} class="mt-2.5 block w-full text-center text-xs py-1.5 rounded-lg bg-muted/60 text-foreground hover:bg-muted transition-colors">{content.linkLabel}</button>
	{:else if pinned && content.href}
		<a href={content.href} class="mt-2.5 block text-center text-xs py-1.5 rounded-lg bg-muted/60 text-foreground hover:bg-muted transition-colors">{content.linkLabel}</a>
	{/if}
	<!-- Pointer towards the marker -->
	<span
		class="absolute w-2.5 h-2.5 rotate-45 bg-card border-border/70 {below ? '-top-[6px] border-l border-t' : '-bottom-[6px] border-r border-b'}"
		style="left: {Math.min(Math.max(anchor.left + anchor.width / 2 - left - 5, 12), 228)}px"
	></span>
</div>
