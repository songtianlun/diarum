<script lang="ts">
	/**
	 * Shared top nav for the diary entry route (/diary/[date]), used by both
	 * visual styles: the classic editor view and the immersive book view.
	 * Both have an identical layout — brand, prev/next day, a date icon +
	 * date text, and a right-hand actions cluster (assistant, share, table
	 * of contents, sync status).
	 *
	 * The only behavioral difference between the two views is what the date
	 * text does when clicked:
	 *   - classic: not clickable (pass nothing / leave onDateTextClick unset)
	 *   - immersive/book: opens the book's own date-picker/catalog overlay
	 *
	 * The date *icon* always navigates to /diary.
	 */
	import { goto } from '$app/navigation';
	import { formatDisplayDate, formatShortDate, getDayOfWeek, isToday } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	/** Date currently shown in the header (drives display + "Today" state). */
	export let date: string;
	/** Disables prev/next/calendar-icon navigation while a load/animation is in flight. */
	export let busy = false;

	export let onPrevDay: () => void;
	export let onNextDay: () => void;

	/** If provided, the date text becomes a button that calls this on click. */
	export let onDateTextClick: (() => void) | null = null;
	/** Highlights the date text button when the thing it opens is currently open. */
	export let dateTextActive = false;

	export let onShareMouseDown: () => void;
	export let onShareClick: () => void;

	export let tocActive = false;
	export let onTocClick: () => void;

	export let isOnline: boolean;
	export let isSyncing: boolean;
	export let isDirty: boolean;
	export let onSyncClick: () => void;

	$: atToday = isToday(date);

	function goToCalendar() {
		goto('/diary');
	}
</script>

<header class="glass border-b border-border/50 sticky top-0 z-30">
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
					class="nav-btn"
					title={$t('entryNav.previousDay')}
					disabled={busy}
					on:click={onPrevDay}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
					</svg>
				</button>

				<button
					class="nav-btn"
					title={$t('entryNav.calendar')}
					disabled={busy}
					on:click={goToCalendar}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
							d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
					</svg>
				</button>

				{#if onDateTextClick}
					<button
						class="date-text date-text-btn {dateTextActive ? 'bg-muted/60' : ''}"
						title={$t('entryNav.contents')}
						on:click={onDateTextClick}
					>
						<span class="hidden sm:inline">{formatDisplayDate(date)}</span>
						<span class="sm:hidden">{formatShortDate(date)}</span>
						<span class="hidden sm:inline text-xs text-muted-foreground font-normal ml-1">{getDayOfWeek(date)}</span>
						{#if atToday}
							<span class="text-xs px-1.5 py-0.5 bg-primary/10 text-primary rounded-full ml-1">{$t('common.today')}</span>
						{/if}
					</button>
				{:else}
					<div class="date-text text-foreground">
						<span class="hidden sm:inline">{formatDisplayDate(date)}</span>
						<span class="sm:hidden">{formatShortDate(date)}</span>
						<span class="hidden sm:inline text-xs text-muted-foreground font-normal ml-1">{getDayOfWeek(date)}</span>
						{#if atToday}
							<span class="text-xs px-1.5 py-0.5 bg-primary/10 text-primary rounded-full ml-1">{$t('common.today')}</span>
						{/if}
					</div>
				{/if}

				<button
					class="nav-btn"
					title={atToday ? $t('entryNav.cannotGoBeyondToday') : $t('entryNav.nextDay')}
					disabled={busy || atToday}
					on:click={onNextDay}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
					</svg>
				</button>
			</div>

			<!-- Right: Actions -->
			<div class="flex items-center gap-2">
				<a href="/assistant" class="hidden sm:block p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200" title={$t('entryNav.assistant')}>
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
					on:mousedown={onShareMouseDown}
					on:click={onShareClick}
					class="hidden sm:block p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
					title={$t('entryNav.shareAsImage')}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" />
					</svg>
				</button>

				<button
					class="nav-btn {tocActive ? 'bg-muted/60' : ''}"
					on:click={onTocClick}
					title={$t('entryNav.tableOfContents')}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h7" />
					</svg>
				</button>

				<button
					class="nav-btn"
					on:click={onSyncClick}
					title={!isOnline
						? $t('entryNav.offline')
						: isSyncing
							? $t('entryNav.syncing')
							: isDirty
								? $t('entryNav.clickToSave')
								: $t('entryNav.allSaved')}
				>
					{#if !isOnline}
						<svg class="w-4 h-4 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M18.364 5.636a9 9 0 010 12.728m0 0l-2.829-2.829m2.829 2.829L21 21M15.536 8.464a5 5 0 010 7.072m0 0l-2.829-2.829m-4.243 2.829a4.978 4.978 0 01-1.414-2.83m-1.414 5.658a9 9 0 01-2.167-9.238m7.824 2.167a1 1 0 111.414 1.414m-1.414-1.414L3 3"></path>
						</svg>
					{:else if isSyncing}
						<svg class="w-4 h-4 text-yellow-500 animate-spin" fill="none" viewBox="0 0 24 24">
							<circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" stroke-dasharray="40 20" stroke-linecap="round"></circle>
						</svg>
					{:else if isDirty}
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

<style>
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
		opacity: 0.5;
	}

	.date-text {
		font-size: 0.875rem;
		white-space: nowrap;
	}
	.date-text-btn {
		padding: 0.25rem 0.375rem;
		border-radius: 0.5rem;
		color: hsl(var(--foreground));
		transition: background-color 0.2s ease;
	}
	.date-text-btn:hover {
		background: hsl(var(--muted) / 0.5);
	}
</style>
