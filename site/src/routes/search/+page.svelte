<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import ThemeToggle from '$lib/components/ui/ThemeToggle.svelte';
	import { searchDiaries } from '$lib/api/diaries';
	import { isAuthenticated } from '$lib/api/client';

	let query = '';
	let results: any[] = [];
	let loading = false;
	let searched = false;

	async function handleSearch() {
		if (!query.trim()) return;
		
		loading = true;
		searched = true;
		results = await searchDiaries(query);
		loading = false;
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			handleSearch();
		}
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
	});
</script>

<svelte:head>
	<title>Search - Diaria</title>
</svelte:head>

<div class="min-h-screen bg-background">
	<!-- Header -->
	<header class="glass border-b border-border/50 sticky top-0 z-20">
		<div class="max-w-6xl mx-auto px-4 h-11">
			<div class="flex items-center justify-between h-full">
				<a href="/diary" class="text-lg font-semibold text-foreground hover:opacity-80 transition-opacity">
					← Diaria
				</a>
				<ThemeToggle />
			</div>
		</div>
	</header>

	<!-- Search -->
	<main class="max-w-2xl mx-auto px-4 py-8">
		<div class="bg-card rounded-xl shadow-sm border border-border/50 p-6 animate-fade-in">
			<h1 class="text-xl font-semibold text-foreground mb-4">Search Diaries</h1>
			
			<div class="flex gap-2">
				<input
					type="text"
					bind:value={query}
					on:keydown={handleKeydown}
					placeholder="Search your diary entries..."
					class="flex-1 px-4 py-2 rounded-lg border border-border bg-background text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary/50"
				/>
				<button
					on:click={handleSearch}
					disabled={loading || !query.trim()}
					class="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:opacity-90 transition-opacity disabled:opacity-50"
				>
					{#if loading}
						<svg class="w-5 h-5 animate-spin" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
						</svg>
					{:else}
						Search
					{/if}
				</button>
			</div>
		</div>

		<!-- Results -->
		{#if searched}
			<div class="mt-6">
				{#if results.length === 0}
					<div class="text-center text-muted-foreground py-8">
						No entries found for "{query}"
					</div>
				{:else}
					<div class="text-sm text-muted-foreground mb-4">
						Found {results.length} {results.length === 1 ? 'entry' : 'entries'}
					</div>
					<div class="space-y-3">
						{#each results as result}
							<a
								href="/diary/{result.date}"
								class="block bg-card rounded-lg border border-border/50 p-4 hover:border-primary/50 transition-colors animate-fade-in"
							>
								<div class="text-sm text-muted-foreground mb-1">{result.date}</div>
								<div class="text-foreground line-clamp-2">
									{result.snippet || 'No content'}
								</div>
							</a>
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	</main>
</div>
