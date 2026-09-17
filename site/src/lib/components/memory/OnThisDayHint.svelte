<script lang="ts">
	import { getOnThisDay, type OnThisDayEntry } from '$lib/api/diaries';
	import { formatDisplayDate } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	/** The day being viewed. Changing it reloads the lookback. */
	export let date: string;
	export let limit: number = 3;

	let entries: OnThisDayEntry[] = [];
	let expanded = false;
	let loadedDate = '';

	// The diary page swaps `date` in place rather than remounting, so the
	// lookback follows day navigation instead of loading once on mount.
	$: if (date && date !== loadedDate && typeof window !== 'undefined') {
		loadedDate = date;
		void load(date);
	}

	async function load(target: string) {
		// Collapse first: leaving yesterday's memories open over today's entry
		// would be jarring, and the heights differ.
		expanded = false;
		const result = await getOnThisDay(target, limit);
		// Ignore a response that lost the race against faster day navigation.
		if (target === loadedDate) {
			entries = result;
		}
	}

	function hintLabel(list: OnThisDayEntry[]): string {
		if (list.length === 1) {
			return $t('diaryOverview.onThisDayHintSingular', { n: list[0].yearsAgo });
		}
		return $t('diaryOverview.onThisDayHintPlural', { n: list.length });
	}

	function yearsAgoLabel(years: number): string {
		const key = years === 1 ? 'diaryOverview.yearsAgoSingular' : 'diaryOverview.yearsAgoPlural';
		return $t(key, { n: years });
	}
</script>

<!-- A single quiet line above the editor. Writing today should not be
     interrupted by the past, so nothing renders until there is a memory, and
     the previews stay collapsed until asked for. -->
{#if entries.length > 0}
	<div class="mb-3 animate-fade-in-only">
		<button
			type="button"
			on:click={() => (expanded = !expanded)}
			aria-expanded={expanded}
			title={expanded ? $t('diaryOverview.onThisDayCollapse') : $t('diaryOverview.onThisDayExpand')}
			class="w-full flex items-center gap-2 px-3 py-2 rounded-lg border border-border/40 bg-card/50 hover:bg-muted/50 transition-colors text-left"
		>
			<svg class="w-4 h-4 text-primary flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
					d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
			</svg>
			<span class="text-sm text-muted-foreground truncate">{hintLabel(entries)}</span>
			<svg
				class="w-4 h-4 text-muted-foreground flex-shrink-0 ml-auto transition-transform duration-200"
				class:rotate-180={expanded}
				fill="none"
				stroke="currentColor"
				viewBox="0 0 24 24"
			>
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
			</svg>
		</button>

		{#if expanded}
			<div class="mt-2 space-y-2 animate-fade-in-only">
				{#each entries as entry (entry.date)}
					<a
						href="/diary/{entry.date}"
						class="block p-3 rounded-lg border border-border/30 bg-card/50 hover:bg-muted/50 transition-colors"
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
