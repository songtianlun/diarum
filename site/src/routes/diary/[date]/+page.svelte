<script lang="ts">
	import { onMount } from 'svelte';
	import ClassicDiaryView from '$lib/components/diary/ClassicDiaryView.svelte';
	import BookDiaryView from '$lib/components/book/BookDiaryView.svelte';
	import Win95DiaryView from '$lib/components/win95/Win95DiaryView.svelte';
	import { getGeneralSettings, type VisualStylePreference } from '$lib/api/settings';
	import { t } from '$lib/i18n';

	// Which entry view to render — resolved once from the user's saved
	// preference (Settings → General → Visual Style) before anything mounts.
	let visualStyle: VisualStylePreference | null = null;

	onMount(async () => {
		try {
			const settings = await getGeneralSettings();
			visualStyle = settings.visual_style;
		} catch {
			visualStyle = 'classic';
		}
	});
</script>

{#if visualStyle === 'immersive'}
	<BookDiaryView />
{:else if visualStyle === 'win95'}
	<Win95DiaryView />
{:else if visualStyle === 'classic'}
	<ClassicDiaryView />
{:else}
	<div class="flex flex-col items-center justify-center min-h-screen gap-3 bg-background">
		<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
			<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
			<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
		</svg>
		<div class="text-muted-foreground text-sm">{$t('common.loading')}</div>
	</div>
{/if}
