<script lang="ts">
	import { lightboxImages } from '$lib/stores/lightbox';
	import { withDisplayImages, imageFallback } from '$lib/utils/imageDisplay';
	import { livePhotos } from '$lib/utils/livePlayer';
	/**
	 * Browse and restore earlier versions of one diary entry. Shared by every
	 * visual style. Saving archives the replaced version on the server, so
	 * this only reads history back and asks the server to restore one.
	 */
	import type { Diary } from '$lib/api/client';
	import {
		getDiaryByDateResult,
		getDiaryHistory,
		getDiaryRevision,
		restoreDiaryRevision,
		type DiaryRevisionSummary
	} from '$lib/api/diaries';
	import { clearCache, forceSyncNow, hasDirtyCache } from '$lib/stores/diaryCache';
	import { formatDisplayDate } from '$lib/utils/date';
	import { getIntlLocale, t } from '$lib/i18n';

	export let isOpen = false;
	export let date: string;
	export let onClose: () => void = () => {};
	/** Called with the entry as it is after a restore. */
	export let onRestored: (diary: Diary) => void = () => {};

	const CURRENT = 'current';

	interface Version {
		content: string;
		mood: string;
		weather: string;
	}

	let loading = false;
	let error = '';
	let unsyncedWarning = false;
	let limit = 0;
	let current: (Version & { updated: string }) | null = null;
	let revisions: DiaryRevisionSummary[] = [];
	let selectedId = CURRENT;
	let versions = new Map<string, Version>();
	let previewLoading = false;
	let confirming = false;
	let restoring = false;
	// On narrow screens the list and the preview take turns.
	let mobilePreview = false;
	let loadedFor = '';
	let loadToken = 0;

	$: if (isOpen && date && loadedFor !== date) {
		loadedFor = date;
		void load(date);
	}
	$: if (!isOpen) loadedFor = '';

	$: selectedSummary = revisions.find((r) => r.id === selectedId) ?? null;
	$: selectedVersion = selectedId === CURRENT ? current : versions.get(selectedId) ?? null;

	function parseServerTime(value: string): Date | null {
		if (!value) return null;
		const parsed = new Date(value.replace(' ', 'T'));
		return isNaN(parsed.getTime()) ? null : parsed;
	}

	function formatTime(value: string): string {
		const parsed = parseServerTime(value);
		if (!parsed) return '—';
		return parsed.toLocaleString(getIntlLocale(), {
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			year: parsed.getFullYear() === new Date().getFullYear() ? undefined : 'numeric'
		});
	}

	async function load(target: string) {
		const token = ++loadToken;
		loading = true;
		error = '';
		confirming = false;
		mobilePreview = false;
		selectedId = CURRENT;
		versions = new Map();
		revisions = [];
		current = null;

		// Push unsaved edits first so they become the current version (and the
		// version they replace lands in history) before anything is compared.
		await forceSyncNow();
		if (token !== loadToken) return;
		unsyncedWarning = hasDirtyCache(target);

		try {
			const [history, currentResult] = await Promise.all([
				getDiaryHistory(target),
				getDiaryByDateResult(target)
			]);
			if (token !== loadToken) return;
			limit = history.limit;
			revisions = history.revisions;
			if (currentResult.status === 'found') {
				current = {
					content: currentResult.diary.content || '',
					mood: currentResult.diary.mood || '',
					weather: currentResult.diary.weather || '',
					updated: currentResult.diary.updated || ''
				};
			} else if (currentResult.status === 'error') {
				error = $t('diaryHistory.loadFailed');
			}
			if (!current && revisions.length > 0) {
				void select(revisions[0].id, false);
			}
		} catch (e) {
			if (token !== loadToken) return;
			console.error('Failed to load diary history:', e);
			error = $t('diaryHistory.loadFailed');
		} finally {
			if (token === loadToken) loading = false;
		}
	}

	async function select(id: string, showPreview = true) {
		selectedId = id;
		confirming = false;
		if (showPreview) mobilePreview = true;
		if (id === CURRENT || versions.has(id)) return;
		previewLoading = true;
		try {
			const revision = await getDiaryRevision(id);
			versions = new Map(versions).set(id, {
				content: revision.content,
				mood: revision.mood,
				weather: revision.weather
			});
		} catch (e) {
			console.error('Failed to load revision:', e);
			error = $t('diaryHistory.loadFailed');
		} finally {
			previewLoading = false;
		}
	}

	async function restore() {
		if (selectedId === CURRENT || restoring) return;
		restoring = true;
		error = '';
		try {
			const diary = await restoreDiaryRevision(selectedId);
			// Any local draft for this day predates the restore; drop it so the
			// autosave cannot overwrite the restored version.
			clearCache(date);
			onRestored(diary);
			handleClose();
		} catch (e) {
			console.error('Failed to restore revision:', e);
			error = $t('diaryHistory.restoreFailed');
		} finally {
			restoring = false;
			confirming = false;
		}
	}

	function handleClose() {
		loadToken++;
		isOpen = false;
		onClose();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (!isOpen || e.key !== 'Escape') return;
		if (confirming) {
			confirming = false;
		} else {
			handleClose();
		}
	}
