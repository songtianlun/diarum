<script lang="ts">
	/**
	 * Windows 95 visual style for the diary entry route.
	 *
	 * Desktop: a teal desktop holding two windows — an Explorer-ish rail on the
	 * left (contents tree / date picker / outline / status) and a Notepad window
	 * on the right that hosts the same TipTap editor every other style uses.
	 * A taskbar at the foot tracks which window has focus.
	 *
	 * Narrow screens fall back to a Windows CE handheld layout: one maximised
	 * window, the date stepper inline under the menu bar, and the two trees
	 * moved behind taskbar buttons.
	 *
	 * Nothing here changes diary behaviour — loading, dirty-tracking, syncing,
	 * mood/weather and sharing all go through the exact same stores and APIs as
	 * the classic view.
	 */
	import './win95.css';

	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';

	import TiptapEditor from '$lib/components/editor/TiptapEditor.svelte';
	import DiaryShareModal from '$lib/components/share/DiaryShareModal.svelte';
	import Win95Icon from './Win95Icon.svelte';
	import Win95Tree from './Win95Tree.svelte';
	import Win95DateNav from './Win95DateNav.svelte';
	import Win95Outline from './Win95Outline.svelte';
	import Win95StatusPanel from './Win95StatusPanel.svelte';
	import Win95MenuBar from './Win95MenuBar.svelte';
	import Win95Dialog from './Win95Dialog.svelte';
	import Win95EmojiDialog from './Win95EmojiDialog.svelte';
	import type { Win95Menu } from './types';

	import { getDiaryByDate, getDatesWithDiaries, type CalendarDiaryMeta } from '$lib/api/diaries';
	import { isAuthenticated } from '$lib/api/client';
	import { getDiaryEmojiSettings } from '$lib/api/settings';
	import { DEFAULT_MOOD_OPTIONS, DEFAULT_WEATHER_OPTIONS } from '$lib/utils/diaryEmoji';
	import {
		formatDisplayDate,
		getDayOfWeek,
		getNextDay,
		getPreviousDay,
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
		initDiaryCache
	} from '$lib/stores/diaryCache';
	import { onlineState } from '$lib/stores/onlineStatus';
	import { t } from '$lib/i18n';

	// ---------------------------------------------------------------- state

	let content = '';
	let loading = true;
	let loadRequestId = 0;
	let cacheReady = false;
	let previousDate = '';
	let date = getToday();

	let selectedMood = '';
	let selectedWeather = '';
	let moodPresets: string[] = [...DEFAULT_MOOD_OPTIONS];
	let weatherPresets: string[] = [...DEFAULT_WEATHER_OPTIONS];

	// share (identical capture dance to the other views)
	let showShareModal = false;
	let selectedContent = '';
	let shareSelectedContent = '';
	let shareOpenedByMouse = false;

	// contents tree
	let entries: CalendarDiaryMeta[] = [];
	let entriesLoading = true;

	// shell
	let isMobile = false;
	let activeWindow: 'rail' | 'pad' = 'pad';
	let outlineOpen = true;
	let emojiDialog: 'mood' | 'weather' | null = null;
	let showAbout = false;
	let sheet: 'contents' | 'outline' | null = null;
	let clock = '';
	let clockTimer: ReturnType<typeof setInterval> | null = null;

	$: date = $page.params.date ?? getToday();
	$: currentDateIsDirty = date ? $diaryCache[date]?.isDirty || false : false;
	$: isAnySyncing = $syncState.isSyncing;
	$: atToday = isToday(date);
	$: docName = `${date}.txt`;
	$: padTitle = $t('win95.notepadTitle', { name: `${docName}${currentDateIsDirty ? ' *' : ''}` });
	$: charCount = countChars(content);

	function countChars(html: string): number {
		if (!html) return 0;
		return html
			.replace(/<[^>]*>/g, '')
			.replace(/&nbsp;/g, ' ')
			.replace(/\s+/g, '')
			.trim().length;
	}

	// ------------------------------------------------------------- loading

	async function loadDiary(targetDate: string) {
		const requestId = ++loadRequestId;
		const cached = getCachedContent(targetDate);

		// An unsynced local draft always wins; never overwrite it from the server.
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
		loading = true;

		try {
			const diary = await getDiaryByDate(targetDate);
			if (requestId !== loadRequestId) return;
			updateFromServer(targetDate, diary);
			if (requestId !== loadRequestId) return;
			content = diary?.content || '';
			selectedMood = diary?.mood || '';
			selectedWeather = diary?.weather || '';
		} catch (error) {
			console.error('Failed to load diary:', error);
			if (cached?.isDirty) {
				content = cached.content;
				selectedMood = cached.mood || '';
				selectedWeather = cached.weather || '';
			}
		}
		loading = false;
	}

	async function loadEntries() {
		entriesLoading = true;
		try {
			entries = await getDatesWithDiaries('1970-01-01', getToday());
		} catch (error) {
			console.error('Failed to load diary index:', error);
			entries = [];
		}
		entriesLoading = false;
	}

	async function loadEmojiPresets() {
		try {
			const settings = await getDiaryEmojiSettings();
			moodPresets = [...settings.mood_options];
			weatherPresets = [...settings.weather_options];
		} catch (error) {
			console.error('Failed to load mood/weather presets:', error);
		}
	}

	/**
	 * Keep the contents tree honest while typing: an entry appears the moment it
	 * has content and disappears again when everything is cleared, matching what
	 * the server would store.
	 */
	function syncTreeEntry() {
		const meaningful = countChars(content) > 0 || !!selectedMood || !!selectedWeather;
		const index = entries.findIndex((e) => e.date === date);

		if (!meaningful) {
			if (index !== -1) entries = entries.filter((e) => e.date !== date);
			return;
		}

		const next = { date, mood: selectedMood, weather: selectedWeather };
		if (index === -1) {
			entries = [...entries, next];
		} else if (entries[index].mood !== selectedMood || entries[index].weather !== selectedWeather) {
			entries = entries.map((e) => (e.date === date ? next : e));
		}
	}

	// ------------------------------------------------------------- actions

	function commit() {
		updateLocalCache(date, { content, mood: selectedMood, weather: selectedWeather });
		syncTreeEntry();
	}

	function handleContentChange(next: string) {
		content = next;
		commit();
	}

	function handleMoodSelect(emoji: string) {
		selectedMood = selectedMood === emoji ? '' : emoji;
		commit();
	}

	function handleWeatherSelect(emoji: string) {
		selectedWeather = selectedWeather === emoji ? '' : emoji;
		commit();
	}

	async function handleManualSave() {
		await forceSyncNow();
	}

	function openDate(target: string) {
		if (target === date) return;
		sheet = null;
		goto(`/diary/${target}`);
	}

	function goPrev() {
		openDate(getPreviousDay(date));
	}

	function goNext() {
		if (atToday) return;
		openDate(getNextDay(date));
	}

	function goToday() {
		openDate(getToday());
	}

	function navigate(href: string) {
		goto(href);
	}

	function captureShareSelection() {
		shareSelectedContent = selectedContent;
		shareOpenedByMouse = true;
	}

	function openShareModal() {
		// Keyboard path: mousedown never fired, so drop any stale snapshot.
		if (!shareOpenedByMouse) shareSelectedContent = '';
		shareOpenedByMouse = false;
		showShareModal = true;
	}

	function shareFromMenu() {
		shareSelectedContent = '';
		shareOpenedByMouse = false;
		showShareModal = true;
	}

	// --------------------------------------------------------------- menus

	/** Underline the first letter as an access key, but only for latin labels. */
	function mn(label: string): number | undefined {
		return /^[\x20-\x7E]+$/.test(label) ? 0 : undefined;
	}

	// Menu handlers live outside the reactive block on purpose: assigning to a
	// variable inside `$: menus = [...]` would drop that variable from the
	// block's dependencies and freeze the checkmarks.
	function toggleSheet(which: 'contents' | 'outline') {
		sheet = sheet === which ? null : which;
	}

	function viewContents() {
		if (isMobile) toggleSheet('contents');
	}

	function viewOutline() {
		if (isMobile) toggleSheet('outline');
		else outlineOpen = !outlineOpen;
	}

	function openMoodDialog() {
		emojiDialog = 'mood';
	}

	function openWeatherDialog() {
		emojiDialog = 'weather';
	}

	function clearMood() {
		handleMoodSelect(selectedMood);
	}

	function clearWeather() {
		handleWeatherSelect(selectedWeather);
	}

	function openAbout() {
		showAbout = true;
	}

	function openCalendarPage() {
		navigate('/diary');
	}

	let menus: Win95Menu[] = [];

	$: menus = [
		{
			label: $t('win95.menuFile'),
			mnemonic: mn($t('win95.menuFile')),
			items: [
				{ label: $t('win95.fileToday'), shortcut: 'Ctrl+T', disabled: atToday, action: goToday },
				{ label: $t('win95.fileCalendar'), action: openCalendarPage },
				{ sep: true },
				{
					label: $t('win95.fileSave'),
					shortcut: 'Ctrl+S',
					disabled: !currentDateIsDirty,
					action: handleManualSave
				},
				{ label: $t('win95.fileShare'), action: shareFromMenu },
				{ sep: true },
				{ label: $t('win95.fileExit'), action: openCalendarPage }
			]
		},
		{
			label: $t('win95.menuEdit'),
			mnemonic: mn($t('win95.menuEdit')),
			items: [
				{ label: $t('win95.editMood'), action: openMoodDialog },
				{ label: $t('win95.editWeather'), action: openWeatherDialog },
				{ sep: true },
				{ label: $t('win95.editClearMood'), disabled: !selectedMood, action: clearMood },
				{ label: $t('win95.editClearWeather'), disabled: !selectedWeather, action: clearWeather }
			]
		},
		{
			label: $t('win95.menuView'),
			mnemonic: mn($t('win95.menuView')),
			items: [
				{
					label: $t('win95.viewContents'),
					checked: isMobile ? sheet === 'contents' : true,
					disabled: !isMobile,
					action: viewContents
				},
				{
					label: $t('win95.viewOutline'),
					checked: isMobile ? sheet === 'outline' : outlineOpen,
					action: viewOutline
				},
				{ sep: true },
				{ label: $t('win95.viewAssistant'), action: () => navigate('/assistant') },
				{ label: $t('win95.viewMedia'), action: () => navigate('/media') },
				{ label: $t('win95.viewSearch'), action: () => navigate('/search') },
				{ sep: true },
				{ label: $t('win95.viewSettings'), action: () => navigate('/settings') }
			]
		},
		{
			label: $t('win95.menuHelp'),
			mnemonic: mn($t('win95.menuHelp')),
			items: [{ label: $t('win95.helpAbout'), action: openAbout }]
		}
	];

	// ------------------------------------------------------------ lifecycle

	function tickClock() {
		const now = new Date();
		clock = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
	}

	function handleKeyboard(event: KeyboardEvent) {
		if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
			event.preventDefault();
			void handleManualSave();
			return;
		}
		if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 't') {
			event.preventDefault();
			goToday();
		}
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}

		// The shell owns the viewport: kill document scrolling and the warm
		// paper background from app.css while this style is mounted.
		document.documentElement.classList.add('w95-active');

		initDiaryCache();
		cacheReady = true;
		void loadEmojiPresets();
		void loadEntries();

		const mq = window.matchMedia('(max-width: 820px)');
		isMobile = mq.matches;
		const onMq = (e: MediaQueryListEvent) => {
			isMobile = e.matches;
			sheet = null;
		};
		mq.addEventListener('change', onMq);

		tickClock();
		clockTimer = setInterval(tickClock, 20000);

		window.addEventListener('keydown', handleKeyboard);
		return () => {
			window.removeEventListener('keydown', handleKeyboard);
			mq.removeEventListener('change', onMq);
		};
	});

	onDestroy(() => {
		if (clockTimer) clearInterval(clockTimer);
		if (typeof document !== 'undefined') {
			document.documentElement.classList.remove('w95-active');
		}
	});

	$: if (cacheReady && date && date !== previousDate && typeof window !== 'undefined') {
		previousDate = date;
		void loadDiary(date);
	}
