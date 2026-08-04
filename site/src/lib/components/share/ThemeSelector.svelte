<script lang="ts">
	import { themes, type ThemeId } from '$lib/utils/imageExport';

	export let selected: ThemeId = 'warm-paper';
	export let onSelect: (theme: ThemeId) => void = () => {};

	const themeList = Object.values(themes);
</script>

<div class="grid grid-cols-2 gap-2">
	{#each themeList as theme}
		<button
			type="button"
			on:click={() => onSelect(theme.id)}
			class="relative p-3 rounded-lg border-2 transition-all duration-200 text-left
				{selected === theme.id
					? 'border-primary ring-2 ring-primary/20'
					: 'border-border hover:border-primary/50'}"
		>
			{#if theme.variant === 'win95'}
				<!-- Miniature Notepad window on the teal desktop. -->
				<div class="w-full h-8 mb-2 p-1" style="background-color: #008080;">
					<div class="h-full p-[2px]" style="background-color: #c0c0c0; box-shadow: inset -1px -1px 0 0 #0a0a0a, inset 1px 1px 0 0 #ffffff;">
						<div class="h-[6px] flex items-center justify-end px-[1px]" style="background: linear-gradient(90deg, #000080, #1084d0);">
							<span class="w-[5px] h-[4px]" style="background-color: #c0c0c0;"></span>
						</div>
						<div class="mt-[1px] h-[calc(100%-7px)]" style="background-color: #ffffff; box-shadow: inset 1px 1px 0 0 #808080;">
							<div class="w-3/4 h-[2px] mt-[3px] ml-[3px]" style="background-color: #000000; opacity: 0.5;"></div>
							<div class="w-1/2 h-[2px] mt-[2px] ml-[3px]" style="background-color: #000000; opacity: 0.3;"></div>
						</div>
					</div>
				</div>
			{:else}
				<div
					class="w-full h-8 rounded mb-2"
					style="background-color: {theme.background}; border: 1px solid {theme.border};"
				>
					<div
						class="w-3/4 h-2 rounded mt-2 ml-2"
						style="background-color: {theme.foreground}; opacity: 0.3;"
					></div>
					<div
						class="w-1/2 h-1.5 rounded mt-1 ml-2"
						style="background-color: {theme.mutedForeground}; opacity: 0.3;"
					></div>
				</div>
			{/if}
			<div class="text-xs font-medium text-foreground">{theme.name}</div>
			{#if selected === theme.id}
				<div class="absolute top-2 right-2">
					<svg class="w-4 h-4 text-primary" fill="currentColor" viewBox="0 0 20 20">
						<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd" />
					</svg>
				</div>
			{/if}
		</button>
	{/each}
</div>
