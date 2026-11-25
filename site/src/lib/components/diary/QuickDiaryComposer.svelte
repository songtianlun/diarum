<script lang="ts">
	import { onMount, onDestroy, tick } from 'svelte';
	import type { Diary } from '$lib/api/client';
	import { appendDiaryNote } from '$lib/api/diaries';
	import { formatShortDate, getToday } from '$lib/utils/date';
	import { initDiaryCache, hasDirtyCache, forceSyncNow } from '$lib/stores/diaryCache';
	import { t } from '$lib/i18n';

	interface SentNote {
		id: number;
		content: string;
		timestamp: string;
		date: string;
	}

	let { onAppended }: { onAppended?: (diary: Diary) => void } = $props();

	const TIMESTAMP_PREF_KEY = 'diarum_quick_write_timestamp';

	let open = $state(false);
	let draft = $state('');
	let sending = $state(false);
	let errorMessage = $state('');
	let includeTimestamp = $state(true);
	let liveTime = $state(formatClock(new Date()));
	let todayDate = $state(getToday());
	let sentNotes = $state<SentNote[]>([]);
	let isComposing = $state(false);
	let isCoarsePointer = $state(false);
	let viewportHeight = $state<number | null>(null);
	let viewportOffsetTop = $state(0);

	let textareaEl = $state<HTMLTextAreaElement | undefined>();
	let feedEl = $state<HTMLDivElement | undefined>();

	let nextNoteId = 1;
	let clockTimer: ReturnType<typeof setInterval> | null = null;
	let previousBodyOverflow = '';
	let bodyLocked = false;

	function formatClock(now: Date): string {
		const hours = String(now.getHours()).padStart(2, '0');
		const minutes = String(now.getMinutes()).padStart(2, '0');
		const seconds = String(now.getSeconds()).padStart(2, '0');
		return `${hours}:${minutes}:${seconds}`;
	}

	function syncClock() {
		const now = new Date();
		liveTime = formatClock(now);
		todayDate = getToday();
	}

	function startClock() {
		syncClock();
		stopClock();
		clockTimer = setInterval(syncClock, 1000);
	}

	function stopClock() {
		if (clockTimer) {
			clearInterval(clockTimer);
			clockTimer = null;
		}
	}

	function lockBodyScroll() {
		if (typeof document === 'undefined' || bodyLocked) return;
		previousBodyOverflow = document.body.style.overflow;
		document.body.style.overflow = 'hidden';
		bodyLocked = true;
	}

	function unlockBodyScroll() {
		if (typeof document === 'undefined' || !bodyLocked) return;
		document.body.style.overflow = previousBodyOverflow;
		bodyLocked = false;
	}

	function updateViewportMetrics() {
		if (typeof window === 'undefined' || !window.visualViewport) return;
		viewportHeight = window.visualViewport.height;
		viewportOffsetTop = window.visualViewport.offsetTop;
	}

	function resizeTextarea() {
		if (!textareaEl) return;
		textareaEl.style.height = 'auto';
		const nextHeight = Math.min(Math.max(textareaEl.scrollHeight, 44), 200);
		textareaEl.style.height = `${nextHeight}px`;
	}

	async function openComposer() {
		errorMessage = '';
		syncClock();
		open = true;
		lockBodyScroll();
		startClock();
		updateViewportMetrics();
		await tick();
		resizeTextarea();
		textareaEl?.focus();
	}

	function closeComposer() {
		open = false;
		errorMessage = '';
		stopClock();
		unlockBodyScroll();
	}

	function toggleTimestamp() {
		includeTimestamp = !includeTimestamp;
		try {
			localStorage.setItem(TIMESTAMP_PREF_KEY, includeTimestamp ? '1' : '0');
		} catch {
			// Ignore storage errors in restricted contexts.
		}
	}

	async function handleSubmit() {
		const text = draft.trim();
		if (!text || sending) return;

		sending = true;
		errorMessage = '';

		const now = new Date();
		const targetDate = getToday();
		const timestamp = includeTimestamp ? formatClock(now) : '';
		todayDate = targetDate;

		try {
			// Flush any unsynced local draft for today before appending on the server
			// so quick notes never race with or bypass pending local edits.
			if (hasDirtyCache(targetDate)) {
				const synced = await forceSyncNow();
				if (!synced) {
					errorMessage = $t('quickWrite.unsyncedError');
					return;
				}
			}

			const diary = await appendDiaryNote({
				date: targetDate,
				content: text,
				timestamp
			});

			sentNotes = [
				...sentNotes,
				{
					id: nextNoteId++,
					content: text,
					timestamp,
					date: targetDate
				}
			];
			draft = '';

			await tick();
			resizeTextarea();
			if (feedEl) {
				feedEl.scrollTop = feedEl.scrollHeight;
			}
			textareaEl?.focus();
			onAppended?.(diary);
		} catch (err) {
			const msg = err instanceof Error && err.message ? err.message : '';
			errorMessage = msg ? `${$t('quickWrite.sendFailed')}: ${msg}` : $t('quickWrite.sendFailed');
		} finally {
			sending = false;
		}
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.isComposing || isComposing || event.keyCode === 229) {
			return;
		}
		if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
			event.preventDefault();
			void handleSubmit();
			return;
		}
		if (event.key === 'Enter' && !event.shiftKey && !isCoarsePointer) {
			event.preventDefault();
			void handleSubmit();
		}
	}

	function handleWindowKeydown(event: KeyboardEvent) {
		if (!open) return;
		if (event.key === 'Escape' && !event.isComposing && !isComposing) {
			event.preventDefault();
			closeComposer();
		}
	}

	onMount(() => {
		initDiaryCache();

		try {
			const stored = localStorage.getItem(TIMESTAMP_PREF_KEY);
			if (stored === '0') includeTimestamp = false;
			else if (stored === '1') includeTimestamp = true;
		} catch {
			// Ignore storage read errors.
		}

		if (typeof window !== 'undefined') {
			isCoarsePointer = window.matchMedia?.('(pointer: coarse)').matches ?? false;
			window.addEventListener('keydown', handleWindowKeydown);
			window.visualViewport?.addEventListener('resize', updateViewportMetrics);
			window.visualViewport?.addEventListener('scroll', updateViewportMetrics);
		}

		return () => {
			if (typeof window !== 'undefined') {
				window.removeEventListener('keydown', handleWindowKeydown);
				window.visualViewport?.removeEventListener('resize', updateViewportMetrics);
				window.visualViewport?.removeEventListener('scroll', updateViewportMetrics);
			}
		};
	});

	onDestroy(() => {
		stopClock();
		unlockBodyScroll();
	});

	let lastSentDate = $derived(sentNotes.length > 0 ? sentNotes[sentNotes.length - 1].date : todayDate);
	let trimmedLength = $derived(draft.trim().length);
	let overlayStyle = $derived(
		viewportHeight !== null
			? `height: ${viewportHeight}px; top: ${viewportOffsetTop}px;`
			: ''
	);
