<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import TiptapEditor from '$lib/components/editor/TiptapEditor.svelte';
	import PageFace from '$lib/components/book/PageFace.svelte';
	import BookCatalog from '$lib/components/book/BookCatalog.svelte';
	import { getDiaryByDate, getDatesWithDiaries, type CalendarDiaryMeta } from '$lib/api/diaries';
	import { isAuthenticated } from '$lib/api/client';
	import { getDiaryEmojiSettings } from '$lib/api/settings';
	import { DEFAULT_MOOD_OPTIONS, DEFAULT_WEATHER_OPTIONS } from '$lib/utils/diaryEmoji';
	import {
		getToday,
		isToday,
		getPreviousDay,
		getNextDay,
		addMonths,
		addYears,
		clampToToday,
		formatDisplayDate,
		formatShortDate,
		getDayOfWeek
	} from '$lib/utils/date';
	import {
		diaryCache,
		syncState,
		updateLocalCache,
		getCachedContent,
		forceSyncNow,
		initDiaryCache
	} from '$lib/stores/diaryCache';
	import { onlineState } from '$lib/stores/onlineStatus';

	interface View {
		date: string;
		content: string;
		mood: string;
		weather: string;
	}

	const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

	let ready = false;
	let loading = true;
	let date = getToday();
	let view: View = { date, content: '', mood: '', weather: '' };

	// flip animation state
	let flip: { dir: 'fwd' | 'back'; from: View; to: View; fromScroll: number } | null = null;
	let bookEl: HTMLElement | undefined;
	let animating = false;
	let committed = false;
	let pendingTarget: string | null = null;
	let flipDuration = 650;
	let commitTimer: ReturnType<typeof setTimeout> | null = null;

	// catalog
	let showCatalog = false;
	let catalogEntries: CalendarDiaryMeta[] = [];
	let catalogLoaded = false;
	let catalogLoading = false;

	// mood / weather presets (configured in settings)
	let moodPresets: string[] = [...DEFAULT_MOOD_OPTIONS];
	let weatherPresets: string[] = [...DEFAULT_WEATHER_OPTIONS];

	// responsive: single page below 860px
	let isMobile = false;

	const viewCache = new Map<string, View>();

	$: currentDateIsDirty = date ? $diaryCache[date]?.isDirty || false : false;
	$: isAnySyncing = $syncState.isSyncing;
	$: atToday = isToday(date);
	// the toolbar shows the incoming date as soon as a flip starts, so nothing
	// in the header changes at the landing moment
	$: headerDate = flip ? flip.to.date : date;
	$: headerAtToday = isToday(headerDate);

	// leaf/base data derived from flip state
	$: leafFront = flip ? (flip.dir === 'fwd' ? flip.from : flip.to) : null;
	$: leafBack = flip ? (flip.dir === 'fwd' ? flip.to : flip.from) : null;
	$: baseLeft = flip ? (flip.dir === 'fwd' ? flip.from : flip.to) : view;
	$: baseRight = flip ? (flip.dir === 'fwd' ? flip.to : flip.from) : view;
	$: mobileBase = flip ? (flip.dir === 'fwd' ? flip.to : flip.from) : view;

	async function fetchView(d: string): Promise<View> {
		const dirty = getCachedContent(d);
		if (dirty?.isDirty) {
			return { date: d, content: dirty.content, mood: dirty.mood || '', weather: dirty.weather || '' };
		}
		const cached = viewCache.get(d);
		if (cached) return cached;
		const diary = await getDiaryByDate(d);
		const v: View = {
			date: d,
			content: diary?.content || '',
			mood: diary?.mood || '',
			weather: diary?.weather || ''
		};
		viewCache.set(d, v);
		return v;
	}

	function prefetchNeighbors() {
		const prev = getPreviousDay(date);
		void fetchView(prev).catch(() => {});
		if (!atToday) {
			void fetchView(getNextDay(date)).catch(() => {});
		}
	}

	function navigateTo(target: string) {
		target = clampToToday(target);
		if (!DATE_RE.test(target) || target === date) return;
		if (animating) {
			pendingTarget = target;
			return;
		}
		animating = true;
		committed = false;

		// Start the flip immediately with the best data we already have;
		// fill in fetched content mid-animation if needed (no pre-flip delay).
		const dirty = getCachedContent(target);
		let to: View;
		let needsFetch = false;
		if (dirty?.isDirty) {
			to = { date: target, content: dirty.content, mood: dirty.mood || '', weather: dirty.weather || '' };
		} else if (viewCache.has(target)) {
			to = viewCache.get(target)!;
		} else {
			to = { date: target, content: '', mood: '', weather: '' };
			needsFetch = true;
		}

		// preserve the outgoing page's scroll position on its static snapshot
		const fromScroll = bookEl?.querySelector('.content-scroll')?.scrollTop ?? 0;

		flipDuration = pendingTarget ? 400 : 650;
		flip = { dir: target > date ? 'fwd' : 'back', from: view, to, fromScroll };
		// fallback in case animationend doesn't fire
		commitTimer = setTimeout(() => void commitFlip(), flipDuration + 120);

		if (needsFetch) {
			void fetchView(target)
				.then((v) => {
					if (!committed && flip && flip.to.date === target) {
						// still mid-flip: update the incoming page in place
						flip = { ...flip, to: v };
					} else if (date === target) {
						// already committed with the placeholder: swap in real data
						view = v;
					}
				})
				.catch(() => {});
		}
	}

	async function commitFlip() {
		if (!flip || committed) return;
		committed = true;
		if (commitTimer) {
			clearTimeout(commitTimer);
			commitTimer = null;
		}
		const to = flip.to;
		date = to.date;
		view = to;
		flip = null;
		// Keep `animating` true until the URL matches, otherwise the route
		// watcher sees a stale $page.params.date and navigates backwards.
		if ($page.params.date !== to.date) {
			try {
				await goto(`/diary/book/${to.date}`, { noScroll: true, keepFocus: true });
			} catch {
				/* ignore */
			}
		}
		animating = false;
		prefetchNeighbors();
		const next = pendingTarget;
		pendingTarget = null;
		if (next && next !== date) void navigateTo(next);
	}

	function handleLeafAnimationEnd(e: AnimationEvent) {
		if (e.animationName.startsWith('leaf-')) void commitFlip();
	}

	function handleContentChange(newContent: string) {
		view = { ...view, content: newContent };
		viewCache.set(date, view);
		updateLocalCache(date, { content: newContent, mood: view.mood, weather: view.weather });
	}

	function handleMoodSelect(emoji: string) {
		view = { ...view, mood: emoji };
		viewCache.set(date, view);
		updateLocalCache(date, { content: view.content, mood: emoji, weather: view.weather });
	}

	function handleWeatherSelect(emoji: string) {
		view = { ...view, weather: emoji };
		viewCache.set(date, view);
		updateLocalCache(date, { content: view.content, mood: view.mood, weather: emoji });
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

	async function toggleCatalog() {
		showCatalog = !showCatalog;
		if (showCatalog && !catalogLoading) {
			catalogLoading = true;
			try {
				catalogEntries = await getDatesWithDiaries('1970-01-01', addMonths(getToday(), 2));
				catalogLoaded = true;
			} finally {
				catalogLoading = false;
			}
		}
	}

	function handleCatalogSelect(d: string) {
		showCatalog = false;
		void navigateTo(d);
	}

	/**
	 * Force the book width to an even integer number of pixels.
	 * The flip leaf hinges at exactly 50% width; with a fractional half-width
	 * the leaf's composited faces land on half-pixel offsets and the whole
	 * page appears to shift/soften for a frame when a flip starts or ends.
	 * With an integer half-width the leaf-at-rest composite is a pure integer
	 * translation and matches the static page pixel for pixel.
	 */
	function evenWidth(node: HTMLElement) {
		const parent = node.parentElement;
		const apply = () => {
			node.style.width = '';
			const w = node.getBoundingClientRect().width;
			node.style.width = `${Math.floor(w / 2) * 2}px`;
		};
		apply();
		const ro = new ResizeObserver(apply);
		if (parent) ro.observe(parent);
		return { destroy: () => ro.disconnect() };
	}

	function isEditing(): boolean {
		const el = document.activeElement as HTMLElement | null;
		return !!el && (!!el.closest('.ProseMirror') || el.isContentEditable || el.tagName === 'INPUT' || el.tagName === 'TEXTAREA');
	}

	function handleKeyboard(e: KeyboardEvent) {
		if ((e.ctrlKey || e.metaKey) && e.key === 's') {
			e.preventDefault();
			void forceSyncNow();
			return;
		}
		if (e.key === 'Escape' && showCatalog) {
			showCatalog = false;
			return;
		}
		if (isEditing() || showCatalog) return;
		if (e.key === 'ArrowLeft') void navigateTo(getPreviousDay(date));
		else if (e.key === 'ArrowRight') void navigateTo(getNextDay(date));
	}

	// touch swipe (single-page mode mostly, but works on desktop touch too)
	let touchStartX = 0;
	let touchStartY = 0;
	let touchFromEditor = false;

	function handleTouchStart(e: TouchEvent) {
		const t = e.touches[0];
		touchStartX = t.clientX;
		touchStartY = t.clientY;
		touchFromEditor = !!(e.target as HTMLElement)?.closest('.ProseMirror');
	}

	function handleTouchEnd(e: TouchEvent) {
		if (touchFromEditor || isEditing()) return;
		const t = e.changedTouches[0];
		const dx = t.clientX - touchStartX;
		const dy = t.clientY - touchStartY;
		if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 2) {
			if (dx < 0) void navigateTo(getNextDay(date));
			else void navigateTo(getPreviousDay(date));
		}
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		initDiaryCache();
		void loadDiaryEmojiPresets();

		const mq = window.matchMedia('(max-width: 859px)');
		isMobile = mq.matches;
		const onMq = (e: MediaQueryListEvent) => (isMobile = e.matches);
		mq.addEventListener('change', onMq);

		// initial date from route
		let initial = $page.params.date ?? getToday();
		if (!DATE_RE.test(initial) || isNaN(new Date(initial + 'T00:00:00').getTime())) {
			initial = getToday();
		}
		initial = clampToToday(initial);
		date = initial;
		if ($page.params.date !== initial) {
			goto(`/diary/book/${initial}`, { replaceState: true, noScroll: true });
		}

		void (async () => {
			loading = true;
			try {
				view = await fetchView(date);
			} finally {
				loading = false;
				ready = true;
				prefetchNeighbors();
			}
		})();

		window.addEventListener('keydown', handleKeyboard);
		return () => {
			window.removeEventListener('keydown', handleKeyboard);
			mq.removeEventListener('change', onMq);
			if (commitTimer) clearTimeout(commitTimer);
		};
	});

	// react to browser back/forward (route param changed externally)
	$: routeDate = $page.params.date ?? '';
	$: if (ready && routeDate && DATE_RE.test(routeDate) && routeDate !== date && !animating && routeDate !== pendingTarget) {
		// defer: navigateTo mutates state, which is unsafe synchronously inside a reactive statement
		const target = routeDate;
		setTimeout(() => void navigateTo(target), 0);
	}
