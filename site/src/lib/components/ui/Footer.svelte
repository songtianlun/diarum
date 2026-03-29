<script lang="ts">
	import { onMount } from 'svelte';
	import ThemeToggle from './ThemeToggle.svelte';

	let {
		maxWidth = '6xl',
		tagline = '',
		dynamicMaxWidth = '',
		dynamicMaxWidthDesktop = ''
	}: {
		maxWidth?: string;
		tagline?: string;
		dynamicMaxWidth?: string;
		dynamicMaxWidthDesktop?: string;
	} = $props();

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

	const maxWidthValues: Record<string, string> = {
		'md': '28rem',
		'3xl': '48rem',
		'6xl': '72rem'
	};
	let fallbackMaxWidth = $derived(maxWidthValues[maxWidth] || '72rem');
	let mobileMaxWidth = $derived(dynamicMaxWidth || fallbackMaxWidth);
	let desktopMaxWidth = $derived(dynamicMaxWidthDesktop || dynamicMaxWidth || fallbackMaxWidth);
</script>

<footer class="border-t border-border/50 mt-auto">
	<div
		class="footer-inner mx-auto px-4 py-3 transition-all duration-300"
		style={`--footer-mobile-max-width: ${mobileMaxWidth}; --footer-desktop-max-width: ${desktopMaxWidth};`}
	>
		<div class="flex flex-row items-center justify-center sm:justify-between gap-2 sm:gap-4 flex-wrap">
			<div class="flex flex-wrap items-center justify-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
				{#if tagline}
					<span class="whitespace-nowrap">{tagline}</span>
				{/if}
				<span class="whitespace-nowrap">© {new Date().getFullYear()} Diarum</span>
				{#if version}
					<a href="https://github.com/songtianlun/diarum" target="_blank" rel="noopener noreferrer" class="font-mono text-[10px] text-muted-foreground/70 whitespace-nowrap hover:text-foreground transition-colors">{version}</a>
				{/if}
			</div>
			<ThemeToggle />
		</div>
	</div>
</footer>

<style>
	.footer-inner {
		max-width: var(--footer-mobile-max-width);
	}

	@media (min-width: 1024px) {
		.footer-inner {
			max-width: var(--footer-desktop-max-width);
		}
	}
</style>