</script>

<!-- Floating Quick-Write Button -->
{#if !open}
	<button
		type="button"
		onclick={openComposer}
		title={$t('quickWrite.fabTitle')}
		aria-label={$t('quickWrite.fabTitle')}
		aria-expanded={open}
		class="fixed right-5 bottom-[max(1.25rem,env(safe-area-inset-bottom))] sm:right-7 sm:bottom-7 z-40
			inline-flex items-center gap-2 h-12 px-4 sm:px-5 rounded-full
			bg-primary text-primary-foreground font-medium text-sm
			shadow-lg shadow-primary/25 hover:shadow-xl hover:shadow-primary/30
			hover:-translate-y-0.5 active:translate-y-0 active:scale-[0.98]
			ring-1 ring-primary-foreground/15 transition-all duration-200"
	>
		<svg class="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
			<path
				stroke-linecap="round"
				stroke-linejoin="round"
				stroke-width="2"
				d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"
			/>
		</svg>
		<span>{$t('quickWrite.fabLabel')}</span>
	</button>
{/if}

<!-- Full-Screen Quick-Write Overlay -->
{#if open}
	<div
		class="fixed inset-x-0 top-0 h-dvh z-50 flex flex-col justify-end items-center"
		style={overlayStyle}
		role="dialog"
		aria-modal="true"
		aria-label={$t('quickWrite.fabLabel')}
	>
		<!-- Dimmed backdrop -->
		<button
			type="button"
			class="absolute inset-0 bg-black/35 dark:bg-black/55 backdrop-blur-[3px] animate-fade-in-only cursor-default"
			onclick={closeComposer}
			aria-label={$t('quickWrite.close')}
		></button>

		<!-- Bottom Chat-Bubble Container -->
		<div class="relative z-10 w-full max-w-2xl px-3 sm:px-5 pb-[max(1rem,env(safe-area-inset-bottom))] sm:pb-8 flex flex-col gap-3 max-h-full pointer-events-none">
			<!-- Sent Notes Feed (Chat Bubbles) -->
			{#if sentNotes.length > 0}
				<div
					bind:this={feedEl}
					class="overflow-y-auto overscroll-contain space-y-2 px-1 max-h-[42vh] pointer-events-auto"
				>
					{#each sentNotes as note (note.id)}
						<div class="flex justify-end quick-bubble-in">
							<div class="max-w-[88%] sm:max-w-[80%] rounded-2xl rounded-br-md bg-card/95 border border-border/70 shadow-md px-4 py-2.5 text-foreground">
								<p class="text-sm leading-relaxed whitespace-pre-wrap break-words">{note.content}</p>
								<div class="mt-1.5 flex items-center justify-end gap-2 text-[11px] text-muted-foreground">
									{#if note.timestamp}
										<span class="font-mono tabular-nums">{note.timestamp}</span>
										<span aria-hidden="true">·</span>
									{/if}
									<span class="inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
										<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7" />
										</svg>
										{$t('quickWrite.appended')}
									</span>
								</div>
							</div>
						</div>
					{/each}
				</div>
			{/if}

			<!-- Input Bubble Card -->
			<div class="pointer-events-auto quick-bubble-in bg-card border border-border/80 rounded-[24px] shadow-2xl p-3 sm:p-3.5">
				<!-- Top Metadata Bar -->
				<div class="flex items-center justify-between gap-2 px-1.5 pb-2 mb-1 border-b border-border/40">
					<a
						href="/diary/{todayDate}"
						onclick={closeComposer}
						class="inline-flex items-center gap-1.5 text-xs font-medium text-foreground/90 hover:text-primary transition-colors min-w-0"
						title={$t('quickWrite.openToday')}
					>
						<span class="w-2 h-2 rounded-full bg-primary flex-shrink-0"></span>
						<span class="truncate">{$t('quickWrite.todayLabel')} · {formatShortDate(todayDate)}</span>
						<svg class="w-3 h-3 text-muted-foreground flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
						</svg>
					</a>

					<div class="flex items-center gap-1.5 flex-shrink-0">
						<button
							type="button"
							onclick={toggleTimestamp}
							title={includeTimestamp ? $t('quickWrite.timestampOn') : $t('quickWrite.timestampOff')}
							aria-pressed={includeTimestamp}
							class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-mono tabular-nums border transition-all
								{includeTimestamp
									? 'bg-primary/10 text-primary border-primary/25 hover:bg-primary/15'
									: 'bg-muted/60 text-muted-foreground border-border/60 line-through opacity-75 hover:opacity-100'}"
						>
							<svg class="w-3 h-3 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
							</svg>
							<span>{liveTime}</span>
						</button>

						<button
							type="button"
							onclick={closeComposer}
							class="p-1 rounded-full text-muted-foreground hover:text-foreground hover:bg-muted/70 transition-colors"
							title={$t('quickWrite.close')}
							aria-label={$t('quickWrite.close')}
						>
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
							</svg>
						</button>
					</div>
				</div>

				<!-- Error Banner -->
				{#if errorMessage}
					<div
						role="alert"
						class="mx-1 mb-2 px-3 py-1.5 rounded-xl bg-red-500/10 border border-red-500/25 text-xs text-red-600 dark:text-red-400"
					>
						{errorMessage}
					</div>
				{/if}

				<!-- Chat-like Input Row -->
				<form
					onsubmit={(e) => {
						e.preventDefault();
						void handleSubmit();
					}}
					class="flex items-end gap-2"
				>
					<textarea
						bind:this={textareaEl}
						bind:value={draft}
						oninput={resizeTextarea}
						onkeydown={handleKeydown}
						oncompositionstart={() => (isComposing = true)}
						oncompositionend={() => (isComposing = false)}
						placeholder={$t('quickWrite.placeholder')}
						disabled={sending}
						rows="1"
						class="flex-1 resize-none bg-transparent border-0 px-2 py-2.5 text-sm sm:text-[15px] leading-relaxed
							text-foreground placeholder:text-muted-foreground/70
							focus:outline-none disabled:opacity-60 max-h-[200px] overflow-y-auto"
					></textarea>

					<button
						type="submit"
						disabled={sending || trimmedLength === 0}
						title={sending ? $t('quickWrite.sending') : $t('quickWrite.send')}
						aria-label={sending ? $t('quickWrite.sending') : $t('quickWrite.send')}
						class="w-10 h-10 rounded-full flex-shrink-0 flex items-center justify-center
							bg-primary text-primary-foreground shadow-sm
							hover:opacity-90 active:scale-95 transition-all duration-150
							disabled:opacity-35 disabled:cursor-not-allowed disabled:active:scale-100"
					>
						{#if sending}
							<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24" aria-hidden="true">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3.5"></circle>
								<path class="opacity-85" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
							</svg>
						{:else}
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M5 12h14M13 5l7 7-7 7" />
							</svg>
						{/if}
					</button>
				</form>

				<!-- Bottom Hint Row -->
				<div class="flex items-center justify-between gap-2 px-2 pt-1.5 text-[11px] text-muted-foreground">
					{#if sentNotes.length > 0}
						<a
							href="/diary/{lastSentDate}"
							onclick={closeComposer}
							class="text-primary hover:underline font-medium"
						>
							{$t('quickWrite.openToday')} →
						</a>
					{:else}
						<span class="truncate">
							{isCoarsePointer ? $t('quickWrite.mobileHint') : $t('quickWrite.shortcutHint')}
						</span>
					{/if}

					{#if trimmedLength > 0}
						<span class="tabular-nums flex-shrink-0">{$t('quickWrite.chars', { count: trimmedLength })}</span>
					{/if}
				</div>
			</div>
		</div>
	</div>
{/if}

<style>
	@keyframes quick-bubble-in {
		from {
			opacity: 0;
			transform: translateY(14px) scale(0.985);
		}
		to {
			opacity: 1;
			transform: translateY(0) scale(1);
		}
	}

	.quick-bubble-in {
		animation: quick-bubble-in 0.2s cubic-bezier(0.16, 1, 0.3, 1) forwards;
	}

	@media (prefers-reduced-motion: reduce) {
		.quick-bubble-in {
			animation: none;
		}
	}
</style>