</script>

<svelte:head>
	<title>{formatDisplayDate(date)} · Book - Diarum</title>
</svelte:head>

<div class="book-screen">
	<!-- Toolbar -->
	<header class="glass border-b border-border/50 sticky top-0 z-30">
		<div class="max-w-6xl mx-auto px-2 sm:px-4 h-11">
			<div class="flex items-center justify-between h-full gap-1">
				<!-- Left: back -->
				<a
					href="/diary"
					class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 flex-shrink-0"
					title="Back to calendar"
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 19l-7-7m0 0l7-7m-7 7h18" />
					</svg>
				</a>

				<!-- Center: navigation cluster -->
				<div class="flex items-center gap-0.5 sm:gap-1 min-w-0">
					<button
						class="nav-btn"
						title="Previous year"
						disabled={animating}
						on:click={() => navigateTo(addYears(date, -1))}
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M18.5 19l-7-7 7-7M11 19l-7-7 7-7" />
							<path stroke-linecap="round" stroke-width="2" d="M3.5 5v14" />
						</svg>
					</button>
					<button
						class="nav-btn"
						title="Previous month"
						disabled={animating}
						on:click={() => navigateTo(addMonths(date, -1))}
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M18 19l-7-7 7-7M11 19l-7-7 7-7" />
						</svg>
					</button>
					<button
						class="nav-btn"
						title="Previous day"
						disabled={animating}
						on:click={() => navigateTo(getPreviousDay(date))}
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
						</svg>
					</button>

					<button
						class="px-1.5 text-sm text-foreground whitespace-nowrap hover:bg-muted/50 rounded-lg py-1 transition-colors min-w-0 {showCatalog ? 'bg-muted/60' : ''}"
						title="Contents"
						on:click={toggleCatalog}
					>
						<span class="hidden md:inline">{formatDisplayDate(headerDate)}</span>
						<span class="md:hidden">{formatShortDate(headerDate)}</span>
						<span class="hidden lg:inline text-xs text-muted-foreground ml-1">{getDayOfWeek(headerDate)}</span>
						{#if headerAtToday}
							<span class="text-[10px] px-1.5 py-0.5 bg-primary/10 text-primary rounded-full ml-1">Today</span>
						{/if}
					</button>

					<button
						class="nav-btn"
						title={atToday ? 'Cannot go beyond today' : 'Next day'}
						disabled={animating || atToday}
						on:click={() => navigateTo(getNextDay(date))}
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
						</svg>
					</button>
					<button
						class="nav-btn"
						title="Next month"
						disabled={animating || atToday}
						on:click={() => navigateTo(addMonths(date, 1))}
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 5l7 7-7 7M13 5l7 7-7 7" />
						</svg>
					</button>
					<button
						class="nav-btn"
						title="Next year"
						disabled={animating || atToday}
						on:click={() => navigateTo(addYears(date, 1))}
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5.5 5l7 7-7 7M13 5l7 7-7 7" />
							<path stroke-linecap="round" stroke-width="2" d="M20.5 5v14" />
						</svg>
					</button>
				</div>

				<!-- Right: save status -->
				<div class="flex items-center gap-0.5 sm:gap-1 flex-shrink-0">
					<button
						class="nav-btn"
						on:click={() => forceSyncNow()}
						title={!$onlineState.isOnline
							? 'Offline - changes saved locally'
							: isAnySyncing
								? 'Syncing...'
								: currentDateIsDirty
									? 'Click to save now'
									: 'All changes saved'}
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

	<!-- Book -->
	<main class="book-main">
		{#if loading}
			<div class="flex flex-col items-center justify-center py-24 gap-3">
				<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
				</svg>
				<div class="text-muted-foreground text-sm">Opening your diary...</div>
			</div>
		{:else}
			<div
				class="book"
				role="group"
				aria-label="Diary book"
				bind:this={bookEl}
				use:evenWidth
				class:mobile={isMobile}
				class:flipping={!!flip}
				style="--flip-dur: {flipDuration}ms"
				on:touchstart={handleTouchStart}
				on:touchend={handleTouchEnd}
			>
				{#if !isMobile}
					<!-- desktop: two-page spread -->
					<div class="page-slot left">
						<PageFace
							kind="meta"
							side="left"
							date={baseLeft.date}
							mood={baseLeft.mood}
							weather={baseLeft.weather}
							interactive={!flip}
							{moodPresets}
							{weatherPresets}
							onMoodSelect={handleMoodSelect}
							onWeatherSelect={handleWeatherSelect}
						/>
						{#if flip && flip.dir === 'back'}
							<div class="reveal-shade to-left"></div>
						{/if}
						{#if flip && flip.dir === 'fwd'}
							<div class="cover-shade to-left"></div>
						{/if}
					</div>
					<div class="page-slot right">
						{#if flip}
							<PageFace
								kind="content"
								side="right"
								date={baseRight.date}
								content={baseRight.content}
								mood={baseRight.mood}
								weather={baseRight.weather}
								scrollTop={flip.dir === 'back' ? flip.fromScroll : 0}
							/>
							{#if flip.dir === 'fwd'}
								<div class="reveal-shade to-right"></div>
							{:else}
								<div class="cover-shade to-right"></div>
							{/if}
						{:else}
							<PageFace kind="live" side="right" date={view.date}>
								{#key view.date}
									<TiptapEditor
										content={view.content}
										onChange={handleContentChange}
										placeholder="What's on your mind today?"
										emptyStatePrompt="✨ Reflect on today... What will you remember from this day?"
										diaryDate={view.date}
									/>
								{/key}
							</PageFace>
						{/if}
					</div>
					<div class="spine" aria-hidden="true"></div>

					{#if flip && leafFront && leafBack}
						<div class="leaf {flip.dir}" on:animationend={handleLeafAnimationEnd}>
							<div class="face front">
								<PageFace
									kind="content"
									side="right"
									date={leafFront.date}
									content={leafFront.content}
									mood={leafFront.mood}
									weather={leafFront.weather}
									scrollTop={flip.dir === 'fwd' ? flip.fromScroll : 0}
								/>
								<div class="shade"></div>
							</div>
							<div class="face back">
								<PageFace
									kind="meta"
									side="left"
									date={leafBack.date}
									mood={leafBack.mood}
									weather={leafBack.weather}
								/>
								<div class="shade"></div>
							</div>
						</div>
					{/if}
				{:else}
					<!-- mobile: single page -->
					<div class="page-slot single">
						{#if flip}
							<PageFace
								kind="content"
								side="single"
								header
								date={mobileBase.date}
								content={mobileBase.content}
								mood={mobileBase.mood}
								weather={mobileBase.weather}
								scrollTop={flip.dir === 'back' ? flip.fromScroll : 0}
							/>
							{#if flip.dir === 'fwd'}
								<!-- incoming page starts covered by the leaf, shade fades as it lifts -->
								<div class="reveal-shade to-right"></div>
							{:else}
								<!-- old page stays visible; the arriving leaf's shadow sweeps across -->
								<div class="cover-shade to-left"></div>
							{/if}
						{:else}
							<PageFace
								kind="live"
								side="single"
								header
								date={view.date}
								mood={view.mood}
								weather={view.weather}
								interactive
								{moodPresets}
								{weatherPresets}
								onMoodSelect={handleMoodSelect}
								onWeatherSelect={handleWeatherSelect}
							>
								{#key view.date}
									<TiptapEditor
										content={view.content}
										onChange={handleContentChange}
										placeholder="What's on your mind today?"
										emptyStatePrompt="✨ Reflect on today... What will you remember from this day?"
										diaryDate={view.date}
									/>
								{/key}
							</PageFace>
						{/if}
					</div>

					{#if flip && leafFront}
						<div class="leaf single-leaf {flip.dir}" on:animationend={handleLeafAnimationEnd}>
							<div class="face front">
								<PageFace
									kind="content"
									side="single"
									header
									date={leafFront.date}
									content={leafFront.content}
									mood={leafFront.mood}
									weather={leafFront.weather}
									scrollTop={flip.dir === 'fwd' ? flip.fromScroll : 0}
								/>
								<div class="shade"></div>
							</div>
							<div class="face back">
								<PageFace kind="blank" side="single" />
								<div class="shade"></div>
							</div>
						</div>
					{/if}
				{/if}
			</div>
		{/if}
	</main>

	<!-- Catalog overlay -->
	{#if showCatalog}
		<div class="catalog-overlay">
			{#if catalogLoading && !catalogLoaded}
				<div class="absolute inset-0 flex flex-col items-center justify-center gap-3">
					<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
						<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
						<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
					</svg>
					<div class="text-muted-foreground text-sm">Loading contents...</div>
				</div>
			{:else}
				<BookCatalog entries={catalogEntries} currentDate={date} onSelect={handleCatalogSelect} />
			{/if}
		</div>
	{/if}
</div>

<style>
	.book-screen {
		min-height: 100dvh;
		background: hsl(var(--background));
		display: flex;
		flex-direction: column;
	}

	.nav-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 0.35rem;
		border-radius: 0.5rem;
		transition: background-color 0.2s ease, opacity 0.2s ease;
	}
	.nav-btn:hover:not(:disabled) {
		background: hsl(var(--muted) / 0.5);
	}
	.nav-btn:disabled {
		opacity: 0.4;
	}

	.book-main {
		flex: 1;
		display: flex;
		align-items: stretch;
		justify-content: center;
		padding: 0.9rem 1rem 1.1rem;
	}

	/* ---------- book ---------- */
	.book {
		position: relative;
		width: 100%;
		max-width: 68rem;
		height: calc(100dvh - 5.5rem);
		max-height: 50rem;
		min-height: 26rem;
		perspective: 2800px;
		/* NOTE: no `filter: drop-shadow` here — a filter re-rasterizes the whole
		   subtree when the 3D flip leaf appears, causing a visible subpixel pop.
		   The soft ambient shadow lives on ::before as a geometry-only box-shadow. */
	}
	.book.flipping {
		pointer-events: none;
	}

	/* stacked page edges under the book */
	.book::before {
		content: '';
		position: absolute;
		inset: 0;
		border-radius: 12px;
		background: hsl(42 35% 90%);
		box-shadow:
			0 18px 30px hsl(25 40% 12% / 0.28),
			0 2px 0 hsl(43 30% 84%),
			0 4px 0 hsl(44 32% 88%),
			0 6px 0 hsl(43 28% 82%),
			2px 0 0 hsl(43 30% 84%),
			4px 0 0 hsl(44 32% 88%),
			-2px 0 0 hsl(43 30% 84%),
			-4px 0 0 hsl(44 32% 88%);
	}
	:global(.dark) .book::before {
		background: hsl(27 20% 16%);
		box-shadow:
			0 18px 30px hsl(0 0% 0% / 0.45),
			0 2px 0 hsl(27 18% 12%),
			0 4px 0 hsl(27 20% 15%),
			0 6px 0 hsl(27 16% 10%),
			2px 0 0 hsl(27 18% 12%),
			4px 0 0 hsl(27 20% 15%),
			-2px 0 0 hsl(27 18% 12%),
			-4px 0 0 hsl(27 20% 15%);
	}

	.page-slot {
		position: absolute;
		top: 0;
		bottom: 0;
	}
	.page-slot.left {
		left: 0;
		width: 50%;
	}
	.page-slot.right {
		right: 0;
		width: 50%;
	}
	.page-slot.single {
		left: 0;
		width: 100%;
	}

	.spine {
		position: absolute;
		top: 0;
		bottom: 0;
		left: 50%;
		width: 40px;
		transform: translateX(-50%);
		/* must stay above the turning leaf: the gutter shadow is constant,
		   otherwise it visibly vanishes the moment a flip starts */
		z-index: 30;
		pointer-events: none;
		background: linear-gradient(
			to right,
			transparent,
			hsl(25 40% 15% / 0.1) 35%,
			hsl(25 40% 12% / 0.22) 50%,
			hsl(25 40% 15% / 0.1) 65%,
			transparent
		);
	}

	/* ---------- the turning leaf ---------- */
	.leaf {
		position: absolute;
		top: 0;
		bottom: 0;
		left: 50%;
		width: 50%;
		z-index: 20;
		transform-style: preserve-3d;
		transform-origin: left center;
		will-change: transform;
		animation-duration: var(--flip-dur);
		animation-timing-function: cubic-bezier(0.42, 0.05, 0.35, 0.96);
		animation-fill-mode: both;
	}
	.leaf.single-leaf {
		left: 0;
		width: 100%;
	}
	.leaf.fwd {
		animation-name: leaf-fwd;
	}
	.leaf.back {
		animation-name: leaf-bwd;
	}

	@keyframes leaf-fwd {
		0% {
			transform: rotateY(0deg);
		}
		100% {
			transform: rotateY(-180deg);
		}
	}
	@keyframes leaf-bwd {
		0% {
			transform: rotateY(-180deg);
		}
		100% {
			transform: rotateY(0deg);
		}
	}

	.face {
		position: absolute;
		inset: 0;
		backface-visibility: hidden;
		-webkit-backface-visibility: hidden;
		overflow: hidden;
		border-radius: 0 10px 10px 0;
		/* shadow fades in as the page lifts and out as it lands — no pop */
		animation-name: leaf-shadow;
		animation-duration: var(--flip-dur);
		animation-timing-function: cubic-bezier(0.42, 0.05, 0.35, 0.96);
		animation-fill-mode: both;
	}
	@keyframes leaf-shadow {
		0% {
			box-shadow: 0 0 0 0 hsl(25 40% 10% / 0);
		}
		25% {
			box-shadow: 0 10px 34px 0 hsl(25 40% 10% / 0.32);
		}
		75% {
			box-shadow: 0 10px 34px 0 hsl(25 40% 10% / 0.32);
		}
		100% {
			box-shadow: 0 0 0 0 hsl(25 40% 10% / 0);
		}
	}
	.face.back {
		transform: rotateY(180deg);
		border-radius: 10px 0 0 10px;
	}
	.single-leaf .face,
	.single-leaf .face.back {
		border-radius: 10px;
	}

	/* dynamic shading on leaf faces */
	.face .shade {
		position: absolute;
		inset: 0;
		z-index: 10;
		pointer-events: none;
		animation-duration: var(--flip-dur);
		animation-timing-function: cubic-bezier(0.42, 0.05, 0.35, 0.96);
		animation-fill-mode: both;
	}
	/* face that is turning away gets darker; landing face brightens */
	.leaf.fwd .front .shade {
		background: linear-gradient(to left, hsl(25 45% 8% / 0.42), transparent 60%);
		animation-name: shade-appear;
	}
	.leaf.fwd .back .shade {
		background: linear-gradient(to right, hsl(25 45% 8% / 0.38), transparent 60%);
		animation-name: shade-fade;
	}
	.leaf.back .front .shade {
		background: linear-gradient(to left, hsl(25 45% 8% / 0.42), transparent 60%);
		animation-name: shade-fade;
	}
	.leaf.back .back .shade {
		background: linear-gradient(to right, hsl(25 45% 8% / 0.38), transparent 60%);
		animation-name: shade-appear;
	}

	@keyframes shade-appear {
		0% {
			opacity: 0;
		}
		100% {
			opacity: 1;
		}
	}
	@keyframes shade-fade {
		0% {
			opacity: 1;
		}
		100% {
			opacity: 0;
		}
	}

	/* shading on the base pages */
	.reveal-shade,
	.cover-shade {
		position: absolute;
		inset: 0;
		z-index: 6;
		pointer-events: none;
		animation-duration: var(--flip-dur);
		animation-timing-function: cubic-bezier(0.42, 0.05, 0.35, 0.96);
		animation-fill-mode: both;
	}
	/* clip the shade overlays to the page's rounded corners, otherwise their
	   dark edge shows as a square remnant in the corner notches */
	.page-slot.left .reveal-shade,
	.page-slot.left .cover-shade {
		border-radius: 10px 0 0 10px;
	}
	.page-slot.right .reveal-shade,
	.page-slot.right .cover-shade {
		border-radius: 0 10px 10px 0;
	}
	.page-slot.single .reveal-shade,
	.page-slot.single .cover-shade {
		border-radius: 10px;
	}
	.reveal-shade.to-right {
		background: linear-gradient(to right, hsl(25 45% 8% / 0.45), transparent 65%);
		animation-name: shade-fade;
	}
	.reveal-shade.to-left {
		background: linear-gradient(to left, hsl(25 45% 8% / 0.45), transparent 65%);
		animation-name: shade-fade;
	}
	.cover-shade.to-right {
		background: linear-gradient(to right, hsl(25 45% 8% / 0.35), transparent 65%);
		animation-name: shade-pulse;
	}
	.cover-shade.to-left {
		background: linear-gradient(to left, hsl(25 45% 8% / 0.35), transparent 65%);
		animation-name: shade-pulse;
	}
	/* the turning page's cast shadow sweeps across and passes — ends at 0
	   so removing the overlay after the flip causes no visible change */
	@keyframes shade-pulse {
		0% {
			opacity: 0;
		}
		55% {
			opacity: 0.55;
		}
		100% {
			opacity: 0;
		}
	}

	/* mobile adjustments */
	.book.mobile {
		max-width: 44rem;
		height: calc(100dvh - 5rem);
		perspective: 2200px;
	}
	/* on mobile the fully-turned leaf sits just off the book's left edge and
	   its antialiased border would show as a 1px sliver — fade it at the
	   offscreen endpoint (invisible to the eye, kills the artifact) */
	.book.mobile .leaf.fwd {
		animation-name: leaf-fwd-m;
	}
	.book.mobile .leaf.back {
		animation-name: leaf-bwd-m;
	}
	@keyframes leaf-fwd-m {
		0% {
			transform: rotateY(0deg);
			opacity: 1;
		}
		97% {
			opacity: 1;
		}
		100% {
			transform: rotateY(-180deg);
			opacity: 0;
		}
	}
	@keyframes leaf-bwd-m {
		0% {
			transform: rotateY(-180deg);
			opacity: 0;
		}
		3% {
			opacity: 1;
		}
		100% {
			transform: rotateY(0deg);
			opacity: 1;
		}
	}

	/* ---------- catalog overlay ---------- */
	.catalog-overlay {
		position: fixed;
		top: 2.75rem;
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 25;
		background: hsl(var(--background) / 0.97);
		backdrop-filter: blur(8px);
		-webkit-backdrop-filter: blur(8px);
	}
</style>
