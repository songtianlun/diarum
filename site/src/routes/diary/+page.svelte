<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Calendar from '$lib/components/calendar/Calendar.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import { getDatesWithDiaries } from '$lib/api/diaries';
	import { isAuthenticated } from '$lib/api/client';
	import { getMonthRange } from '$lib/utils/date';

	let currentYear = new Date().getFullYear();
	let currentMonth = new Date().getMonth() + 1;
	let datesWithDiaries: string[] = [];
	let loading = true;

	async function loadDatesWithDiaries() {
		loading = true;
		const range = getMonthRange(currentYear, currentMonth);
		datesWithDiaries = await getDatesWithDiaries(range.start, range.end);
		loading = false;
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		loadDatesWithDiaries();
	});

	// Only run in browser, not during SSR
	$: {
		if (currentYear && currentMonth && typeof window !== 'undefined') {
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

		<!-- Stats -->
		<div class="mt-6 grid grid-cols-1 md:grid-cols-3 gap-4">
			<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4 animate-fade-in" style="animation-delay: 100ms">
				<div class="text-sm text-muted-foreground">Entries this month</div>
				<div class="text-2xl font-bold text-foreground mt-1">
					{datesWithDiaries.length}
				</div>
			</div>

			<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4 animate-fade-in opacity-0" style="animation-delay: 150ms">
				<div class="text-sm text-muted-foreground">Current streak</div>
				<div class="text-2xl font-bold text-foreground mt-1">-</div>
				<div class="text-xs text-muted-foreground/70 mt-1">Coming soon</div>
			</div>

			<div class="bg-card rounded-xl shadow-sm border border-border/50 p-4 animate-fade-in opacity-0" style="animation-delay: 200ms">
				<div class="text-sm text-muted-foreground">Total entries</div>
				<div class="text-2xl font-bold text-foreground mt-1">-</div>
				<div class="text-xs text-muted-foreground/70 mt-1">Coming soon</div>
			</div>
		</div>
	</main>

	<!-- Footer -->
	<Footer maxWidth="6xl" tagline="Your personal diary" />
</div>
