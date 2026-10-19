<script lang="ts">
	import { onMount, type Snippet } from 'svelte';

	/** A bottom sheet on phones, a centred dialog from `sm` up; `wide` for image previews. */
	let {
		title,
		onclose,
		wide = false,
		children,
		footer
	}: { title: string; onclose: () => void; wide?: boolean; children: Snippet; footer?: Snippet } = $props();

	let panel: HTMLDivElement | undefined = $state();

	onMount(() => {
		const previous = document.body.style.overflow;
		document.body.style.overflow = 'hidden';
		panel?.focus();
		const onKey = (event: KeyboardEvent) => {
			if (event.key === 'Escape') onclose();
		};
		window.addEventListener('keydown', onKey);
		return () => {
			document.body.style.overflow = previous;
			window.removeEventListener('keydown', onKey);
		};
	});
</script>

<div class="fixed inset-0 z-50 flex items-end sm:items-center justify-center" role="presentation">
	<button type="button" class="absolute inset-0 bg-black/40 backdrop-blur-[2px] animate-fade-in cursor-default" aria-label="Close" onclick={onclose}></button>
	<div
		bind:this={panel}
		tabindex="-1"
		role="dialog"
		aria-modal="true"
		aria-label={title}
		class="relative w-full {wide ? 'sm:max-w-2xl' : 'sm:max-w-md'} max-h-[92vh] flex flex-col bg-card rounded-t-2xl sm:rounded-2xl shadow-xl border border-border/50 outline-none animate-fade-in"
	>
		<div class="sm:hidden mx-auto mt-2 h-1 w-10 rounded-full bg-muted-foreground/30" aria-hidden="true"></div>
		<header class="flex items-center justify-between gap-3 px-5 pt-3 sm:pt-4 pb-3 border-b border-border/50">
			<h3 class="text-base font-semibold text-foreground truncate">{title}</h3>
			<button type="button" onclick={onclose} class="p-1.5 -mr-1.5 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60" aria-label="Close">
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
			</button>
		</header>
		<div class="overflow-y-auto overscroll-contain px-5 py-4">
			{@render children()}
		</div>
		{#if footer}
			<footer class="flex items-center gap-2 px-5 py-3 border-t border-border/50 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
				{@render footer()}
			</footer>
		{/if}
	</div>
</div>
