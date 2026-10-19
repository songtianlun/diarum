<script lang="ts">
	import { onMount } from 'svelte';
	import { fade } from 'svelte/transition';
	import { getOnThisDay, type OnThisDayEntry } from '$lib/api/diaries';
	import { getToday, formatDisplayDate } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	/** The day to look back from. Defaults to today. */
	export let date: string = getToday();
	/** How many earlier years to page through; only one is shown at a time. */
	export let limit: number = 3;

	let entries: OnThisDayEntry[] = [];
	let loading = true;
	// One memory at a time, so a good year never squeezes the cards below it.
	let index = 0;
	let touchX: number | null = null;

	$: current = entries[index];

	function step(delta: number) {
		if (entries.length < 2) return;
		index = (index + delta + entries.length) % entries.length;
	}

	function onTouchStart(event: TouchEvent) {
		touchX = event.touches[0]?.clientX ?? null;
	}

	function onTouchEnd(event: TouchEvent) {
		if (touchX === null) return;
		const dx = (event.changedTouches[0]?.clientX ?? touchX) - touchX;
		touchX = null;
		if (Math.abs(dx) > 40) step(dx < 0 ? 1 : -1);
	}

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
		<div class="flex items-center justify-between gap-2 mb-3">
			<h3 class="text-sm font-medium text-foreground flex items-center gap-1.5">
				<svg class="w-4 h-4 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
						d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
				</svg>
				{$t('diaryOverview.onThisDay')}
			</h3>
			{#if entries.length > 1}
				<div class="flex items-center gap-0.5 -mr-1.5 text-muted-foreground">
					<button type="button" class="p-1 rounded-md hover:bg-muted/60 hover:text-foreground transition-colors" on:click={() => step(-1)} aria-label={$t('diaryOverview.onThisDayPrev')}>
						<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M15 19l-7-7 7-7" /></svg>
					</button>
					<span class="text-[11px] tabular-nums min-w-[2.25rem] text-center">{index + 1} / {entries.length}</span>
					<button type="button" class="p-1 rounded-md hover:bg-muted/60 hover:text-foreground transition-colors" on:click={() => step(1)} aria-label={$t('diaryOverview.onThisDayNext')}>
						<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M9 5l7 7-7 7" /></svg>
					</button>
				</div>
			{/if}
		</div>

		{#if loading}
			<div class="py-6 flex items-center justify-center">
				<span class="inline-block w-4 h-4 border-2 border-primary/30 border-t-primary rounded-full animate-spin"></span>
			</div>
		{:else if current}
			<div class="grid animate-fade-in-only" role="group" on:touchstart|passive={onTouchStart} on:touchend={onTouchEnd}>
				{#key current.date}
					<a
						href="/diary/{current.date}"
						class="[grid-area:1/1] block p-3 rounded-lg hover:bg-muted/50 transition-colors border border-border/30"
						in:fade={{ duration: 180 }}
						out:fade={{ duration: 120 }}
					>
						<div class="flex items-center gap-2 mb-1">
							<span class="text-xs font-medium text-primary flex-shrink-0">
								{yearsAgoLabel(current.yearsAgo)}
							</span>
							<span class="text-xs text-muted-foreground truncate">
								· {formatDisplayDate(current.date)}
							</span>
							{#if current.mood || current.weather}
								<span class="ml-auto flex items-center gap-1 flex-shrink-0 text-xs">
									{#if current.weather}
										<span class="emoji-chip" title={$t('calendar.weatherLabel', { value: current.weather })}>{current.weather}</span>
									{/if}
									{#if current.mood}
										<span class="emoji-chip" title={$t('calendar.moodLabel', { value: current.mood })}>{current.mood}</span>
									{/if}
								</span>
							{/if}
						</div>
						<div class="text-sm text-foreground line-clamp-2 min-h-[2.5rem]">
							{current.preview}
						</div>
					</a>
				{/key}
			</div>
			{#if entries.length > 1}
				<div class="flex justify-center gap-1 mt-2.5" aria-hidden="true">
					{#each entries as entry, i (entry.date)}
						<button
							type="button"
							tabindex="-1"
							aria-label="{i + 1} / {entries.length}"
							class="h-1.5 rounded-full transition-all {i === index ? 'w-4 bg-primary/70' : 'w-1.5 bg-muted-foreground/25 hover:bg-muted-foreground/40'}"
							on:click={() => (index = i)}
						></button>
					{/each}
				</div>
			{/if}
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
