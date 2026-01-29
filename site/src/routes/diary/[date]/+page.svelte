<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import TiptapEditor from '$lib/components/editor/TiptapEditor.svelte';
	import TableOfContents from '$lib/components/ui/TableOfContents.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import { getDiaryByDate } from '$lib/api/diaries';
	import { isAuthenticated } from '$lib/api/client';
	import {
		formatDisplayDate,
		formatShortDate,
		getDayOfWeek,
		getPreviousDay,
		getNextDay,
		getToday,
		isToday
	} from '$lib/utils/date';
	import {
		diaryCache,
		syncState,
		updateLocalCache,
		updateFromServer,
		getCachedContent,
		forceSyncNow,
		hasDirtyCache
	} from '$lib/stores/diaryCache';

	let content = '';
	let loading = true;
	let loadRequestId = 0;
	let showMobileToc = false;

	$: date = $page.params.date;
	$: canGoNext = !isToday(date);
	$: currentDateIsDirty = date ? $diaryCache[date]?.isDirty || false : false;
	$: isAnySyncing = $syncState.isSyncing;

	function goToPreviousDay() {
		const prevDate = getPreviousDay($page.params.date);
		goto(`/diary/${prevDate}`);
	}

	function goToNextDay() {
		const currentDate = $page.params.date;
		if (isToday(currentDate)) return;
		const nextDate = getNextDay(currentDate);
		goto(`/diary/${nextDate}`);
	}

	function goToToday() {
		if (isToday($page.params.date)) return;
		goto(`/diary/${getToday()}`);
	}

	function goToCalendar() {
		goto('/diary');
	}

	async function loadDiary(targetDate: string) {
		const currentRequestId = ++loadRequestId;
		const cached = getCachedContent(targetDate);
		if (cached) {
			content = cached.content;
			if (cached.isDirty) {
				loading = false;
				return;
			}
		} else {
			content = '';
		}
		loading = true;
		const diary = await getDiaryByDate(targetDate);
		if (currentRequestId !== loadRequestId) return;
		updateFromServer(targetDate, diary);
		if (currentRequestId !== loadRequestId) return;
		const updatedCache = getCachedContent(targetDate);
		content = updatedCache?.content || '';
		loading = false;
	}

	function handleContentChange(newContent: string) {
		content = newContent;
		updateLocalCache(date, newContent);
	}

	async function handleManualSave() {
		await forceSyncNow();
	}

	function handleKeyboard(event: KeyboardEvent) {
		if ((event.ctrlKey || event.metaKey) && event.key === 's') {
			event.preventDefault();
			handleManualSave();
		}
	}

	let previousDate = '';

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		window.addEventListener('keydown', handleKeyboard);
		return () => {
			window.removeEventListener('keydown', handleKeyboard);
		};
	});

	// Load diary only in browser (not during SSR)
	$: if (date && date !== previousDate && typeof window !== 'undefined') {
		previousDate = date;
		loadDiary(date);
	}
</script>

<svelte:head>
	<title>{formatDisplayDate(date)} - Diaria</title>
</svelte:head>

<div class="min-h-screen bg-background">
	<!-- Sticky Header Container -->
	<div class="sticky top-0 z-20">
		<!-- Compact Glass Header -->
		<header class="glass border-b border-border/50">
		<div class="max-w-6xl mx-auto px-4 h-11">
			<div class="flex items-center justify-between h-full">
				<!-- Left: Brand -->
				<a href="/" class="text-lg font-semibold text-foreground hover:text-primary transition-colors">Diaria</a>

				<!-- Center: Date and Navigation -->
				<div class="flex items-center gap-2">
					<button
						on:click={goToPreviousDay}
						disabled={loading}
						class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 disabled:opacity-50"
						title="Previous day"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
						</svg>
					</button>

					<button
						on:click={goToCalendar}
						disabled={loading}
						class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 disabled:opacity-50"
						title="Calendar"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
						</svg>
					</button>

					<div class="text-sm text-foreground">
						<span class="hidden sm:inline">{formatDisplayDate(date)}</span>
						<span class="sm:hidden">{formatShortDate(date)}</span>
						<span class="hidden sm:inline text-xs text-muted-foreground font-normal ml-1">{getDayOfWeek(date)}</span>
						{#if isToday(date)}
							<span class="text-xs px-1.5 py-0.5 bg-primary/10 text-primary rounded-full ml-1">Today</span>
						{/if}
					</div>

					<button
						on:click={goToNextDay}
						disabled={loading || !canGoNext}
						class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 disabled:opacity-50"
						title={canGoNext ? "Next day" : "Cannot go beyond today"}
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
						</svg>
					</button>
				</div>

				<!-- Right: Actions -->
				<div class="flex items-center gap-2">
					<!-- Today Button -->
					{#if !isToday(date)}
						<button
							on:click={goToToday}
							class="px-2 py-1 text-xs bg-primary text-primary-foreground rounded-md hover:opacity-90 transition-all duration-200"
						>
							Today
						</button>
					{/if}

					<!-- Mobile TOC Toggle -->
					<button
						on:click={() => (showMobileToc = !showMobileToc)}
						class="lg:hidden p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
						title="Table of contents"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h7" />
						</svg>
					</button>

					<!-- Sync Status -->
					<div class="flex items-center" title={isAnySyncing ? 'Syncing...' : currentDateIsDirty ? 'Unsaved' : 'Synced'}>
						{#if isAnySyncing}
							<svg class="w-4 h-4 text-yellow-500 animate-spin" fill="none" viewBox="0 0 24 24">
								<circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" stroke-dasharray="40 20" stroke-linecap="round"></circle>
							</svg>
						{:else if currentDateIsDirty}
							<svg class="w-4 h-4 text-yellow-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"></path>
							</svg>
						{:else}
							<svg class="w-4 h-4 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
							</svg>
						{/if}
					</div>
				</div>
			</div>
		</div>
	</header>

		<!-- Mobile TOC - Inside sticky container -->
		{#if showMobileToc}
			<div class="lg:hidden glass-subtle border-b border-border/50 animate-slide-in-down">
				<div class="max-w-6xl mx-auto px-4 py-2 max-h-[30vh] overflow-y-auto">
					<TableOfContents {content} />
				</div>
			</div>
		{/if}
	</div>

	<!-- Main Content -->
	<div class="max-w-6xl mx-auto px-4 py-6">
		<div class="flex gap-6">
			<!-- Editor -->
			<main class="flex-1 min-w-0">
				{#if loading}
					<div class="flex flex-col items-center justify-center py-20 gap-3 animate-fade-in">
						<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
						</svg>
						<div class="text-muted-foreground text-sm">Loading...</div>
					</div>
				{:else}
					<div class="bg-card rounded-xl shadow-sm border border-border/50 overflow-hidden animate-fade-in">
						<TiptapEditor
							{content}
							onChange={handleContentChange}
							placeholder="What's on your mind today?"
						/>
					</div>
				{/if}
			</main>

			<!-- Desktop TOC Sidebar -->
			<aside class="hidden lg:block w-56 flex-shrink-0">
				<div class="sticky top-11 animate-slide-in-right">
					<div class="bg-card/50 rounded-xl border border-border/50 p-4">
						<TableOfContents {content} />
					</div>
				</div>
			</aside>
		</div>
	</div>

	<!-- Footer -->
	<Footer maxWidth="6xl" tagline="Ctrl+S or ⌘S to save" />
</div>

<style>
	kbd {
		font-family: ui-monospace, monospace;
	}
</style>
