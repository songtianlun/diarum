<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import DayFormModal from '$lib/components/days/DayFormModal.svelte';
	import DayCount from '$lib/components/days/DayCount.svelte';
	import TagChip from '$lib/components/days/TagChip.svelte';
	import TagManagerModal from '$lib/components/days/TagManagerModal.svelte';
	import DaysGantt from '$lib/components/days/DaysGantt.svelte';
	import { listDays, listDayTags, listAllMilestones, type Day, type DayTag, type Milestone } from '$lib/api/days';
	import { isAuthenticated } from '$lib/api/client';
	import { countDay, sortDays } from '$lib/utils/days';
	import { getToday } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	let days: Day[] = $state([]);
	let loading = $state(true);
	let error = $state('');
	let creating = $state(false);
	let tags: DayTag[] = $state([]);
	let managingTags = $state(false);
	// The tag the list is filtered by; null shows every day.
	let filter = $state<string | null>(null);
	type View = 'list' | 'gantt';
	const VIEW_KEY = 'diarum_days_view';
	let view = $state<View>('list');
	// Loaded the first time the Gantt view is shown.
	let milestones = $state<Milestone[] | null>(null);

	function setView(next: View) {
		view = next;
		try {
			localStorage.setItem(VIEW_KEY, next);
		} catch {
			// per-device preference only
		}
	}

	$effect(() => {
		if (view === 'gantt' && milestones === null) {
			milestones = [];
			listAllMilestones()
				.then((list) => (milestones = list))
				.catch(() => (milestones = []));
		}
	});
	const today = getToday();

	let tagById = $derived(new Map(tags.map((tag) => [tag.id, tag])));
	let visible = $derived(filter ? days.filter((day) => day.tags?.includes(filter!)) : days);
	let sorted = $derived(sortDays(visible, today).map((day) => ({ day, count: countDay(day, today) })));
	let tagCounts = $derived(
		days.reduce((counts, day) => {
			for (const id of day.tags ?? []) counts.set(id, (counts.get(id) ?? 0) + 1);
			return counts;
		}, new Map<string, number>())
	);

	async function load() {
		loading = true;
		error = '';
		try {
			[days, tags] = await Promise.all([listDays(), listDayTags()]);
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
		try {
			if (localStorage.getItem(VIEW_KEY) === 'gantt') view = 'gantt';
		} catch {
			// ignore
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
		<div class="flex items-center sm:items-end justify-between gap-4 mb-5">
			<div class="min-w-0">
				<h1 class="text-xl font-semibold text-foreground">{$t('days.title')}</h1>
				<p class="hidden sm:block text-sm text-muted-foreground mt-0.5">{$t('days.subtitle')}</p>
			</div>
			<div class="flex items-center gap-2 flex-shrink-0">
				<button
					type="button"
					onclick={() => (managingTags = true)}
					class="inline-flex items-center gap-1.5 px-3 py-2 rounded-lg bg-card border border-border/60 text-sm text-foreground hover:bg-muted/50 transition-colors"
				>
					<svg class="w-4 h-4 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 7h.01M7 3h5a1.99 1.99 0 011.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A1.994 1.994 0 013 12V7a4 4 0 014-4z" /></svg>
					{$t('days.tags.manage')}
				</button>
				<button
					type="button"
					onclick={() => (creating = true)}
					class="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-lg bg-primary text-primary-foreground text-sm font-medium shadow-sm hover:opacity-90 transition-opacity"
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-width="2" d="M12 5v14M5 12h14" /></svg>
					{$t('days.new')}
				</button>
			</div>
		</div>

		{#if !loading && days.length > 0}
		<div class="flex items-center gap-3 mb-4">
			<div class="flex-1 min-w-0 flex gap-1.5 overflow-x-auto -ml-4 pl-4 pb-1 pt-0.5 [scrollbar-width:none]" role="toolbar" aria-label={$t('days.tags.field')}>
				{#if tags.length > 0}
				<button
					type="button"
					onclick={() => (filter = null)}
					aria-pressed={filter === null}
					class="flex-shrink-0 px-3 py-1 rounded-full text-xs border transition-colors {filter === null ? 'bg-primary text-primary-foreground border-primary' : 'bg-card border-border/60 text-muted-foreground hover:text-foreground'}"
				>{$t('days.tags.all')} <span class="opacity-70 tabular-nums">{days.length}</span></button>
				{#each tags as tag (tag.id)}
					<button
						type="button"
						onclick={() => (filter = filter === tag.id ? null : tag.id)}
						aria-pressed={filter === tag.id}
						class="flex-shrink-0 inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs border transition-colors {filter === tag.id ? 'bg-primary text-primary-foreground border-primary' : 'bg-card border-border/60 text-muted-foreground hover:text-foreground'}"
					>
						<span class="w-1.5 h-1.5 rounded-full" style="background: {tag.color}"></span>
						{tag.name}
						<span class="opacity-70 tabular-nums">{tagCounts.get(tag.id) ?? 0}</span>
					</button>
				{/each}
				{/if}
			</div>
			<div class="flex-shrink-0 inline-grid grid-cols-2 p-0.5 rounded-lg bg-muted/60 text-xs">
				{#each [['list', 'days.view.list'], ['gantt', 'days.view.gantt']] as [key, label]}
					<button
						type="button"
						onclick={() => setView(key as View)}
						aria-pressed={view === key}
						class="px-3 py-1.5 rounded-md transition-colors {view === key ? 'bg-card text-foreground shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
					>{$t(label)}</button>
				{/each}
			</div>
		</div>
		{/if}

		{#if loading}
			<div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
				{#each Array(4) as _}<div class="h-[84px] rounded-xl bg-muted/40 animate-pulse"></div>{/each}
			</div>
		{:else if error}
			<div class="p-4 rounded-xl bg-destructive/10 text-destructive text-sm flex items-center justify-between">
				<span>{error}</span>
				<button type="button" class="underline" onclick={load}>{$t('days.retry')}</button>
			</div>
		{:else if days.length > 0 && sorted.length === 0}
			<div class="py-14 text-center text-sm text-muted-foreground">{$t('days.tags.noneMatch')}</div>
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
		{:else if view === 'gantt'}
			<div class="bg-card rounded-2xl border border-border/50 shadow-sm p-4 sm:p-5 animate-fade-in-only">
				<DaysGantt days={visible} milestones={milestones ?? []} {today} />
			</div>
		{:else}
			<ul class="grid grid-cols-1 sm:grid-cols-2 gap-3 animate-fade-in-only">
				{#each sorted as { day, count } (day.id)}
					<li>
						<a
							href="/days/{day.id}"
							class="group flex items-center gap-3 h-full px-4 py-3.5 rounded-xl bg-card border border-border/50 shadow-sm hover:shadow-md hover:border-border transition-all"
						>
							<span class="w-10 h-10 flex-shrink-0 rounded-xl bg-accent/70 flex items-center justify-center text-xl">{day.emoji || '📅'}</span>
							<span class="min-w-0 flex-1">
								<span class="block text-sm font-medium text-foreground truncate">{day.name}</span>
								<span class="flex flex-wrap items-center gap-x-1.5 gap-y-1 mt-0.5 text-[11px] text-muted-foreground whitespace-nowrap">
									<span class="tabular-nums">{count.target}</span>
									{#if day.repeat}<span class="px-1.5 rounded bg-muted/70">↻ {$t(`days.repeats.${day.repeat}`)}</span>{/if}
									{#if day.show_on_home}<span class="px-1.5 rounded bg-muted/70">{$t('days.onHome')}</span>{/if}
									{#if day.milestones > 0}<span class="px-1.5 rounded bg-muted/70">◆ {day.milestones}</span>{/if}
									{#if count.ended}<span class="px-1.5 rounded bg-foreground/5 text-foreground/70">■ {$t('days.endedShort')}</span>{/if}
								</span>
								{#if day.tags?.length}
									<span class="flex flex-wrap gap-1 mt-1.5">
										{#each day.tags as id (id)}
											{@const tag = tagById.get(id)}
											{#if tag}<TagChip {tag} size="xs" />{/if}
										{/each}
									</span>
								{/if}
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

{#if managingTags}
	<TagManagerModal
		bind:tags
		onclose={() => (managingTags = false)}
		ondeleted={(id) => {
			days = days.map((day) => ({ ...day, tags: day.tags.filter((x) => x !== id) }));
			if (filter === id) filter = null;
		}}
	/>
{/if}

{#if creating}
	<DayFormModal
		bind:tags
		onclose={() => (creating = false)}
		onsaved={(day) => {
			creating = false;
			goto(`/days/${day.id}`);
		}}
	/>
{/if}
