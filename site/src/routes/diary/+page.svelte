<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Calendar from '$lib/components/calendar/Calendar.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import { getDatesWithDiaries, getRecentDiaries } from '$lib/api/diaries';
	import { isAuthenticated } from '$lib/api/client';
	import { getMonthRange, formatDisplayDate } from '$lib/utils/date';

	let currentYear = new Date().getFullYear();
	let currentMonth = new Date().getMonth() + 1;
	let datesWithDiaries: string[] = [];
	let recentDiaries: Array<{ date: string; content: string }> = [];
	let loading = true;
	let calendarHeight: number | null = null;
	let calendarContainer: HTMLElement;

	async function loadDatesWithDiaries() {
		loading = true;
		const range = getMonthRange(currentYear, currentMonth);
		datesWithDiaries = await getDatesWithDiaries(range.start, range.end);
		loading = false;
	}

	async function loadRecentDiaries() {
		try {
			recentDiaries = await getRecentDiaries(5);
		} catch (e) {
			recentDiaries = [];
		}
	}

	function getPreview(content: string): string {
		const text = content.replace(/<[^>]*>/g, '').trim();
		return text.length > 80 ? text.slice(0, 80) + '...' : text;
	}

	function syncHeight() {
		if (calendarContainer && window.innerWidth >= 1024) {
			calendarHeight = calendarContainer.offsetHeight;
		} else {
			calendarHeight = null;
		}
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		loadDatesWithDiaries();
		loadRecentDiaries();

		// Sync height after content loads
		setTimeout(syncHeight, 100);
		window.addEventListener('resize', syncHeight);
		return () => window.removeEventListener('resize', syncHeight);
	});

	// Only run in browser, not during SSR
	$: {
		if (currentYear && currentMonth && typeof window !== 'undefined') {
			loadDatesWithDiaries();
		}
	}

	// Sync height when loading state changes
	$: if (!loading && typeof window !== 'undefined') {
		setTimeout(syncHeight, 50);
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
	<main class="max-w-6xl mx-auto px-4 py-6">
		<div class="flex flex-col lg:flex-row lg:items-start gap-6">
			<!-- Left: Calendar -->
			<div class="lg:w-[55%] xl:w-[50%]" bind:this={calendarContainer}>
				<div class="bg-card rounded-xl shadow-sm border border-border/50 p-6 animate-fade-in">
					{#if loading}
						<div class="flex flex-col items-center justify-center py-20 gap-3">
							<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
							</svg>
							<div class="text-muted-foreground text-sm">Loading...</div>
						</div>
					{:else}
						<Calendar bind:currentYear bind:currentMonth {datesWithDiaries} />
					{/if}
				</div>
			</div>

			<!-- Right: Stats and Recent Entries -->
			<div class="lg:w-[45%] xl:w-[50%] flex flex-col gap-4" style={calendarHeight ? `max-height: ${calendarHeight}px` : ''}>
				<!-- Stats -->
				<div class="grid grid-cols-3 gap-4">
					<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4 animate-fade-in" style="animation-delay: 100ms">
						<div class="text-xs text-muted-foreground">This month</div>
						<div class="text-xl font-bold text-foreground mt-1">
							{datesWithDiaries.length}
						</div>
					</div>

					<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4 animate-fade-in" style="animation-delay: 150ms">
						<div class="text-xs text-muted-foreground">Streak</div>
						<div class="text-xl font-bold text-foreground mt-1">-</div>
					</div>

					<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4 animate-fade-in" style="animation-delay: 200ms">
						<div class="text-xs text-muted-foreground">Total</div>
						<div class="text-xl font-bold text-foreground mt-1">-</div>
					</div>
				</div>

				<!-- Recent Entries -->
				<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4 animate-fade-in flex-1 overflow-hidden flex flex-col" style="animation-delay: 250ms">
					<h3 class="text-sm font-medium text-foreground mb-3">Recent Entries</h3>
					{#if recentDiaries.length > 0}
						<div class="space-y-2 overflow-y-auto flex-1">
							{#each recentDiaries as diary}
								<a
									href="/diary/{diary.date}"
									class="block p-3 rounded-lg hover:bg-muted/50 transition-all duration-200 border border-border/30"
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
						<div class="text-sm text-muted-foreground py-8 text-center">
							No entries yet. Start writing today!
						</div>
					{/if}
				</div>
			</div>
		</div>
	</main>

	<!-- Footer -->
	<Footer maxWidth="6xl" tagline="Your personal diary" />
</div>
