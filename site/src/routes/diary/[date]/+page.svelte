<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import TiptapEditor from '$lib/components/editor/TiptapEditor.svelte';
	import TableOfContents from '$lib/components/ui/TableOfContents.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import DiaryShareModal from '$lib/components/share/DiaryShareModal.svelte';
	import { getDiaryByDate } from '$lib/api/diaries';
	import { isAuthenticated } from '$lib/api/client';
	import { getDiaryEmojiSettings } from '$lib/api/settings';
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
		hasDirtyCache,
		initDiaryCache,
		cleanupDiaryCache
	} from '$lib/stores/diaryCache';
	import { onlineState } from '$lib/stores/onlineStatus';
	import { DEFAULT_MOOD_OPTIONS, DEFAULT_WEATHER_OPTIONS } from '$lib/utils/diaryEmoji';

	let moodPresets: string[] = [...DEFAULT_MOOD_OPTIONS];
	let weatherPresets: string[] = [...DEFAULT_WEATHER_OPTIONS];

	let content = '';
	let loading = true;
	let loadRequestId = 0;
	let showDrawer = false;
	let showDesktopToc = true;
	let showShareModal = false;
	let selectedContent = '';
	let selectedMood = '';
	let selectedWeather = '';
	// Snapshot taken on mousedown (before blur clears selectedContent)
	let shareSelectedContent = '';
	let shareOpenedByMouse = false;
	let date = getToday();
	let cacheReady = false;

	function captureShareSelection() {
		shareSelectedContent = selectedContent;
		shareOpenedByMouse = true;
	}

	function openShareModal() {
		// Keyboard path (Enter/Space): mousedown never fired, so clear any stale snapshot
		if (!shareOpenedByMouse) {
			shareSelectedContent = '';
		}
		shareOpenedByMouse = false;
		showShareModal = true;
	}

	$: date = $page.params.date ?? getToday();
	$: canGoNext = !isToday(date);
	$: currentDateIsDirty = date ? $diaryCache[date]?.isDirty || false : false;
	$: isAnySyncing = $syncState.isSyncing;

	function goToPreviousDay() {
		const prevDate = getPreviousDay(date);
		goto(`/diary/${prevDate}`);
	}

	function goToNextDay() {
		const currentDate = date;
		if (isToday(currentDate)) return;
		const nextDate = getNextDay(currentDate);
		goto(`/diary/${nextDate}`);
	}

	function goToCalendar() {
		goto('/diary');
	}

	async function loadDiary(targetDate: string) {
		const currentRequestId = ++loadRequestId;
		const cached = getCachedContent(targetDate);

		// Keep unsynced local draft and skip server fetch.
		if (cached?.isDirty) {
			content = cached.content;
			selectedMood = cached.mood || '';
			selectedWeather = cached.weather || '';
			loading = false;
			return;
		}

		content = '';
		selectedMood = '';
		selectedWeather = '';

		// Browser cache is disabled; fetch current content from server.
		loading = true;
		try {
			const diary = await getDiaryByDate(targetDate);
			if (currentRequestId !== loadRequestId) return;
			updateFromServer(targetDate, diary);
			if (currentRequestId !== loadRequestId) return;
			content = diary?.content || '';
			selectedMood = diary?.mood || '';
			selectedWeather = diary?.weather || '';
		} catch (error) {
			console.error('Failed to load diary:', error);
			// Keep local draft on fetch failure if one exists.
			if (cached?.isDirty) {
				content = cached.content;
				selectedMood = cached.mood || '';
				selectedWeather = cached.weather || '';
			}
		}
		loading = false;
	}

	async function loadDiaryEmojiPresets() {
		try {
			const settings = await getDiaryEmojiSettings();
			moodPresets = [...settings.mood_options];
			weatherPresets = [...settings.weather_options];
		} catch (error) {
			console.error('Failed to load mood/weather presets:', error);
		}
	}

	function handleContentChange(newContent: string) {
		content = newContent;
		updateLocalCache(date, {
			content,
			mood: selectedMood,
			weather: selectedWeather
		});
	}

	function handleMoodSelect(emoji: string) {
		selectedMood = selectedMood === emoji ? '' : emoji;
		updateLocalCache(date, {
			content,
			mood: selectedMood,
			weather: selectedWeather
		});
	}

	function handleWeatherSelect(emoji: string) {
		selectedWeather = selectedWeather === emoji ? '' : emoji;
		updateLocalCache(date, {
			content,
			mood: selectedMood,
			weather: selectedWeather
		});
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

		// Initialize diary cache (includes online status)
		initDiaryCache();
		cacheReady = true;
		void loadDiaryEmojiPresets();

		window.addEventListener('keydown', handleKeyboard);
		return () => {
			window.removeEventListener('keydown', handleKeyboard);
			// Note: Don't cleanup diaryCache here as it's shared across pages
		};
	});

	// Load diary only in browser (not during SSR)
	$: if (cacheReady && date && date !== previousDate && typeof window !== 'undefined') {
		previousDate = date;
		loadDiary(date);
	}
