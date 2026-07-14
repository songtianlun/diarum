<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import TiptapEditor from '$lib/components/editor/TiptapEditor.svelte';
	import PageFace from '$lib/components/book/PageFace.svelte';
	import BookCatalog from '$lib/components/book/BookCatalog.svelte';
	import BookTableOfContents from '$lib/components/book/BookTableOfContents.svelte';
	import DiaryShareModal from '$lib/components/share/DiaryShareModal.svelte';
	import EntryNav from '$lib/components/ui/EntryNav.svelte';
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
		clampToToday,
		formatDisplayDate,
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
		/** false only for a placeholder awaiting its first fetch */
		loaded: boolean;
	}

	const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

	let ready = false;
	let loading = true;
	let date = getToday();
	let view: View = { date, content: '', mood: '', weather: '', loaded: false };

	// flip animation state
	let flip: { dir: 'fwd' | 'back'; from: View; to: View; fromScroll: number } | null = null;
	let bookEl: HTMLElement | undefined;
	let animating = false;
	let committed = false;
	let pendingTarget: string | null = null;
	let flipDuration = 420;
	let commitTimer: ReturnType<typeof setTimeout> | null = null;

	// catalog (full calendar overlay, opened from the date button)
	let showCatalog = false;
	let catalogEntries: CalendarDiaryMeta[] = [];
	let catalogLoaded = false;
	let catalogLoading = false;

	// contents (per-entry heading outline) — desktop swaps the left page in
	// place, mobile opens a left drawer
	let showToc = false;
	let showTocDrawer = false;

	// share
	let showShareModal = false;
	let selectedContent = '';
	let shareSelectedContent = '';
	let shareOpenedByMouse = false;

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

	// leaf/base data derived from flip state
	$: leafFront = flip ? (flip.dir === 'fwd' ? flip.from : flip.to) : null;
	$: leafBack = flip ? (flip.dir === 'fwd' ? flip.to : flip.from) : null;
	$: baseLeft = flip ? (flip.dir === 'fwd' ? flip.from : flip.to) : view;
	$: baseRight = flip ? (flip.dir === 'fwd' ? flip.to : flip.from) : view;
	$: mobileBase = flip ? (flip.dir === 'fwd' ? flip.to : flip.from) : view;

	async function fetchView(d: string): Promise<View> {
		const dirty = getCachedContent(d);
		if (dirty?.isDirty) {
			return { date: d, content: dirty.content, mood: dirty.mood || '', weather: dirty.weather || '', loaded: true };
		}
		const cached = viewCache.get(d);
		if (cached) return cached;
		const diary = await getDiaryByDate(d);
		const v: View = {
			date: d,
			content: diary?.content || '',
			mood: diary?.mood || '',
			weather: diary?.weather || '',
			loaded: true
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
			to = { date: target, content: dirty.content, mood: dirty.mood || '', weather: dirty.weather || '', loaded: true };
		} else if (viewCache.has(target)) {
			to = viewCache.get(target)!;
		} else {
			to = { date: target, content: '', mood: '', weather: '', loaded: false };
			needsFetch = true;
		}

		// preserve the outgoing page's scroll position on its static snapshot
		const fromScroll = bookEl?.querySelector('.content-scroll')?.scrollTop ?? 0;

		flipDuration = pendingTarget ? 260 : 420;
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
				await goto(`/diary/${to.date}`, { noScroll: true, keepFocus: true });
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

	function toggleToc() {
		if (isMobile) {
			showTocDrawer = !showTocDrawer;
		} else {
			showToc = !showToc;
		}
	}

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
		if (e.key === 'Escape' && showTocDrawer) {
			showTocDrawer = false;
			return;
		}
		if (isEditing() || showCatalog || showTocDrawer) return;
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
		const onMq = (e: MediaQueryListEvent) => {
			isMobile = e.matches;
			// the desktop panel and mobile drawer are mutually exclusive
			// presentations of the same feature — drop whichever doesn't apply
			showTocDrawer = false;
		};
		mq.addEventListener('change', onMq);

		// initial date from route
		let initial = $page.params.date ?? getToday();
		if (!DATE_RE.test(initial) || isNaN(new Date(initial + 'T00:00:00').getTime())) {
			initial = getToday();
		}
		initial = clampToToday(initial);
		date = initial;
		if ($page.params.date !== initial) {
			goto(`/diary/${initial}`, { replaceState: true, noScroll: true });
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
	<title>{formatDisplayDate(date)} - Diarum</title>
</svelte:head>

<div class="book-screen">
	<!-- Toolbar (shared with the classic diary entry view) -->
	<EntryNav
		date={headerDate}
		busy={animating}
		onPrevDay={() => navigateTo(getPreviousDay(date))}
		onNextDay={() => navigateTo(getNextDay(date))}
		onDateTextClick={toggleCatalog}
		dateTextActive={showCatalog}
		onShareMouseDown={captureShareSelection}
		onShareClick={openShareModal}
		tocActive={isMobile ? showTocDrawer : showToc}
		onTocClick={toggleToc}
		isOnline={$onlineState.isOnline}
		isSyncing={isAnySyncing}
		isDirty={currentDateIsDirty}
		onSyncClick={() => forceSyncNow()}
	/>

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
						{#if showToc && !flip}
							<PageFace kind="toc" side="left" date={view.date} content={view.content} />
						{:else}
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
						{/if}
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
										bind:selectedContent
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
							<div class="lift-shadow front"></div>
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
							<div class="lift-shadow back"></div>
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
										bind:selectedContent
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
							<div class="lift-shadow front"></div>
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
							<div class="lift-shadow back"></div>
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

	<!-- Mobile contents drawer -->
	{#if showTocDrawer}
		<button
			class="toc-backdrop"
			on:click={() => (showTocDrawer = false)}
			aria-label="Close contents"
		></button>
		<div class="toc-drawer animate-slide-in-left">
			<div class="toc-drawer-header">
				<div>
					<div class="toc-drawer-title">Contents</div>
					<div class="toc-drawer-date">{formatDisplayDate(view.date)} · {getDayOfWeek(view.date)}</div>
				</div>
				<button
					on:click={() => (showTocDrawer = false)}
					class="toc-drawer-close"
					aria-label="Close"
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
					</svg>
				</button>
			</div>
			<div class="toc-drawer-scroll">
				<BookTableOfContents content={view.content} onNavigate={() => (showTocDrawer = false)} />
			</div>
		</div>
	{/if}
</div>

<!-- Share Modal -->
<DiaryShareModal
	isOpen={showShareModal}
	{date}
	content={view.content}
	selectedContent={shareSelectedContent}
	onClose={() => (showShareModal = false)}
/>

<style>
	.book-screen {
		min-height: 100dvh;
		background: hsl(var(--background));
		display: flex;
		flex-direction: column;
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
	}
	.face.back {
		transform: rotateY(180deg);
		border-radius: 10px 0 0 10px;
	}
	.single-leaf .face,
	.single-leaf .face.back {
		border-radius: 10px;
	}

	/* ---------- leaf drop shadow ----------
	   A plain, un-clipped layer sitting just behind its matching `.face`
	   (same inset/rounding, no background) whose *only* visible pixels are
	   the box-shadow bleeding outside its own box — the face fully covers
	   the rest. The shadow value itself never changes; only `opacity`
	   animates, which the compositor can do purely on the GPU. Animating
	   box-shadow directly (the old approach) forces a full repaint on every
	   frame and was the main source of jank on mobile. */
	.lift-shadow {
		position: absolute;
		inset: 0;
		border-radius: 0 10px 10px 0;
		box-shadow: 0 10px 34px 0 hsl(25 40% 10% / 0.32);
		backface-visibility: hidden;
		-webkit-backface-visibility: hidden;
		pointer-events: none;
		will-change: opacity;
		animation-name: shade-pulse-soft;
		animation-duration: var(--flip-dur);
		animation-timing-function: cubic-bezier(0.42, 0.05, 0.35, 0.96);
		animation-fill-mode: both;
	}
	.lift-shadow.back {
		transform: rotateY(180deg);
		border-radius: 10px 0 0 10px;
	}
	.single-leaf .lift-shadow,
	.single-leaf .lift-shadow.back {
		border-radius: 10px;
	}
	@keyframes shade-pulse-soft {
		0% {
			opacity: 0;
		}
		25% {
			opacity: 1;
		}
		75% {
			opacity: 1;
		}
		100% {
			opacity: 0;
		}
	}

	/* dynamic shading on leaf faces */
	.face .shade {
		position: absolute;
		inset: 0;
		z-index: 10;
		pointer-events: none;
		will-change: opacity;
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
		will-change: opacity;
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

	/* ---------- mobile contents drawer ---------- */
	.toc-backdrop {
		position: fixed;
		inset: 0;
		z-index: 40;
		background: hsl(0 0% 0% / 0.4);
		backdrop-filter: blur(3px);
		-webkit-backdrop-filter: blur(3px);
	}
	.toc-drawer {
		position: fixed;
		top: 0;
		bottom: 0;
		left: 0;
		width: min(20rem, 84vw);
		z-index: 50;
		display: flex;
		flex-direction: column;
		background:
			linear-gradient(120deg, hsl(45 42% 97%), hsl(43 38% 94%)),
			hsl(44 40% 96%);
		color: hsl(25 35% 25%);
		box-shadow: 6px 0 28px hsl(25 40% 10% / 0.28);
		border-radius: 0 14px 14px 0;
	}
	:global(.dark) .toc-drawer {
		background:
			linear-gradient(120deg, hsl(28 22% 15%), hsl(26 20% 12%)),
			hsl(27 22% 13%);
		color: hsl(45 25% 88%);
	}
	.toc-drawer-header {
		flex-shrink: 0;
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 0.75rem;
		padding: 1.1rem 1.1rem 0.85rem;
		border-bottom: 1px solid hsl(30 30% 60% / 0.18);
	}
	.toc-drawer-title {
		font-family: ui-serif, Georgia, serif;
		font-size: 1.15rem;
		font-weight: 600;
	}
	.toc-drawer-date {
		font-size: 0.75rem;
		color: hsl(30 20% 50%);
		margin-top: 0.15rem;
	}
	:global(.dark) .toc-drawer-date {
		color: hsl(40 20% 62%);
	}
	.toc-drawer-close {
		flex-shrink: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 0.4rem;
		border-radius: 0.5rem;
		color: hsl(30 20% 45%);
		transition: background-color 0.15s ease, color 0.15s ease;
	}
	.toc-drawer-close:hover {
		background: hsl(30 30% 60% / 0.16);
		color: hsl(25 35% 25%);
	}
	:global(.dark) .toc-drawer-close {
		color: hsl(40 20% 70%);
	}
	:global(.dark) .toc-drawer-close:hover {
		background: hsl(40 25% 45% / 0.18);
		color: hsl(45 25% 92%);
	}
	.toc-drawer-scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overflow-x: hidden;
		overscroll-behavior: contain;
		padding: 0.75rem;
	}
</style>
