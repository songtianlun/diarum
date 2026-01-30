<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import Footer from '$lib/components/ui/Footer.svelte';
	import { searchDiaries } from '$lib/api/diaries';
	import { isAuthenticated } from '$lib/api/client';
	import { formatDisplayDate, formatShortDate, getDayOfWeek } from '$lib/utils/date';

	interface SearchResult {
		id: string;
		date: string;
		snippet: string;
	}

	let query = '';
	let results: SearchResult[] = [];
	let loading = false;
	let searched = false;
	let searchTimeout: ReturnType<typeof setTimeout>;
	let inputElement: HTMLInputElement;

	// Debounced search
	function handleInput() {
		clearTimeout(searchTimeout);
		if (query.trim().length === 0) {
			results = [];
			searched = false;
			return;
		}
		if (query.trim().length < 2) {
			return;
		}
		searchTimeout = setTimeout(() => {
			performSearch();
		}, 300);
	}

	async function performSearch() {
		if (query.trim().length < 2) return;

		loading = true;
		searched = true;

		try {
			const data = await searchDiaries(query.trim());
			results = data.map((item: any) => ({
				id: item.id,
				date: item.date?.split(' ')[0] || item.date,
				snippet: cleanSnippet(item.snippet || '', query.trim())
			}));
		} catch (error) {
			console.error('Search error:', error);
			results = [];
		} finally {
			loading = false;
		}
	}

	function cleanSnippet(snippet: string, searchQuery: string): string {
		// Strip HTML tags and clean up whitespace
		return snippet.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim();
	}

	function highlightMatch(text: string, searchQuery: string): string {
		if (!searchQuery) return text;
		const regex = new RegExp(`(${escapeRegex(searchQuery)})`, 'gi');
		return text.replace(regex, '<mark class="bg-yellow-200 dark:bg-yellow-800/60 px-0.5 rounded">$1</mark>');
	}

	function escapeRegex(str: string): string {
		return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === 'Enter') {
			clearTimeout(searchTimeout);
			performSearch();
		}
		if (event.key === 'Escape') {
			query = '';
			results = [];
			searched = false;
		}
	}

	function navigateToDiary(date: string) {
		goto(`/diary/${date}`);
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		// Auto focus search input
		inputElement?.focus();
	});
</script>

<svelte:head>
	<title>Search - Diaria</title>
</svelte:head>

