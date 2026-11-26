<script lang="ts">
	import { createEventDispatcher } from 'svelte';
	import { t } from '$lib/i18n';

	const dispatch = createEventDispatcher<{ prompt: string }>();

	const prompts = [
		{
			titleKey: 'assistant.prompt1Title',
			descKey: 'assistant.prompt1Desc',
			textKey: 'assistant.prompt1Text',
			icon: 'M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z'
		},
		{
			titleKey: 'assistant.prompt2Title',
			descKey: 'assistant.prompt2Desc',
			textKey: 'assistant.prompt2Text',
			icon: 'M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z'
		},
		{
			titleKey: 'assistant.prompt3Title',
			descKey: 'assistant.prompt3Desc',
			textKey: 'assistant.prompt3Text',
			icon: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z'
		},
		{
			titleKey: 'assistant.prompt4Title',
			descKey: 'assistant.prompt4Desc',
			textKey: 'assistant.prompt4Text',
			icon: 'M5 3v4M3 5h4M6 17v4m-2-2h4m5-16l2.286 6.857L21 12l-5.714 2.143L13 21l-2.286-6.857L5 12l5.714-2.143L13 3z'
		}
	];
</script>

<div class="flex flex-col items-center justify-center min-h-full py-8 px-4 max-w-2xl mx-auto">
	<div class="w-12 h-12 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center mb-4">
		<svg class="w-6 h-6 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
			<path
				stroke-linecap="round"
				stroke-linejoin="round"
				stroke-width="1.75"
				d="M5 3v4M3 5h4M6 17v4m-2-2h4m5-16l2.286 6.857L21 12l-5.714 2.143L13 21l-2.286-6.857L5 12l5.714-2.143L13 3z"
			/>
		</svg>
	</div>

	<h2 class="text-xl sm:text-2xl font-semibold text-foreground text-center tracking-tight">
		{$t('assistant.welcomeTitle')}
	</h2>
	<p class="text-sm text-muted-foreground text-center mt-2 max-w-md leading-relaxed">
		{$t('assistant.welcomeSubtitle')}
	</p>

	<div class="grid grid-cols-1 sm:grid-cols-2 gap-2.5 w-full mt-7">
		{#each prompts as item (item.titleKey)}
			<button
				type="button"
				on:click={() => dispatch('prompt', $t(item.textKey))}
				class="group text-left p-3.5 rounded-2xl border border-border/70 bg-background/70
					hover:bg-muted/50 hover:border-primary/35 transition-all duration-150"
			>
				<div class="flex items-center gap-2 mb-1">
					<svg class="w-4 h-4 text-primary/80 group-hover:text-primary flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d={item.icon} />
					</svg>
					<span class="text-sm font-medium text-foreground">{$t(item.titleKey)}</span>
				</div>
				<p class="text-xs text-muted-foreground leading-relaxed line-clamp-2">
					{$t(item.descKey)}
				</p>
			</button>
		{/each}
	</div>
</div>
