<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Calendar from '$lib/components/calendar/Calendar.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import { getDatesWithDiaries, getRecentDiaries, getDiaryStats } from '$lib/api/diaries';
	import { isAuthenticated } from '$lib/api/client';
	import { getMonthRange, formatDisplayDate } from '$lib/utils/date';

	let currentYear = new Date().getFullYear();
	let currentMonth = new Date().getMonth() + 1;
	let datesWithDiaries: string[] = [];
	let recentDiaries: Array<{ date: string; content: string }> = [];
	let stats: { streak: number; total: number } | null = null;
	let loading = true;
	let recentLoading = true;
	let statsLoading = true;
	let mounted = false;
	let prevYear = currentYear;
	let prevMonth = currentMonth;

	async function loadDatesWithDiaries() {
		loading = true;
		const range = getMonthRange(currentYear, currentMonth);
		datesWithDiaries = await getDatesWithDiaries(range.start, range.end);
		loading = false;
	}

	async function loadRecentDiaries() {
		recentLoading = true;
		try {
			recentDiaries = await getRecentDiaries(5);
		} catch (e) {
			recentDiaries = [];
		}
		recentLoading = false;
	}

	async function loadStats() {
		statsLoading = true;
		stats = await getDiaryStats();
		statsLoading = false;
	}

	function getPreview(content: string): string {
		const text = content.replace(/<[^>]*>/g, '').trim();
		return text.length > 80 ? text.slice(0, 80) + '...' : text;
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		loadDatesWithDiaries();
		loadRecentDiaries();
		loadStats();
		mounted = true;
	});

	$: {
		if (mounted && (currentYear !== prevYear || currentMonth !== prevMonth)) {
			prevYear = currentYear;
			prevMonth = currentMonth;
			loadDatesWithDiaries();
		}
	}
</script>

<svelte:head>
	<title>Calendar - Diaria</title>
</svelte:head>

<div class="min-h-screen bg-background">
	<!-- Header -->
	<header class="glass border-b border-border/50 sticky top-0 z-20">
		<div class="max-w-6xl mx-auto px-4 h-11">
			<div class="flex items-center justify-between h-full">
				<a href="/" class="text-lg font-semibold text-foreground hover:text-primary transition-colors">Diaria</a>

				<div class="flex items-center gap-2">
					<a
						href="/media"
						class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
						title="Media Library"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
						</svg>
					</a>

					<a
						href="/search"
						class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
						title="Search"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
						</svg>
					</a>

					<a
						href="/assistant"
						class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
						title="AI Assistant"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
						</svg>
					</a>

					<a
						href="/settings"
						class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
						title="Settings"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
						</svg>
					</a>

					<a
						href="/diary/{new Date().toISOString().split('T')[0]}"
						class="px-3 py-1.5 text-sm bg-primary text-primary-foreground rounded-lg hover:opacity-90 transition-all duration-200"
					>
						Today
					</a>
				</div>
			</div>
		</div>
	</header>

	<!-- Calendar -->
	<main class="max-w-5xl mx-auto px-4 py-6">
		<div class="flex flex-col lg:flex-row gap-6 lg:h-[540px]">
			<!-- Left: Calendar -->
			<div class="lg:flex-1 lg:min-w-0">
				<div class="bg-card rounded-xl shadow-sm border border-border/50 p-5 h-full relative overflow-hidden">
					{#if loading}
						<div class="absolute inset-0 flex flex-col items-center justify-center gap-3">
							<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
							</svg>
							<div class="text-muted-foreground text-sm">Loading...</div>
						</div>
					{:else}
						<div class="animate-fade-in-only">
							<Calendar bind:currentYear bind:currentMonth {datesWithDiaries} />
						</div>
					{/if}
				</div>
			</div>

			<!-- Right: Stats and Recent Entries -->
			<div class="lg:w-[340px] xl:w-[380px] flex flex-col gap-4 flex-shrink-0">
				<!-- Stats -->
				<div class="grid grid-cols-3 gap-4">
					<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4">
						<div class="text-xs text-muted-foreground">This month</div>
						<div class="text-xl font-bold text-foreground mt-1 h-7 flex items-center">
							{#if loading}
								<span class="inline-block w-4 h-4 border-2 border-primary/30 border-t-primary rounded-full animate-spin"></span>
							{:else}
								<span class="animate-fade-in-only">{datesWithDiaries.length}</span>
							{/if}
						</div>
					</div>

					<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4">
						<div class="text-xs text-muted-foreground">Streak</div>
						<div class="text-xl font-bold text-foreground mt-1 h-7 flex items-center">
							{#if statsLoading}
								<span class="inline-block w-4 h-4 border-2 border-primary/30 border-t-primary rounded-full animate-spin"></span>
							{:else}
								<span class="animate-fade-in-only">{stats?.streak ?? 0}</span>
							{/if}
						</div>
					</div>

					<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4">
						<div class="text-xs text-muted-foreground">Total</div>
						<div class="text-xl font-bold text-foreground mt-1 h-7 flex items-center">
							{#if statsLoading}
								<span class="inline-block w-4 h-4 border-2 border-primary/30 border-t-primary rounded-full animate-spin"></span>
							{:else}
								<span class="animate-fade-in-only">{stats?.total ?? 0}</span>
							{/if}
						</div>
					</div>
				</div>

				<!-- Recent Entries -->
				<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4 flex-1 min-h-0 flex flex-col overflow-hidden">
					<h3 class="text-sm font-medium text-foreground mb-3">Recent Entries</h3>
					{#if recentLoading}
						<div class="flex-1 flex flex-col items-center justify-center gap-3">
							<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
							</svg>
							<div class="text-muted-foreground text-sm">Loading...</div>
						</div>
					{:else if recentDiaries.length > 0}
						<div class="space-y-2 overflow-y-auto flex-1 animate-fade-in-only">
							{#each recentDiaries as diary}
								<a
									href="/diary/{diary.date}"
									class="block p-3 rounded-lg hover:bg-muted/50 transition-colors border border-border/30"
								>
									<div class="text-xs text-muted-foreground mb-1">
										{formatDisplayDate(diary.date)}
									</div>
									<div class="text-sm text-foreground line-clamp-2">
										{getPreview(diary.content)}
									</div>
								</a>
							{/each}
						</div>
					{:else}
						<div class="flex-1 flex items-center justify-center animate-fade-in-only">
							<div class="text-sm text-muted-foreground text-center">
								No entries yet. Start writing today!
							</div>
						</div>
					{/if}
				</div>
			</div>
		</div>
	</main>

	<!-- Footer -->
	<Footer maxWidth="6xl" tagline="Your personal diary" />
</div>
