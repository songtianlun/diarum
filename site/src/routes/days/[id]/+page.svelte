<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { toPng } from 'html-to-image';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import DayCard from '$lib/components/days/DayCard.svelte';
	import DayFormModal from '$lib/components/days/DayFormModal.svelte';
	import DayTimeline from '$lib/components/days/DayTimeline.svelte';
	import DayPoster from '$lib/components/days/DayPoster.svelte';
	import ImageShareSheet from '$lib/components/days/ImageShareSheet.svelte';
	import { getDay, deleteDay, listDayTags, type Day, type DayTag, type Milestone } from '$lib/api/days';
	import { getToday } from '$lib/utils/date';
	import { isAuthenticated } from '$lib/api/client';
	import { t } from '$lib/i18n';

	let id = $derived($page.params.id ?? '');
	let day = $state<Day | null>(null);
	let loading = $state(true);
	let missing = $state(false);
	let editing = $state(false);
	let tags: DayTag[] = $state([]);
	// Milestones beyond the day itself; with any, they are listed beside the card.
	let milestones: Milestone[] = $state([]);
	let wide = $derived(milestones.length > 0);
	const today = getToday();
	let dayTags = $derived(day ? tags.filter((tag) => day!.tags?.includes(tag.id)) : []);
	// The poster exported as the image; only mounted while one is being made.
	let poster: HTMLDivElement | undefined = $state();
	let rendering = $state(false);
	// The image being shared, shown in a preview to copy or download.
	let sharing = $state<{ image: Promise<Blob>; filename: string } | null>(null);

	async function load() {
		loading = true;
		missing = false;
		try {
			const [data, allTags] = await Promise.all([getDay(id), listDayTags().catch(() => [] as DayTag[])]);
			day = data.day;
			milestones = data.milestones ?? [];
			tags = allTags;
		} catch {
			missing = true;
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

	async function render(): Promise<Blob> {
		rendering = true;
		try {
			await tick();
			if (!poster) throw new Error('poster missing');
			// Wait for web fonts so the image matches the page.
			await document.fonts?.ready;
			const url = await toPng(poster, { pixelRatio: 2, cacheBust: false });
			return await (await fetch(url)).blob();
		} finally {
			rendering = false;
		}
	}

	function share() {
		if (!day || rendering) return;
		sharing = { image: render(), filename: `${day.name}.png` };
	}

	async function remove() {
		if (!day || !confirm($t('days.deleteConfirm', { name: day.name }))) return;
		await deleteDay(day.id);
		goto('/days');
	}
</script>

<svelte:head>
	<title>{day?.name ?? $t('days.title')} - Diarum</title>
</svelte:head>

{#snippet actions(day: Day)}
	<button
		type="button"
		onclick={share}
		disabled={rendering}
		class="mt-5 w-full inline-flex items-center justify-center gap-1.5 py-2.5 rounded-xl bg-card border border-border/60 text-sm hover:bg-muted/50 disabled:opacity-60 transition-colors"
	>
		<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12v7a1 1 0 001 1h14a1 1 0 001-1v-7M16 6l-4-4m0 0L8 6m4-4v13" /></svg>
		{$t('days.share')}
	</button>

	<div class="mt-3 flex items-center justify-center gap-1 text-sm">
		<button type="button" onclick={() => (editing = true)} class="px-3 py-1.5 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors">{$t('days.edit')}</button>
		<span class="text-border">·</span>
		<button type="button" onclick={remove} class="px-3 py-1.5 rounded-lg text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors">{$t('days.delete')}</button>
	</div>
{/snippet}

<div class="min-h-screen bg-background flex flex-col">
	<PageHeader title={day?.name ?? $t('days.title')} />

	<main class="flex-1 w-full {wide ? 'max-w-md lg:max-w-5xl' : 'max-w-md'} mx-auto px-4 py-5 sm:py-8">
		<a href="/days" class="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors mb-4">
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" /></svg>
			{$t('days.back')}
		</a>

		{#if loading}
			<div class="h-72 rounded-2xl bg-muted/40 animate-pulse"></div>
		{:else if missing || !day}
			<div class="py-16 text-center text-sm text-muted-foreground">{$t('days.notFound')}</div>
		{:else}
			<div class="animate-fade-in-only">
				{#if wide}
					<div class="grid gap-4 lg:gap-6 lg:grid-cols-[26rem_minmax(0,1fr)] items-start">
						<div>
							<DayCard {day} tags={dayTags} />
							<div class="hidden lg:block">{@render actions(day)}</div>
						</div>
						<DayTimeline {day} {milestones} {today} />
					</div>
					<div class="lg:hidden">{@render actions(day)}</div>
				{:else}
					<DayCard {day} tags={dayTags} />
					<!-- No milestones yet: the same title, leading to the milestone page to add one -->
					<a
						href="/days/{day.id}/milestones"
						class="group mt-4 flex items-center gap-3 px-4 py-3 rounded-2xl bg-card border border-dashed border-border hover:border-primary/40 hover:bg-muted/30 transition-colors"
					>
						<svg class="w-4 h-4 flex-shrink-0 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 21V4m0 0h11l-1.5 4L15 12H4" /></svg>
						<span class="min-w-0 flex-1">
							<span class="block text-sm font-medium text-foreground">{$t('days.milestones.title')}</span>
							<span class="block text-xs text-muted-foreground truncate">{$t('days.milestones.empty')}</span>
						</span>
						<svg class="w-4 h-4 flex-shrink-0 text-muted-foreground group-hover:text-foreground group-hover:translate-x-0.5 transition-transform" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
					</a>
					{@render actions(day)}
				{/if}
			</div>
		{/if}
	</main>

	<Footer maxWidth="6xl" />
</div>

{#if rendering && day}
	<!-- Off screen: laid out for the image, not the page -->
	<div class="fixed top-0 left-[-10000px] pointer-events-none" aria-hidden="true" inert>
		<DayPoster {day} tags={dayTags} {milestones} {today} bind:element={poster} />
	</div>
{/if}

{#if sharing && day}
	<ImageShareSheet title={day.name} image={sharing.image} filename={sharing.filename} onclose={() => (sharing = null)} />
{/if}

{#if editing && day}
	<DayFormModal
		{day}
		bind:tags
		onclose={() => (editing = false)}
		onsaved={(saved) => {
			editing = false;
			day = saved;
		}}
	/>
{/if}