</script>

<svelte:head>
	<title>{formatDisplayDate(date)} - Diarum</title>
</svelte:head>

<div class="min-h-screen bg-background">
	<!-- Sticky Header Container -->
	<div class="sticky top-0 z-20">
		<!-- Compact Glass Header -->
		<header class="glass border-b border-border/50">
			<div class="max-w-6xl mx-auto px-4 h-11">
				<div class="flex items-center justify-between h-full">
					<!-- Left: Brand -->
					<a href="/" class="flex items-center gap-2 hover:opacity-80 transition-opacity">
						<img src="/logo.png" alt="Diarum" class="w-6 h-6" />
						<span class="hidden sm:inline text-lg font-semibold text-foreground hover:text-primary transition-colors">Diarum</span>
					</a>

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
						<a href="/assistant" class="hidden sm:block p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200" title="AI Assistant">
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<rect x="4" y="6" width="16" height="12" rx="2" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
								<line x1="12" y1="6" x2="12" y2="2" stroke-width="2" stroke-linecap="round"/>
								<circle cx="12" cy="2" r="1" fill="currentColor"/>
								<circle cx="9" cy="11" r="1.5" fill="currentColor"/>
								<circle cx="15" cy="11" r="1.5" fill="currentColor"/>
								<path d="M9 15h6" stroke-width="2" stroke-linecap="round"/>
								<rect x="1" y="10" width="2" height="4" rx="1" fill="currentColor"/>
								<rect x="21" y="10" width="2" height="4" rx="1" fill="currentColor"/>
							</svg>
						</a>

						<button
							on:mousedown={captureShareSelection}
							on:click={openShareModal}
							class="hidden sm:block p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
							title="Share as image"
						>
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" />
							</svg>
						</button>

						<button
							on:click={() => {
								if (window.innerWidth >= 1024) {
									showDesktopToc = !showDesktopToc;
								} else {
									showDrawer = !showDrawer;
								}
							}}
							class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 {(showDesktopToc || showDrawer) ? 'bg-muted/50' : ''}"
							title="Table of contents"
						>
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h7" />
							</svg>
						</button>

						<button
							on:click={handleManualSave}
							class="flex items-center p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
							title={!$onlineState.isOnline ? 'Offline - changes saved locally' : isAnySyncing ? 'Syncing...' : currentDateIsDirty ? 'Click to save now' : 'All changes saved'}
						>
							{#if !$onlineState.isOnline}
								<svg class="w-4 h-4 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M18.364 5.636a9 9 0 010 12.728m0 0l-2.829-2.829m2.829 2.829L21 21M15.536 8.464a5 5 0 010 7.072m0 0l-2.829-2.829m-4.243 2.829a4.978 4.978 0 01-1.414-2.83m-1.414 5.658a9 9 0 01-2.167-9.238m7.824 2.167a1 1 0 111.414 1.414m-1.414-1.414L3 3"></path>
								</svg>
							{:else if isAnySyncing}
								<svg class="w-4 h-4 text-yellow-500 animate-spin" fill="none" viewBox="0 0 24 24">
									<circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" stroke-dasharray="40 20" stroke-linecap="round"></circle>
								</svg>
							{:else if currentDateIsDirty}
								<svg class="w-4 h-4 text-yellow-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 4H6a2 2 0 00-2 2v12a2 2 0 002 2h12a2 2 0 002-2V7.828a2 2 0 00-.586-1.414l-1.828-1.828A2 2 0 0016.172 4H15M8 4v4h6V4M8 4h6m-6 0H8m8 12a2 2 0 11-4 0 2 2 0 014 0z"></path>
								</svg>
							{:else}
								<svg class="w-4 h-4 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
								</svg>
							{/if}
						</button>
					</div>
				</div>
			</div>
		</header>

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
							bind:selectedContent
							onChange={handleContentChange}
							placeholder="What's on your mind today?"
							diaryDate={date}
						/>
					</div>
				{/if}
			</main>

			<!-- Desktop Right Sidebar -->
			<aside class="hidden lg:block w-[19rem] flex-shrink-0">
				<div class="sticky top-11 space-y-3 animate-slide-in-right">
					<div class="rounded-2xl border border-orange-500/25 bg-gradient-to-br from-orange-50/80 via-card to-rose-100/60 dark:from-orange-950/20 dark:via-card dark:to-rose-950/10 p-4 shadow-sm">
						<div class="flex items-center justify-between mb-2">
							<div>
								<div class="text-xs uppercase tracking-[0.14em] text-muted-foreground">Today's mood</div>
								<div class="text-sm font-semibold text-foreground">How are you feeling?</div>
							</div>
							{#if selectedMood}
								<button
									on:click={() => handleMoodSelect(selectedMood)}
									class="text-[11px] px-2 py-1 rounded-full bg-background/70 hover:bg-background border border-border/70 transition-colors"
								>
									Clear
								</button>
							{/if}
						</div>
						<div class="grid grid-cols-4 gap-2">
							{#each moodPresets as option}
								<button
									on:click={() => handleMoodSelect(option)}
									class="emoji-option {selectedMood === option ? 'emoji-option-active' : ''}"
									title={option}
									aria-label={`Mood ${option}`}
								>
									<span class="text-xl leading-none">{option}</span>
								</button>
							{/each}
						</div>
					</div>

					<div class="rounded-2xl border border-cyan-500/25 bg-gradient-to-br from-cyan-50/80 via-card to-blue-100/60 dark:from-cyan-950/20 dark:via-card dark:to-blue-950/10 p-4 shadow-sm">
						<div class="flex items-center justify-between mb-2">
							<div>
								<div class="text-xs uppercase tracking-[0.14em] text-muted-foreground">Today's weather</div>
								<div class="text-sm font-semibold text-foreground">What's outside?</div>
							</div>
							{#if selectedWeather}
								<button
									on:click={() => handleWeatherSelect(selectedWeather)}
									class="text-[11px] px-2 py-1 rounded-full bg-background/70 hover:bg-background border border-border/70 transition-colors"
								>
									Clear
								</button>
							{/if}
						</div>
						<div class="grid grid-cols-4 gap-2">
							{#each weatherPresets as option}
								<button
									on:click={() => handleWeatherSelect(option)}
									class="emoji-option {selectedWeather === option ? 'emoji-option-active' : ''}"
									title={option}
									aria-label={`Weather ${option}`}
								>
									<span class="text-xl leading-none">{option}</span>
								</button>
							{/each}
						</div>
					</div>

					{#if showDesktopToc}
						<div class="bg-card/50 rounded-xl border border-border/50 p-4">
							<TableOfContents {content} />
						</div>
					{/if}
				</div>
			</aside>
		</div>
	</div>

	<!-- Footer -->
	<Footer maxWidth="6xl" tagline="Ctrl+S or ⌘S to save" />
</div>

<!-- Left Drawer -->
{#if showDrawer}
	<!-- Backdrop -->
	<button
		class="fixed inset-0 bg-black/40 backdrop-blur-sm z-40 lg:hidden"
		on:click={() => showDrawer = false}
		aria-label="Close menu"
	></button>

	<!-- Drawer Panel -->
	<div class="fixed inset-y-0 left-0 w-72 bg-card border-r border-border shadow-2xl z-50 lg:hidden animate-slide-in-left">
		<!-- Drawer Header -->
		<div class="flex items-center justify-between px-5 py-4 border-b border-border/50">
			<div class="flex items-center gap-2">
				<img src="/logo.png" alt="Diarum" class="w-6 h-6" />
				<span class="font-semibold text-foreground">Menu</span>
			</div>
			<button
				on:click={() => showDrawer = false}
				class="p-2 hover:bg-muted rounded-lg transition-colors"
				aria-label="Close"
			>
				<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
				</svg>
			</button>
		</div>

		<!-- Drawer Content -->
		<div class="flex flex-col h-[calc(100%-57px)]">
			<!-- Actions Section -->
			<div class="px-3 py-3">
				<div class="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-2 px-1">
					Quick Actions
				</div>
				<div class="space-y-0.5">
					<a
						href="/assistant"
						class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
						on:click={() => showDrawer = false}
					>
						<div class="p-1.5 rounded-md bg-primary/10 text-primary group-hover:bg-primary/20 transition-colors">
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<rect x="4" y="6" width="16" height="12" rx="2" stroke-width="2"/>
								<circle cx="9" cy="11" r="1.5" fill="currentColor"/>
								<circle cx="15" cy="11" r="1.5" fill="currentColor"/>
							</svg>
						</div>
						<div class="min-w-0">
							<div class="text-xs font-medium text-foreground">AI Assistant</div>
							<div class="text-[10px] text-muted-foreground truncate">Chat with AI about your diary</div>
						</div>
					</a>

					<button
						on:mousedown={captureShareSelection}
						on:click={() => { showDrawer = false; openShareModal(); }}
						class="w-full flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
					>
						<div class="p-1.5 rounded-md bg-blue-500/10 text-blue-500 group-hover:bg-blue-500/20 transition-colors">
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" />
							</svg>
						</div>
						<div class="min-w-0 text-left">
							<div class="text-xs font-medium text-foreground">Share</div>
							<div class="text-[10px] text-muted-foreground truncate">Export as beautiful image</div>
						</div>
					</button>

					<a
						href="/diary"
						class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
						on:click={() => showDrawer = false}
					>
						<div class="p-1.5 rounded-md bg-green-500/10 text-green-500 group-hover:bg-green-500/20 transition-colors">
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
									d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
							</svg>
						</div>
						<div class="min-w-0">
							<div class="text-xs font-medium text-foreground">Calendar</div>
							<div class="text-[10px] text-muted-foreground truncate">View all diary entries</div>
						</div>
					</a>

					<a
						href="/settings"
						class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
						on:click={() => showDrawer = false}
					>
						<div class="p-1.5 rounded-md bg-gray-500/10 text-gray-500 group-hover:bg-gray-500/20 transition-colors">
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
							</svg>
						</div>
						<div class="min-w-0">
							<div class="text-xs font-medium text-foreground">Settings</div>
							<div class="text-[10px] text-muted-foreground truncate">Preferences & sync</div>
						</div>
					</a>
				</div>
			</div>

			<!-- Divider -->
			<div class="mx-3 border-t border-border/50"></div>

			<!-- Mood & Weather -->
			<div class="px-3 py-3 space-y-3 border-b border-border/50">
				<div>
					<div class="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-2 px-1">Mood</div>
					<div class="grid grid-cols-4 gap-1.5">
						{#each moodPresets as option}
							<button
								on:click={() => handleMoodSelect(option)}
								class="emoji-option {selectedMood === option ? 'emoji-option-active' : ''}"
								title={option}
								aria-label={`Mood ${option}`}
							>
								<span class="text-lg">{option}</span>
							</button>
						{/each}
					</div>
				</div>

				<div>
					<div class="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-2 px-1">Weather</div>
					<div class="grid grid-cols-4 gap-1.5">
						{#each weatherPresets as option}
							<button
								on:click={() => handleWeatherSelect(option)}
								class="emoji-option {selectedWeather === option ? 'emoji-option-active' : ''}"
								title={option}
								aria-label={`Weather ${option}`}
							>
								<span class="text-lg">{option}</span>
							</button>
						{/each}
					</div>
				</div>
			</div>

			<!-- TOC Section -->
			<div class="flex-1 overflow-y-auto px-3 py-3">
				<TableOfContents {content} onNavigate={() => showDrawer = false} />
			</div>
		</div>
	</div>
{/if}

<!-- Share Modal -->
<DiaryShareModal
	isOpen={showShareModal}
	{date}
	{content}
	selectedContent={shareSelectedContent}
	onClose={() => showShareModal = false}
/>

<style>
	.emoji-option {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		padding: 0.5rem;
		border-radius: 0.8rem;
		border: 1px solid hsl(var(--border) / 0.6);
		background: hsl(var(--background) / 0.72);
		transition: transform 0.18s ease, border-color 0.18s ease, background-color 0.18s ease, box-shadow 0.18s ease;
	}

	.emoji-option:hover {
		transform: translateY(-1px);
		background: hsl(var(--muted) / 0.65);
		border-color: hsl(var(--primary) / 0.3);
	}

	.emoji-option-active {
		border-color: hsl(var(--primary) / 0.65);
		background: hsl(var(--primary) / 0.12);
		box-shadow: 0 8px 16px hsl(var(--primary) / 0.12);
	}
</style>

