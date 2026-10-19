<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import DayFormModal from '$lib/components/days/DayFormModal.svelte';
	import DayCount from '$lib/components/days/DayCount.svelte';
	import { listDays, type Day } from '$lib/api/days';
	import { isAuthenticated } from '$lib/api/client';
	import { countDay, sortDays } from '$lib/utils/days';
	import { getToday } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	let days: Day[] = $state([]);
	let loading = $state(true);
	let error = $state('');
	let creating = $state(false);
	const today = getToday();

	let sorted = $derived(sortDays(days, today).map((day) => ({ day, count: countDay(day, today) })));

	async function load() {
		loading = true;
		error = '';
		try {
			days = await listDays();
		} catch {
			error = $t('days.loadFailed');
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		load();
	});
</script>

<svelte:head>
	<title>{$t('days.title')} - Diarum</title>
</svelte:head>

<div class="min-h-screen bg-background flex flex-col">
	<PageHeader title={$t('days.title')} />

	<main class="flex-1 w-full max-w-4xl mx-auto px-4 py-6">
		<div class="flex items-end justify-between gap-4 mb-5">
			<div class="min-w-0">
				<h1 class="text-xl font-semibold text-foreground">{$t('days.title')}</h1>
				<p class="text-sm text-muted-foreground mt-0.5">{$t('days.subtitle')}</p>
			</div>
			<button
				type="button"
				onclick={() => (creating = true)}
				class="flex-shrink-0 inline-flex items-center gap-1.5 px-3.5 py-2 rounded-lg bg-primary text-primary-foreground text-sm font-medium shadow-sm hover:opacity-90 transition-opacity"
			>
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-width="2" d="M12 5v14M5 12h14" /></svg>
				{$t('days.new')}
			</button>
		</div>

		{#if loading}
			<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
				{#each Array(4) as _}<div class="h-[84px] rounded-xl bg-muted/40 animate-pulse"></div>{/each}
			</div>
		{:else if error}
			<div class="p-4 rounded-xl bg-destructive/10 text-destructive text-sm flex items-center justify-between">
				<span>{error}</span>
				<button type="button" class="underline" onclick={load}>{$t('days.retry')}</button>
			</div>
		{:else if sorted.length === 0}
			<button
				type="button"
				onclick={() => (creating = true)}
				class="w-full py-16 rounded-2xl border-2 border-dashed border-border hover:border-primary/40 hover:bg-muted/30 transition-colors flex flex-col items-center gap-3 text-muted-foreground"
			>
				<span class="text-4xl">⏳</span>
				<span class="text-sm max-w-xs">{$t('days.empty')}</span>
				<span class="text-sm font-medium text-primary">+ {$t('days.new')}</span>
			</button>
		{:else}
			<ul class="grid grid-cols-1 sm:grid-cols-2 gap-3 animate-fade-in-only">
				{#each sorted as { day, count } (day.id)}
					<li>
						<a
							href="/days/{day.id}"
							class="group relative flex items-center gap-3 h-full pl-4 pr-4 py-3.5 rounded-xl bg-card border border-border/50 shadow-sm hover:shadow-md hover:border-border transition-all overflow-hidden"
						>
							<span
								class="absolute left-0 inset-y-0 w-1 {count.direction === 'future' ? 'bg-amber-400/80' : count.direction === 'past' ? 'bg-sky-400/80' : 'bg-rose-400/80'}"
								aria-hidden="true"
							></span>
							<span class="w-10 h-10 flex-shrink-0 rounded-xl bg-muted/60 flex items-center justify-center text-xl">{day.emoji || '📅'}</span>
							<span class="min-w-0 flex-1">
								<span class="block text-sm font-medium text-foreground truncate">{day.name}</span>
								<span class="flex items-center gap-1.5 mt-0.5 text-[11px] text-muted-foreground">
									<span class="tabular-nums">{count.target}</span>
									{#if day.repeat}<span class="px-1.5 rounded bg-muted/70">↻ {$t(`days.repeats.${day.repeat}`)}</span>{/if}
									{#if day.show_on_home}<span class="px-1.5 rounded bg-primary/10 text-primary">{$t('days.onHome')}</span>{/if}
									{#if day.milestones > 0}<span class="px-1.5 rounded bg-muted/70">◆ {day.milestones}</span>{/if}
								</span>
							</span>
							<DayCount {count} />
						</a>
					</li>
				{/each}
			</ul>
		{/if}
	</main>

	<Footer maxWidth="6xl" />
</div>

{#if creating}
	<DayFormModal
		onclose={() => (creating = false)}
		onsaved={(day) => {
			creating = false;
			goto(`/days/${day.id}`);
		}}
	/>
{/if}
