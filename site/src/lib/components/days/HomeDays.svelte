<script lang="ts">
	import { onMount } from 'svelte';
	import { listDays, type Day } from '$lib/api/days';
	import { countDay, sortDays } from '$lib/utils/days';
	import { getToday } from '$lib/utils/date';
	import { t } from '$lib/i18n';
	import DayCount from './DayCount.svelte';

	/** Days marked "show on home", as a row of tiles under the calendar. */
	let { class: className = '' }: { class?: string } = $props();
	let days: Day[] = $state([]);
	let loaded = $state(false);
	const today = getToday();

	let shown = $derived(
		sortDays(days.filter((day) => day.show_on_home), today).map((day) => ({ day, count: countDay(day, today) }))
	);

	onMount(async () => {
		try {
			days = await listDays();
		} catch {
			days = [];
		}
		loaded = true;
	});
</script>

<!-- Stays out of the layout until there is something to show. -->
{#if loaded && shown.length > 0}
	<section class="{className} animate-fade-in-only" aria-labelledby="home-days-heading">
		<div class="flex items-center justify-between mb-2.5 px-0.5">
			<h3 id="home-days-heading" class="text-sm font-medium text-foreground flex items-center gap-1.5">
				<svg class="w-4 h-4 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 3h12M6 21h12M7 3v3a5 5 0 0010 0V3M7 21v-3a5 5 0 0110 0v3" />
				</svg>
				{$t('days.title')}
			</h3>
			<a href="/days" class="text-xs text-muted-foreground hover:text-foreground transition-colors">{$t('days.all')} →</a>
		</div>
		<ul class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
			{#each shown as { day, count } (day.id)}
				<li>
					<a
						href="/days/{day.id}"
						class="group relative block h-full px-4 py-3 rounded-xl bg-card border border-border/50 shadow-sm hover:shadow-md hover:-translate-y-px transition-all overflow-hidden"
					>
						<span
							class="absolute inset-x-0 top-0 h-0.5 {count.direction === 'future' ? 'bg-amber-400/80' : count.direction === 'past' ? 'bg-sky-400/80' : 'bg-rose-400/80'}"
							aria-hidden="true"
						></span>
						<span class="flex items-center gap-1.5 min-w-0">
							<span class="text-base leading-none">{day.emoji || '📅'}</span>
							<span class="text-sm text-foreground truncate">{day.name}</span>
						</span>
						<span class="mt-2 flex items-end justify-between gap-2">
							<DayCount {count} size="sm" />
							<span class="text-[10px] text-muted-foreground tabular-nums pb-0.5 hidden sm:inline">{count.target.slice(5)}</span>
						</span>
					</a>
				</li>
			{/each}
		</ul>
	</section>
{/if}
