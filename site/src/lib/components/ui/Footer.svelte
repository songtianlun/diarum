<script lang="ts">
	import { onMount, type Snippet } from 'svelte';
	import ThemeToggle from './ThemeToggle.svelte';

	let { maxWidth = '6xl', tagline = '' }: { maxWidth?: string; tagline?: string } = $props();

	let version = $state('');

	onMount(() => {
		fetchVersion();
	});

	async function fetchVersion() {
		try {
			const res = await fetch('/api/version');
			if (res.ok) {
				const data = await res.json();
				version = data.version;
			}
		} catch (e) {
			// Silently fail
		}
	}

	const maxWidthClasses: Record<string, string> = {
		'md': 'max-w-md',
		'3xl': 'max-w-3xl',
		'6xl': 'max-w-6xl'
	};
	let maxWidthClass = $derived(maxWidthClasses[maxWidth] || 'max-w-6xl');
</script>

<footer class="border-t border-border/50 mt-auto">
	<div class="{maxWidthClass} mx-auto px-4 py-3">
		<div class="flex items-center justify-between">
			<div class="flex items-center gap-1.5 text-xs text-muted-foreground">
				{#if tagline}
					<span>{tagline}</span>
					<span class="text-border">·</span>
				{/if}
				<span>© {new Date().getFullYear()} Diaria</span>
				{#if version}
					<span class="text-border">·</span>
					<span class="font-mono text-[10px] px-1.5 py-0.5 bg-primary/10 text-primary/80 dark:bg-primary/15 dark:text-primary/90 rounded border border-primary/20">{version}</span>
				{/if}
			</div>
			<ThemeToggle />
		</div>
	</div>
</footer>