</script>

<svelte:head>
	<title>{formatDisplayDate(date)} - Diarum</title>
</svelte:head>

<div class="w95-root">
	{#if isMobile}
		<!-- ------------------------------------------------ Windows CE layout -->
		<div class="ce">
			<div class="w95-window ce-window">
				<div class="w95-titlebar">
					<Win95Icon name="notepad" size={14} />
					<span class="w95-title-text">{docName}{currentDateIsDirty ? ' *' : ''}</span>
					<div class="w95-title-buttons">
						<button
							type="button"
							class="w95-title-btn"
							title={$t('win95.fileSave')}
							on:click={handleManualSave}
						>
							<Win95Icon name="disk" size={11} />
						</button>
					</div>
				</div>

				<Win95MenuBar {menus} />

				<div class="ce-navrow">
					<Win95DateNav
						{date}
						{entries}
						busy={loading}
						compact
						onPrev={goPrev}
						onNext={goNext}
						onPick={openDate}
						onOpenOverview={() => navigate('/diary')}
					/>
					<button
						type="button"
						class="w95-btn w95-iconbtn"
						title={$t('entryNav.shareAsImage')}
						on:mousedown={captureShareSelection}
						on:click={openShareModal}
					>
						<Win95Icon name="share" size={16} />
					</button>
				</div>

				<div class="w95-field pad-client">
					<div class="pad-scroll w95-editor-scroll">
						{#if loading}
							<div class="loading">{$t('win95.opening')}</div>
						{:else}
							<TiptapEditor
								{content}
								bind:selectedContent
								onChange={handleContentChange}
								placeholder={$t('win95.placeholder')}
								diaryDate={date}
							/>
						{/if}
					</div>
				</div>

				<div class="w95-statusbar">
					<button
						type="button"
						class="w95-status-cell clickable"
						on:click={() => (emojiDialog = 'mood')}
					>
						<span>{selectedMood || '—'}</span>
					</button>
					<button
						type="button"
						class="w95-status-cell clickable"
						on:click={() => (emojiDialog = 'weather')}
					>
						<span>{selectedWeather || '—'}</span>
					</button>
					<span class="w95-status-cell grow">{$t('win95.chars', { n: charCount })}</span>
				</div>
			</div>
		</div>

		<div class="w95-taskbar">
			<button
				type="button"
				class="w95-btn ce-task"
				class:pressed={sheet === 'contents'}
				on:click={() => (sheet = sheet === 'contents' ? null : 'contents')}
			>
				<Win95Icon name="contents" size={14} />
				<span>{$t('win95.tabContents')}</span>
			</button>
			<button
				type="button"
				class="w95-btn ce-task"
				class:pressed={sheet === 'outline'}
				on:click={() => (sheet = sheet === 'outline' ? null : 'outline')}
			>
				<Win95Icon name="outline" size={14} />
				<span>{$t('win95.tabOutline')}</span>
			</button>

			<div class="w95-tray">
				<Win95Icon
					name={!$onlineState.isOnline
						? 'offline'
						: isAnySyncing
							? 'syncing'
							: currentDateIsDirty
								? 'dirty'
								: 'saved'}
					size={14}
				/>
				<span>{clock}</span>
			</div>
		</div>
	{:else}
		<!-- ---------------------------------------------------- Win95 desktop -->
		<div class="desk">
			<!-- svelte-ignore a11y-no-static-element-interactions -->
			<section
				class="w95-window rail"
				on:mousedown={() => (activeWindow = 'rail')}
				on:focusin={() => (activeWindow = 'rail')}
			>
				<div class="w95-titlebar" class:inactive={activeWindow !== 'rail'}>
					<Win95Icon name="diarum" size={14} />
					<span class="w95-title-text">Diarum</span>
				</div>

				<div class="rail-body">
					<Win95Tree
						{entries}
						currentDate={date}
						loading={entriesLoading}
						onSelect={openDate}
						onToday={goToday}
					/>

					<Win95DateNav
						{date}
						{entries}
						busy={loading}
						onPrev={goPrev}
						onNext={goNext}
						onPick={openDate}
						onOpenOverview={() => navigate('/diary')}
					/>

					{#if outlineOpen}
						<Win95Outline {content} />
					{/if}

					<Win95StatusPanel
						isOnline={$onlineState.isOnline}
						isSyncing={isAnySyncing}
						isDirty={currentDateIsDirty}
						onSync={handleManualSave}
						onShareMouseDown={captureShareSelection}
						onShare={openShareModal}
						onNavigate={navigate}
						{outlineOpen}
						onToggleOutline={() => (outlineOpen = !outlineOpen)}
					/>
				</div>
			</section>

			<!-- svelte-ignore a11y-no-static-element-interactions -->
			<section
				class="w95-window pad"
				on:mousedown={() => (activeWindow = 'pad')}
				on:focusin={() => (activeWindow = 'pad')}
			>
				<div class="w95-titlebar" class:inactive={activeWindow !== 'pad'}>
					<Win95Icon name="notepad" size={14} />
					<span class="w95-title-text">{padTitle}</span>
					<div class="w95-title-buttons">
						<button type="button" class="w95-title-btn" disabled aria-label="Minimize">
							<svg width="6" height="6" viewBox="0 0 6 6" shape-rendering="crispEdges"
								><path d="M0 4h5v2H0z" fill="currentColor" /></svg
							>
						</button>
						<button type="button" class="w95-title-btn" disabled aria-label="Maximize">
							<svg width="8" height="8" viewBox="0 0 8 8" shape-rendering="crispEdges"
								><path d="M0 0h8v8H0V0zm1 2v5h6V2H1z" fill="currentColor" /></svg
							>
						</button>
						<button
							type="button"
							class="w95-title-btn"
							aria-label={$t('win95.fileExit')}
							on:click={() => navigate('/diary')}
						>
							<svg width="8" height="7" viewBox="0 0 8 7" shape-rendering="crispEdges">
								<path
									d="M0 0h2v1h1v1h2V1h1V0h2v1H6v1H5v1h1v1h1v1h1v1H6V5H5V4H3v1H2v1H0V5h1V4h1V3h1V2H1V1H0z"
									fill="currentColor"
								/>
							</svg>
						</button>
					</div>
				</div>

				<Win95MenuBar {menus} />

				<div class="w95-field pad-client">
					<div class="pad-scroll w95-editor-scroll">
						{#if loading}
							<div class="loading">{$t('win95.opening')}</div>
						{:else}
							<TiptapEditor
								{content}
								bind:selectedContent
								onChange={handleContentChange}
								placeholder={$t('win95.placeholder')}
								diaryDate={date}
							/>
						{/if}
					</div>
				</div>

				<div class="w95-statusbar">
					<button
						type="button"
						class="w95-status-cell clickable"
						title={$t('win95.editMood')}
						on:click={() => (emojiDialog = 'mood')}
					>
						<span class="cap">{$t('win95.mood')}</span>
						<span class="emo">{selectedMood || $t('win95.none')}</span>
					</button>
					<button
						type="button"
						class="w95-status-cell clickable"
						title={$t('win95.editWeather')}
						on:click={() => (emojiDialog = 'weather')}
					>
						<span class="cap">{$t('win95.weather')}</span>
						<span class="emo">{selectedWeather || $t('win95.none')}</span>
					</button>
					<span class="w95-status-cell grow">
						{formatDisplayDate(date)} · {getDayOfWeek(date)}
					</span>
					<span class="w95-status-cell">{$t('win95.chars', { n: charCount })}</span>
				</div>
			</section>
		</div>

		<div class="w95-taskbar">
			<button type="button" class="w95-btn start" on:click={() => navigate('/')}>
				<Win95Icon name="diarum" size={15} />
				<span><u>D</u>iarum</span>
			</button>
			<span class="w95-vsep"></span>
			<button
				type="button"
				class="w95-btn task"
				class:pressed={activeWindow === 'rail'}
				on:click={() => (activeWindow = 'rail')}
			>
				<Win95Icon name="contents" size={14} />
				<span>{$t('win95.contentsTitle')}</span>
			</button>
			<button
				type="button"
				class="w95-btn task"
				class:pressed={activeWindow === 'pad'}
				on:click={() => (activeWindow = 'pad')}
			>
				<Win95Icon name="notepad" size={14} />
				<span>{padTitle}</span>
			</button>

			<div class="w95-tray">
				<Win95Icon
					name={!$onlineState.isOnline
						? 'offline'
						: isAnySyncing
							? 'syncing'
							: currentDateIsDirty
								? 'dirty'
								: 'saved'}
					size={14}
					title={currentDateIsDirty ? $t('win95.statusDirty') : $t('win95.statusSaved')}
				/>
				<span>{clock}</span>
			</div>
		</div>
	{/if}

	<!-- ------------------------------------------------------------ dialogs -->

	{#if emojiDialog === 'mood'}
		<Win95EmojiDialog
			title={$t('win95.moodDialogTitle')}
			options={moodPresets}
			value={selectedMood}
			onPick={(emoji) => {
				selectedMood = emoji;
				commit();
			}}
			onClose={() => (emojiDialog = null)}
		/>
	{:else if emojiDialog === 'weather'}
		<Win95EmojiDialog
			title={$t('win95.weatherDialogTitle')}
			options={weatherPresets}
			value={selectedWeather}
			onPick={(emoji) => {
				selectedWeather = emoji;
				commit();
			}}
			onClose={() => (emojiDialog = null)}
		/>
	{/if}

	{#if sheet}
		<Win95Dialog
			title={sheet === 'contents' ? $t('win95.contentsTitle') : $t('win95.outlineTitle')}
			icon={sheet === 'contents' ? 'contents' : 'outline'}
			full
			onClose={() => (sheet = null)}
		>
			{#if sheet === 'contents'}
				<Win95Tree
					{entries}
					currentDate={date}
					loading={entriesLoading}
					onSelect={openDate}
					onToday={() => {
						sheet = null;
						goToday();
					}}
				/>
			{:else}
				<Win95Outline {content} onNavigate={() => (sheet = null)} />
			{/if}
		</Win95Dialog>
	{/if}

	{#if showAbout}
		<Win95Dialog title={$t('win95.aboutTitle')} icon="info" onClose={() => (showAbout = false)}>
			<div class="about">
				<Win95Icon name="diarum" size={40} />
				<div class="about-text">
					<div class="about-title">{$t('win95.aboutProduct')}</div>
					<div>{$t('win95.aboutVersion')}</div>
					<div class="w95-sep"></div>
					<div class="w95-hint">{$t('win95.aboutMemo')}</div>
				</div>
			</div>
			<svelte:fragment slot="actions">
				<button type="button" class="w95-btn ok" on:click={() => (showAbout = false)}>
					{$t('win95.ok')}
				</button>
			</svelte:fragment>
		</Win95Dialog>
	{/if}
</div>

<DiaryShareModal
	isOpen={showShareModal}
	{date}
	{content}
	selectedContent={shareSelectedContent}
	onClose={() => (showShareModal = false)}
/>

<style>
	/* ------------------------------------------------------------- desktop */

	.desk {
		flex: 1;
		min-height: 0;
		display: flex;
		gap: 6px;
		padding: 8px 8px 6px;
	}

	.rail {
		width: 268px;
		flex-shrink: 0;
	}

	.rail-body {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
		gap: 5px;
		padding-top: 4px;
	}

	.pad {
		flex: 1;
		min-width: 0;
	}

	/* The bevel lives on the frame and the scrolling happens inside it,
	   otherwise the content would scroll straight over the sunken edge. */
	.pad-client {
		flex: 1;
		min-height: 0;
		display: flex;
		padding: 2px;
		margin: 3px 0 0;
	}

	.pad-scroll {
		flex: 1;
		min-width: 0;
		overflow: auto;
		background: #fff;
	}

	.loading {
		padding: 14px;
		font-size: 12px;
		color: #404040;
	}

	.w95-statusbar .cap {
		color: #404040;
	}

	.w95-statusbar .emo {
		font-size: 13px;
		line-height: 1;
	}

	/* ------------------------------------------------------------- taskbar */

	.start {
		font-weight: 700;
		gap: 5px;
		padding: 3px 7px;
	}

	.task {
		flex: 0 1 190px;
		min-width: 0;
		justify-content: flex-start;
		gap: 5px;
		font-size: 11px;
		padding: 3px 7px;
		overflow: hidden;
	}

	.task span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	/* ---------------------------------------------------------- Windows CE */

	.ce {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
		padding: 3px 3px 0;
	}

	.ce-window {
		flex: 1;
		min-height: 0;
	}

	.ce-navrow {
		flex-shrink: 0;
		display: flex;
		align-items: stretch;
		gap: 3px;
		padding: 3px 0 0;
	}

	.ce-task {
		flex: 1;
		min-width: 0;
		gap: 5px;
		font-size: 11px;
		justify-content: flex-start;
		overflow: hidden;
	}

	.ce-task span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	/* --------------------------------------------------------------- about */

	.about {
		display: flex;
		align-items: flex-start;
		gap: 12px;
		min-width: 240px;
	}

	.about-text {
		flex: 1;
		min-width: 0;
		font-size: 12px;
		line-height: 1.6;
	}

	.about-title {
		font-weight: 700;
	}

	.ok {
		min-width: 74px;
	}
</style>