<div class="min-h-screen bg-background">
	<!-- Header -->
	<header class="glass border-b border-border/50 sticky top-0 z-20">
		<div class="max-w-3xl mx-auto px-4 h-11">
			<div class="flex items-center justify-between h-full">
				<a href="/" class="text-lg font-semibold text-foreground hover:text-primary transition-colors">
					Diaria
				</a>

				<div class="flex items-center gap-2">
					<a
						href="/diary"
						class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
						title="Back to Calendar"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
						</svg>
					</a>
				</div>
			</div>
		</div>
	</header>

	<!-- Main Content -->
	<main class="max-w-3xl mx-auto px-4 py-8">
		<!-- Search Header -->
		<div class="mb-8 animate-fade-in">
			<h1 class="text-2xl font-bold text-foreground mb-2">Search Diaries</h1>
			<p class="text-sm text-muted-foreground">Find entries by keywords in your diary</p>
		</div>

		<!-- Search Input -->
		<div class="relative mb-6 animate-fade-in" style="animation-delay: 50ms">
			<div class="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none">
				<svg class="w-5 h-5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
						d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
				</svg>
			</div>
			<input
				bind:this={inputElement}
				bind:value={query}
				on:input={handleInput}
				on:keydown={handleKeydown}
				type="text"
				placeholder="Search your diaries..."
				class="w-full pl-12 pr-4 py-3 bg-card border border-border/50 rounded-xl text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring/50 focus:border-transparent transition-all duration-200"
			/>
			{#if query.length > 0}
				<button
					on:click={() => { query = ''; results = []; searched = false; inputElement?.focus(); }}
					class="absolute inset-y-0 right-0 pr-4 flex items-center text-muted-foreground hover:text-foreground transition-colors"
					title="Clear search"
				>
					<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
					</svg>
				</button>
			{/if}
		</div>

		<!-- Loading State -->
		{#if loading}
			<div class="flex flex-col items-center justify-center py-12 gap-3 animate-fade-in">
				<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
				</svg>
				<div class="text-muted-foreground text-sm">Searching...</div>
			</div>
		{:else if searched && results.length === 0}
			<!-- No Results -->
			<div class="flex flex-col items-center justify-center py-12 gap-4 animate-fade-in">
				<div class="w-16 h-16 rounded-full bg-muted/50 flex items-center justify-center">
					<svg class="w-8 h-8 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
							d="M9.172 16.172a4 4 0 015.656 0M9 10h.01M15 10h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
					</svg>
				</div>
				<div class="text-center">
					<p class="text-foreground font-medium">No results found</p>
					<p class="text-sm text-muted-foreground mt-1">Try different keywords or check your spelling</p>
				</div>
			</div>
		{:else if results.length > 0}
			<!-- Results Count -->
			<div class="mb-4 text-sm text-muted-foreground animate-fade-in">
				Found <span class="font-medium text-foreground">{results.length}</span> {results.length === 1 ? 'entry' : 'entries'}
			</div>

			<!-- Results List -->
			<div class="space-y-3">
				{#each results as result, index}
					<button
						on:click={() => navigateToDiary(result.date)}
						class="w-full text-left bg-card hover:bg-accent/50 border border-border/50 rounded-xl p-4 transition-all duration-200 hover:shadow-md hover:border-border animate-fade-in group"
						style="animation-delay: {(index + 1) * 50}ms"
					>
						<!-- Date Header -->
						<div class="flex items-center justify-between mb-2">
							<div class="flex items-center gap-2">
								<span class="text-sm font-medium text-foreground">
									<span class="hidden sm:inline">{formatDisplayDate(result.date)}</span>
									<span class="sm:hidden">{formatShortDate(result.date)}</span>
								</span>
								<span class="text-xs text-muted-foreground">{getDayOfWeek(result.date)}</span>
							</div>
							<svg class="w-4 h-4 text-muted-foreground group-hover:text-foreground group-hover:translate-x-0.5 transition-all" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
							</svg>
						</div>
						<!-- Snippet -->
						<p class="text-sm text-muted-foreground leading-relaxed line-clamp-3">
							{@html highlightMatch(result.snippet, query)}
						</p>
					</button>
				{/each}
			</div>
		{:else}
			<!-- Initial State -->
			<div class="flex flex-col items-center justify-center py-12 gap-4 animate-fade-in" style="animation-delay: 100ms">
				<div class="w-16 h-16 rounded-full bg-muted/50 flex items-center justify-center">
					<svg class="w-8 h-8 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
							d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
					</svg>
				</div>
				<div class="text-center">
					<p class="text-foreground font-medium">Start searching</p>
					<p class="text-sm text-muted-foreground mt-1">Enter at least 2 characters to search</p>
				</div>
			</div>
		{/if}

		<!-- Keyboard Shortcuts Hint -->
		<div class="mt-8 flex justify-center gap-4 text-xs text-muted-foreground animate-fade-in" style="animation-delay: 150ms">
			<span>
				<kbd class="px-1.5 py-0.5 bg-muted rounded text-[10px]">Enter</kbd>
				<span class="ml-1">to search</span>
			</span>
			<span>
				<kbd class="px-1.5 py-0.5 bg-muted rounded text-[10px]">Esc</kbd>
				<span class="ml-1">to clear</span>
			</span>
		</div>
	</main>

	<!-- Footer -->
	<Footer maxWidth="3xl" tagline="Search through your memories" />
</div>

<style>
	kbd {
		font-family: ui-monospace, monospace;
	}

	.line-clamp-3 {
		display: -webkit-box;
		-webkit-line-clamp: 3;
		line-clamp: 3;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
</style>
