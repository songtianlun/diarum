<script lang="ts">
	import { onMount } from 'svelte';
	import { getOnThisDay, type OnThisDayEntry } from '$lib/api/diaries';
	import { getToday, formatDisplayDate } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	/** The day to look back from. Defaults to today. */
	export let date: string = getToday();
	/** Keep the card short enough to sit beside the calendar without scrolling. */
	export let limit: number = 3;

	let entries: OnThisDayEntry[] = [];
	let loading = true;

	onMount(async () => {
		entries = await getOnThisDay(date, limit);
		loading = false;
	});

	function yearsAgoLabel(years: number): string {
		const key = years === 1 ? 'diaryOverview.yearsAgoSingular' : 'diaryOverview.yearsAgoPlural';
		return $t(key, { n: years });
	}
</script>

<!-- Stays out of the layout entirely until there is a memory to show, so a new
     user never sees an empty card wondering what it is for. -->
{#if loading || entries.length > 0}
	<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4 flex flex-col min-h-0 flex-shrink-0">
		<h3 class="text-sm font-medium text-foreground mb-3 flex items-center gap-1.5">
			<svg class="w-4 h-4 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
					d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
			</svg>
			{$t('diaryOverview.onThisDay')}
		</h3>

		{#if loading}
			<div class="py-6 flex items-center justify-center">
				<span class="inline-block w-4 h-4 border-2 border-primary/30 border-t-primary rounded-full animate-spin"></span>
			</div>
		{:else}
			<div class="space-y-2 overflow-y-auto animate-fade-in-only">
				{#each entries as entry (entry.date)}
					<a
						href="/diary/{entry.date}"
						class="block p-3 rounded-lg hover:bg-muted/50 transition-colors border border-border/30"
					>
						<div class="flex items-center gap-2 mb-1">
							<span class="text-xs font-medium text-primary flex-shrink-0">
								{yearsAgoLabel(entry.yearsAgo)}
							</span>
							<span class="text-xs text-muted-foreground truncate">
								· {formatDisplayDate(entry.date)}
							</span>
							{#if entry.mood || entry.weather}
								<span class="ml-auto flex items-center gap-1 flex-shrink-0 text-xs">
									{#if entry.weather}
										<span class="emoji-chip" title={$t('calendar.weatherLabel', { value: entry.weather })}>{entry.weather}</span>
									{/if}
									{#if entry.mood}
										<span class="emoji-chip" title={$t('calendar.moodLabel', { value: entry.mood })}>{entry.mood}</span>
									{/if}
								</span>
							{/if}
						</div>
						<div class="text-sm text-foreground line-clamp-2">
							{entry.preview}
						</div>
					</a>
				{/each}
			</div>
		{/if}
	</div>
{/if}

<style>
	.emoji-chip {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		padding: 0.1rem 0.25rem;
		border-radius: 999px;
		background: color-mix(in srgb, var(--muted) 75%, transparent);
	}
</style>
