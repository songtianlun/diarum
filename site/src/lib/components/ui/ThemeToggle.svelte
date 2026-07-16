<script lang="ts">
	import { theme, type Theme } from '$lib/stores/theme';
	import { t } from '$lib/i18n';

	$: themes = [
		{ value: 'light' as Theme, label: $t('theme.light'), icon: 'sun' },
		{ value: 'dark' as Theme, label: $t('theme.dark'), icon: 'moon' },
		{ value: 'system' as Theme, label: $t('theme.system'), icon: 'monitor' }
	];

	let isOpen = false;

	function selectTheme(t: Theme) {
		theme.set(t);
		isOpen = false;
	}

	function handleClickOutside(event: MouseEvent) {
		const target = event.target as HTMLElement;
		if (!target.closest('.theme-toggle')) {
			isOpen = false;
		}
	}
</script>

<svelte:window on:click={handleClickOutside} />

<div class="theme-toggle relative">
	<button
		on:click|stopPropagation={() => (isOpen = !isOpen)}
		class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-sm
			   text-muted-foreground hover:text-foreground hover:bg-muted/50
			   transition-all duration-200"
		aria-label={$t('theme.toggle')}
	>
		{#if $theme === 'light'}
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
					d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
			</svg>
		{:else if $theme === 'dark'}
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
					d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
			</svg>
		{:else}
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
					d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
			</svg>
		{/if}
		<span class="hidden sm:inline">{themes.find(t => t.value === $theme)?.label}</span>
	</button>

	{#if isOpen}
		<div class="absolute bottom-full right-0 mb-2 py-1 min-w-[120px]
					bg-card border border-border rounded-lg shadow-lg
					animate-slide-in-down z-50">
			{#each themes as t}
				<button
					on:click|stopPropagation={() => selectTheme(t.value)}
					class="w-full flex items-center gap-2 px-3 py-1.5 text-sm
						   hover:bg-muted/50 transition-colors
						   {$theme === t.value ? 'text-primary font-medium' : 'text-muted-foreground'}"
				>
					{#if t.value === 'light'}
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
						</svg>
					{:else if t.value === 'dark'}
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
						</svg>
					{:else}
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
						</svg>
					{/if}
					{t.label}
				</button>
			{/each}
		</div>
	{/if}
</div>