</script>

<svelte:window on:keydown={handleKeydown} />

{#if isOpen}
	<div
		class="fixed inset-0 bg-black/50 z-50 animate-fade-in-only"
		on:click={handleClose}
		on:keydown={(e) => e.key === 'Enter' && handleClose()}
		role="button"
		tabindex="0"
		aria-label={$t('diaryHistory.close')}
	></div>

	<div class="fixed inset-2 sm:inset-6 lg:inset-12 z-50 flex items-center justify-center pointer-events-none">
		<div
			class="bg-card rounded-xl shadow-2xl border border-border/50 w-full max-w-5xl h-full max-h-[46rem] overflow-hidden flex flex-col pointer-events-auto animate-fade-in"
			role="dialog"
			aria-modal="true"
			aria-labelledby="diary-history-title"
		>
			<!-- Header -->
			<div class="flex items-center justify-between gap-3 px-4 py-3 border-b border-border/50">
				<div class="flex items-center gap-2 min-w-0">
					{#if mobilePreview}
						<button
							class="md:hidden p-1.5 -ml-1 hover:bg-muted/50 rounded-lg transition-colors"
							on:click={() => (mobilePreview = false)}
							aria-label={$t('diaryHistory.backToList')}
						>
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
							</svg>
						</button>
					{/if}
					<div class="min-w-0">
						<h2 id="diary-history-title" class="text-base font-semibold text-foreground truncate">{$t('diaryHistory.title')}</h2>
						<div class="text-xs text-muted-foreground truncate">{formatDisplayDate(date)}</div>
					</div>
				</div>
				<button
					on:click={handleClose}
					class="p-2 hover:bg-muted/50 rounded-lg transition-colors"
					title={$t('diaryHistory.close')}
					aria-label={$t('diaryHistory.close')}
				>
					<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
					</svg>
				</button>
			</div>

			{#if error}
				<div class="mx-4 mt-3 p-2.5 bg-destructive/10 text-destructive rounded-lg text-sm">{error}</div>
			{/if}
			{#if unsyncedWarning}
				<div class="mx-4 mt-3 p-2.5 bg-amber-500/10 text-amber-600 dark:text-amber-400 rounded-lg text-sm">
					{$t('diaryHistory.unsyncedWarning')}
				</div>
			{/if}

			{#if loading}
				<div class="flex-1 flex flex-col items-center justify-center gap-3">
					<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
						<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
						<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
					</svg>
					<div class="text-muted-foreground text-sm">{$t('common.loading')}</div>
				</div>
			{:else if !current && revisions.length === 0}
				<div class="flex-1 flex flex-col items-center justify-center gap-2 px-6 text-center">
					<div class="text-sm font-medium text-foreground">{$t('diaryHistory.emptyTitle')}</div>
					<div class="text-sm text-muted-foreground max-w-sm">{$t('diaryHistory.emptyDesc')}</div>
				</div>
			{:else}
				<div class="flex-1 min-h-0 flex">
					<!-- Version list -->
					<aside class="w-full md:w-72 md:flex-shrink-0 md:border-r border-border/50 overflow-y-auto {mobilePreview ? 'hidden md:block' : ''}">
						<ul class="p-2 space-y-1">
							{#if current}
								<li>
									<button
										class="version-item {selectedId === CURRENT ? 'version-item-active' : ''}"
										on:click={() => select(CURRENT)}
									>
										<div class="flex items-center justify-between gap-2">
											<span class="text-sm font-medium text-foreground">{$t('diaryHistory.current')}</span>
											<span class="text-[11px] text-muted-foreground">{formatTime(current.updated)}</span>
										</div>
										<div class="text-xs text-muted-foreground mt-0.5">{$t('diaryHistory.currentDesc')}</div>
									</button>
								</li>
							{/if}
							{#each revisions as revision, index (revision.id)}
								<li>
									<button
										class="version-item {selectedId === revision.id ? 'version-item-active' : ''}"
										on:click={() => select(revision.id)}
									>
										<div class="flex items-center justify-between gap-2">
											<span class="text-sm font-medium text-foreground">{formatTime(revision.saved || revision.created)}</span>
											<span class="text-[11px] text-muted-foreground whitespace-nowrap">
												{revision.mood}{revision.weather}
												{$t('diaryHistory.words', { count: revision.words })}
											</span>
										</div>
										<div class="text-xs text-muted-foreground mt-0.5 line-clamp-2 break-words">
											{revision.preview || $t('diaryHistory.noText')}
										</div>
										{#if index === revisions.length - 1 && revisions.length >= limit}
											<div class="text-[10px] text-muted-foreground/80 mt-1">{$t('diaryHistory.oldestKept', { limit })}</div>
										{/if}
									</button>
								</li>
							{/each}
							{#if revisions.length === 0}
								<li class="px-3 py-4 text-xs text-muted-foreground">{$t('diaryHistory.noSnapshots')}</li>
							{/if}
						</ul>
					</aside>

					<!-- Preview -->
					<section class="flex-1 min-w-0 flex-col {mobilePreview ? 'flex' : 'hidden md:flex'}">
						<div class="flex-1 overflow-y-auto px-5 py-4">
							{#if previewLoading && !selectedVersion}
								<div class="text-sm text-muted-foreground">{$t('common.loading')}</div>
							{:else if selectedVersion}
								{#if selectedVersion.mood || selectedVersion.weather}
									<div class="flex gap-3 text-sm text-muted-foreground mb-3">
										{#if selectedVersion.mood}<span>{$t('diaryHistory.mood')} {selectedVersion.mood}</span>{/if}
										{#if selectedVersion.weather}<span>{$t('diaryHistory.weather')} {selectedVersion.weather}</span>{/if}
									</div>
								{/if}
								{#if selectedVersion.content}
									<div class="tiptap-editor-content history-preview" use:lightboxImages use:imageFallback use:livePhotos>{@html withDisplayImages(selectedVersion.content)}</div>
								{:else}
									<div class="text-sm text-muted-foreground">{$t('diaryHistory.noText')}</div>
								{/if}
							{/if}
						</div>

						<div class="px-4 py-3 border-t border-border/50 flex flex-wrap items-center justify-end gap-2">
							{#if selectedId === CURRENT}
								<span class="text-xs text-muted-foreground mr-auto">{$t('diaryHistory.pickVersion')}</span>
							{:else if confirming}
								<span class="text-xs text-muted-foreground mr-auto">{$t('diaryHistory.confirmRestore')}</span>
								<button
									class="px-3 py-1.5 text-sm bg-muted hover:bg-muted/80 rounded-lg transition-colors"
									on:click={() => (confirming = false)}
									disabled={restoring}
								>
									{$t('common.cancel')}
								</button>
								<button
									class="px-3 py-1.5 text-sm bg-primary text-primary-foreground hover:bg-primary/90 rounded-lg transition-colors disabled:opacity-50"
									on:click={restore}
									disabled={restoring}
								>
									{restoring ? $t('diaryHistory.restoring') : $t('diaryHistory.confirm')}
								</button>
							{:else}
								{#if selectedSummary}
									<span class="text-xs text-muted-foreground mr-auto">
										{$t('diaryHistory.archivedAt', { time: formatTime(selectedSummary.created) })}
									</span>
								{/if}
								<button
									class="px-3 py-1.5 text-sm bg-primary text-primary-foreground hover:bg-primary/90 rounded-lg transition-colors disabled:opacity-50"
									on:click={() => (confirming = true)}
									disabled={!selectedVersion || unsyncedWarning}
								>
									{$t('diaryHistory.restore')}
								</button>
							{/if}
						</div>
					</section>
				</div>
			{/if}
		</div>
	</div>
{/if}

<style>
	.version-item {
		width: 100%;
		text-align: left;
		padding: 0.5rem 0.75rem;
		border-radius: 0.6rem;
		border: 1px solid transparent;
		transition: background-color 0.15s ease, border-color 0.15s ease;
	}
	.version-item:hover {
		background: hsl(var(--muted) / 0.5);
	}
	.version-item-active {
		background: hsl(var(--primary) / 0.08);
		border-color: hsl(var(--primary) / 0.35);
	}
	.history-preview {
		padding: 0;
		min-height: 0;
	}
</style>
