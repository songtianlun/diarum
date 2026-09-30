<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { isAuthenticated } from '$lib/api/client';
	import { emptyTrash, fetchTrashPage, purgeMedia, restoreMedia, type MediaBatchResult, type TrashPage } from '$lib/api/media';
	import { t, getIntlLocale } from '$lib/i18n';
	import Footer from '$lib/components/ui/Footer.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import Lightbox from '$lib/components/ui/Lightbox.svelte';
	import MediaThumb from '$lib/components/media/MediaThumb.svelte';
	import {
		createGalleryFeed,
		groupByDay,
		formatDayLabel,
		inView,
		type GalleryFeed,
		type GalleryItem
	} from '$lib/components/media/galleryFeed';

	const DAY_MS = 24 * 60 * 60 * 1000;

	let retentionDays = 30;
	const feed: GalleryFeed = createGalleryFeed<TrashPage>({
		pageSize: 40,
		fetchPage: fetchTrashPage,
		onPage: (page) => (retentionDays = page.retentionDays)
	});

	// Selection
	let selecting = false;
	let selected = new Set<string>();

	// Lightbox
	let viewerOpen = false;
	let viewerIndex = 0;

	// Actions
	let busy = false;
	let confirm: { kind: 'purge' | 'empty'; ids: string[] } | null = null;
	let toast: { text: string; ok: boolean } | null = null;
	let toastTimer: ReturnType<typeof setTimeout> | undefined;

	function showToast(text: string, ok = true) {
		toast = { text, ok };
		clearTimeout(toastTimer);
		toastTimer = setTimeout(() => (toast = null), 3500);
	}

	function parseTime(value: string | undefined): Date | null {
		if (!value) return null;
		const date = new Date(value.replace(' ', 'T'));
		return Number.isNaN(date.getTime()) ? null : date;
	}

	function formatTime(value: string | undefined): string {
		const date = parseTime(value);
		if (!date) return value ?? '';
		return date.toLocaleString(getIntlLocale(), { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
	}

	/** Whole days until the image is removed for good; null if never. */
	function daysLeft(item: GalleryItem): number | null {
		if (retentionDays <= 0) return null;
		const deleted = parseTime(item.media.deleted);
		if (!deleted) return null;
		const due = deleted.getTime() + retentionDays * DAY_MS;
		return Math.max(0, Math.ceil((due - Date.now()) / DAY_MS));
	}

	function reasonLabel(reason: string | undefined): string {
		if (!reason) return '';
		const key = `mediaLib.trash.reason.${reason}`;
		const label = $t(key);
		return label === key ? reason : label;
	}

	function startSelecting() {
		selecting = true;
		selected = new Set();
	}

	function stopSelecting() {
		selecting = false;
		selected = new Set();
	}

	function toggle(key: string) {
		const next = new Set(selected);
		if (next.has(key)) next.delete(key);
		else next.add(key);
		selected = next;
	}

	function toggleGroup(items: GalleryItem[]) {
		const next = new Set(selected);
		const all = items.every((item) => next.has(item.key));
		for (const item of items) {
			if (all) next.delete(item.key);
			else next.add(item.key);
		}
		selected = next;
	}

	function toggleAll() {
		selected = allSelected ? new Set() : new Set(state.items.map((item) => item.key));
	}

	function onTile(item: GalleryItem) {
		if (selecting) {
			toggle(item.key);
			return;
		}
		viewerIndex = state.items.findIndex((candidate) => candidate.key === item.key);
		viewerOpen = viewerIndex >= 0;
	}

	function closeViewer() {
		viewerOpen = false;
	}

	/** Drops finished images from the view and reports the outcome. */
	function settle(result: MediaBatchResult, doneKey: string) {
		feed.removeMany(result.done);
		const next = new Set(selected);
		for (const id of result.done) next.delete(id);
		selected = next;
		const failed = Object.keys(result.failed ?? {}).length;
		if (failed > 0) {
			showToast(`${result.done.length ? $t(doneKey, { count: result.done.length }) + ' · ' : ''}${$t('mediaLib.trash.partial', { failed })}`, false);
		} else {
			showToast($t(doneKey, { count: result.done.length }));
		}
		if ($feed.items.length === 0) {
			stopSelecting();
			closeViewer();
			// Items further down may still be waiting on the server.
			if ($feed.hasMore) feed.reload();
		} else if (viewerOpen) {
			viewerIndex = Math.min(viewerIndex, $feed.items.length - 1);
		}
	}

	async function restore(ids: string[]) {
		if (!ids.length || busy) return;
		busy = true;
		try {
			settle(await restoreMedia(ids), 'mediaLib.trash.restored');
		} catch (error) {
			showToast(error instanceof Error ? error.message : $t('mediaLib.trash.actionFailed'), false);
		} finally {
			busy = false;
		}
	}

	async function runConfirmed() {
		if (!confirm || busy) return;
		const { kind, ids } = confirm;
		busy = true;
		try {
			const result = kind === 'empty' ? await emptyTrash() : await purgeMedia(ids);
			confirm = null;
			settle(result, 'mediaLib.trash.purged');
			if (kind === 'empty' && Object.keys(result.failed ?? {}).length === 0) {
				await feed.reload();
			}
		} catch (error) {
			confirm = null;
			showToast(error instanceof Error ? error.message : $t('mediaLib.trash.actionFailed'), false);
		} finally {
			busy = false;
		}
	}

	function loadMoreIfIdle() {
		if (!$feed.loading && !$feed.error && $feed.hasMore) feed.loadMore();
	}

	function onKeydown(event: KeyboardEvent) {
		if (event.key !== 'Escape' || viewerOpen) return;
		if (confirm && !busy) confirm = null;
		else if (selecting) stopSelecting();
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		feed.loadMore();
	});

	onDestroy(() => {
		feed.destroy();
		clearTimeout(toastTimer);
	});

	$: state = $feed;
	$: groups = groupByDay(state.items);
	$: total = state.total ?? state.items.length;
	$: allSelected = state.items.length > 0 && state.items.every((item) => selected.has(item.key));
	$: selectedIds = [...selected];
	$: viewerItem = viewerOpen ? state.items[viewerIndex] : undefined;
	$: if (viewerOpen && viewerIndex >= state.items.length - 3) loadMoreIfIdle();
</script>

<svelte:head>
	<title>{$t('mediaLib.trash.title')} - Diarum</title>
</svelte:head>

<svelte:window on:keydown={onKeydown} />

<div class="min-h-screen bg-background">
	<PageHeader title={$t('mediaLib.trash.title')}>
		<span slot="subtitle" class="ml-1 text-sm text-muted-foreground tabular-nums">{state.initial ? '' : `(${total})`}</span>
		<a
			slot="actions"
			href="/media"
			class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 text-muted-foreground hover:text-foreground"
			title={$t('mediaLib.trash.back')}
			aria-label={$t('mediaLib.trash.back')}
		>
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
			</svg>
		</a>
	</PageHeader>

	<main class="max-w-5xl mx-auto px-4 py-4 {selecting ? 'pb-28' : ''}">
		<!-- Toolbar -->
		<div class="flex flex-wrap items-center gap-x-3 gap-y-2 mb-4">
			<p class="flex-1 min-w-[12rem] text-xs text-muted-foreground flex items-center gap-1.5">
				<svg class="w-3.5 h-3.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
				</svg>
				{retentionDays > 0 ? $t('mediaLib.trash.retention', { days: retentionDays }) : $t('mediaLib.trash.retentionNever')}
			</p>
			{#if state.items.length > 0}
				<div class="flex items-center gap-2">
					{#if selecting}
						<button class="tb-btn" on:click={toggleAll}>{allSelected ? $t('mediaLib.trash.deselectAll') : $t('mediaLib.trash.selectAll')}</button>
						<button class="tb-btn" on:click={stopSelecting}>{$t('mediaLib.trash.cancel')}</button>
					{:else}
						<button class="tb-btn" on:click={startSelecting}>
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
							{$t('mediaLib.trash.select')}
						</button>
						<button class="tb-btn danger" disabled={busy} on:click={() => (confirm = { kind: 'empty', ids: [] })}>
							{$t('mediaLib.trash.emptyTrash')}
						</button>
					{/if}
				</div>
			{/if}
		</div>

		{#if state.initial && state.loading}
			<div class="space-y-8" aria-busy="true">
				{#each [6, 4] as count}
					<div>
						<div class="h-4 w-40 mb-4 rounded bg-muted animate-pulse"></div>
						<div class="grid grid-cols-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 gap-2 sm:gap-3">
							{#each Array(count) as _}
								<div class="aspect-square rounded-lg bg-muted/60 animate-pulse"></div>
							{/each}
						</div>
					</div>
				{/each}
			</div>
		{:else if state.items.length === 0 && state.error}
			<div class="flex flex-col items-center justify-center py-20 gap-4 text-center">
				<p class="text-lg font-medium text-foreground">{$t('mediaLib.trash.loadFailed')}</p>
				<p class="text-sm text-muted-foreground max-w-md">{state.error}</p>
				<button class="tb-btn" on:click={() => feed.reload()}>{$t('mediaLib.trash.retry')}</button>
			</div>
		{:else if state.items.length === 0}
			<div class="flex flex-col items-center justify-center py-20 gap-4">
				<svg class="w-16 h-16 text-muted-foreground/30" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
				</svg>
				<div class="text-muted-foreground text-center">
					<p class="text-lg font-medium">{$t('mediaLib.trash.empty')}</p>
					<p class="text-sm mt-1">{$t('mediaLib.trash.emptyHint')}</p>
				</div>
			</div>
		{:else}
			<div class="space-y-6">
				{#each groups as group (group.date)}
					{@const groupSelected = group.items.every((item) => selected.has(item.key))}
					<section>
						<div class="sticky top-11 z-10 -mx-4 px-4 py-2 mb-2 flex items-center gap-3 bg-background/85 backdrop-blur-sm">
							<h2 class="text-sm font-medium text-foreground">{formatDayLabel(group.date)}</h2>
							<div class="flex-1 h-px bg-border/50"></div>
							{#if selecting}
								<button class="text-xs text-primary hover:underline" on:click={() => toggleGroup(group.items)}>
									{groupSelected ? $t('mediaLib.trash.deselectDay') : $t('mediaLib.trash.selectDay')}
								</button>
							{:else}
								<span class="text-xs text-muted-foreground tabular-nums">{group.items.length}</span>
							{/if}
						</div>

						<div class="grid grid-cols-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 gap-2 sm:gap-3">
							{#each group.items as item (item.key)}
								{@const left = daysLeft(item)}
								{@const isSelected = selected.has(item.key)}
								<button
									class="tile group relative aspect-square rounded-lg overflow-hidden border transition-all duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary {isSelected ? 'border-primary ring-2 ring-primary' : 'border-border/50 hover:border-primary/50'}"
									on:click={() => onTile(item)}
									title={item.title}
									aria-pressed={selecting ? isSelected : undefined}
								>
									<div class="w-full h-full transition-transform duration-150 {isSelected ? 'scale-[0.92] rounded-md overflow-hidden' : ''}">
										<MediaThumb src={item.thumb} fallback={item.src} alt={item.title} />
									</div>
									{#if selecting}
										<span class="absolute top-1.5 left-1.5 w-5 h-5 rounded-full border-2 flex items-center justify-center {isSelected ? 'bg-primary border-primary text-primary-foreground' : 'bg-black/25 border-white/90'}">
											{#if isSelected}
												<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" /></svg>
											{/if}
										</span>
									{/if}
									{#if left !== null}
										<span class="absolute bottom-1.5 right-1.5 px-1.5 py-0.5 rounded text-[10px] font-medium tabular-nums text-white {left <= 3 ? 'bg-rose-600/85' : 'bg-black/55'}">
											{left === 0 ? $t('mediaLib.trash.dueToday') : $t('mediaLib.trash.daysLeft', { days: left })}
										</span>
									{/if}
									{#if item.media.deleteTrigger === 'auto'}
										<span class="absolute top-1.5 right-1.5 px-1.5 py-0.5 rounded text-[10px] font-medium text-white bg-sky-600/80">{$t('mediaLib.trash.auto')}</span>
									{/if}
								</button>
							{/each}
						</div>
					</section>
				{/each}
			</div>

			<div class="flex justify-center py-8" use:inView={{ callback: loadMoreIfIdle, key: `${state.items.length}:${state.loading}` }}>
				{#if state.loading}
					<div class="flex items-center gap-2 text-sm text-muted-foreground">
						<span class="w-4 h-4 rounded-full border-2 border-muted-foreground/30 border-t-primary animate-spin"></span>
						{$t('mediaLib.trash.loadingMore')}
					</div>
				{:else if state.error}
					<div class="flex flex-col items-center gap-2 text-sm">
						<span class="text-rose-600">{state.error}</span>
						<button class="tb-btn" on:click={() => feed.loadMore()}>{$t('mediaLib.trash.retry')}</button>
					</div>
				{:else if !state.hasMore}
					<span class="text-xs text-muted-foreground/70">{$t('mediaLib.trash.end')}</span>
				{/if}
			</div>
		{/if}
	</main>

	<Footer maxWidth="4xl" tagline={$t('mediaLib.trash.title')} />
</div>

<!-- Selection action bar -->
{#if selecting}
	<div class="fixed inset-x-0 bottom-0 z-30 px-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-3 pointer-events-none animate-fade-in">
		<div class="pointer-events-auto max-w-xl mx-auto flex items-center gap-2 rounded-2xl border border-border/60 bg-card/95 backdrop-blur shadow-lg px-3 py-2.5">
			<span class="text-sm font-medium text-foreground tabular-nums flex-1 min-w-0 truncate">{$t('mediaLib.trash.selected', { count: selected.size })}</span>
			<button class="bar-btn" disabled={busy || selected.size === 0} on:click={() => restore(selectedIds)}>
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 10h10a5 5 0 015 5v2M3 10l5 5m-5-5l5-5" /></svg>
				<span>{$t('mediaLib.trash.restore')}</span>
			</button>
			<button class="bar-btn danger" disabled={busy || selected.size === 0} on:click={() => (confirm = { kind: 'purge', ids: selectedIds })}>
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
				<span>{$t('mediaLib.trash.deleteForever')}</span>
			</button>
		</div>
	</div>
{/if}

<!-- Confirmation -->
{#if confirm}
	<div class="fixed inset-0 z-[70] flex items-end sm:items-center justify-center bg-black/40 backdrop-blur-[2px] p-3 animate-fade-in" role="presentation" on:click|self={() => !busy && (confirm = null)}>
		<div class="w-full max-w-sm rounded-2xl bg-card border border-border/60 shadow-xl p-5" role="alertdialog" aria-modal="true" aria-labelledby="trash-confirm-text">
			<div class="w-10 h-10 rounded-full bg-rose-500/10 text-rose-600 dark:text-rose-400 flex items-center justify-center mb-3">
				<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" /></svg>
			</div>
			<p id="trash-confirm-text" class="text-sm text-foreground leading-relaxed">
				{confirm.kind === 'empty' ? $t('mediaLib.trash.confirmEmpty', { count: total }) : $t('mediaLib.trash.confirmPurge', { count: confirm.ids.length })}
			</p>
			<div class="mt-5 flex justify-end gap-2">
				<button class="tb-btn" disabled={busy} on:click={() => (confirm = null)}>{$t('mediaLib.trash.cancel')}</button>
				<button class="px-4 py-2 text-sm rounded-lg bg-rose-600 text-white hover:bg-rose-700 disabled:opacity-60 transition-colors" disabled={busy} on:click={runConfirmed}>
					{busy ? $t('mediaLib.trash.working') : $t('mediaLib.trash.confirm')}
				</button>
			</div>
		</div>
	</div>
{/if}

{#if toast}
	<div class="fixed inset-x-0 z-[80] flex justify-center px-4 pointer-events-none {selecting ? 'bottom-24' : 'bottom-6'}">
		<div class="max-w-full px-4 py-2 rounded-full text-sm shadow-lg animate-fade-in {toast.ok ? 'bg-foreground text-background' : 'bg-rose-600 text-white'}" role="status">
			{toast.text}
		</div>
	</div>
{/if}

{#if viewerOpen && state.items.length > 0}
	<Lightbox
		items={state.items.map((item) => ({ src: item.src, thumb: item.thumb, title: item.title }))}
		bind:index={viewerIndex}
		onClose={closeViewer}
		hasInfo
	>
		<div slot="info" class="info-panel">
			{#if viewerItem}
				{@const left = daysLeft(viewerItem)}
				<div class="flex flex-wrap items-start justify-between gap-3">
					<div class="min-w-0 space-y-0.5">
						<div class="font-medium truncate">{viewerItem.title}</div>
						<div class="text-xs text-white/70">
							{$t('mediaLib.trash.imageDate')}: {viewerItem.media.date ? formatDayLabel(viewerItem.date) : $t('mediaLib.trash.undated')}
						</div>
						<div class="text-xs text-white/60">
							{$t('mediaLib.trash.deletedAt', { time: formatTime(viewerItem.media.deleted) })}
							{#if viewerItem.media.deletedBy}{$t('mediaLib.trash.deletedBy', { who: viewerItem.media.deletedBy === 'system' ? 'Diarum' : viewerItem.media.deletedBy })}{/if}
							{#if viewerItem.media.deleteReason} · {reasonLabel(viewerItem.media.deleteReason)}{/if}
							{#if left !== null} · {left === 0 ? $t('mediaLib.trash.dueToday') : $t('mediaLib.trash.daysLeft', { days: left })}{/if}
						</div>
						<div class="text-xs text-white/50">{$t('mediaLib.trash.uploaded', { time: formatTime(viewerItem.media.created) })}</div>
					</div>
					<div class="flex flex-wrap items-center gap-2">
						<button class="info-btn" disabled={busy} on:click={() => viewerItem && restore([viewerItem.key])}>{$t('mediaLib.trash.restore')}</button>
						<button class="info-btn danger" disabled={busy} on:click={() => viewerItem && (confirm = { kind: 'purge', ids: [viewerItem.key] })}>{$t('mediaLib.trash.deleteForever')}</button>
					</div>
				</div>
			{/if}
		</div>
	</Lightbox>
{/if}

<style>
	.tb-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.375rem 0.75rem;
		border-radius: 0.5rem;
		border: 1px solid hsl(var(--border));
		font-size: 0.8125rem;
		color: hsl(var(--foreground));
		transition: background-color 0.15s ease;
	}
	.tb-btn:hover:not(:disabled) {
		background: hsl(var(--muted) / 0.6);
	}
	.tb-btn:disabled {
		opacity: 0.5;
	}
	.tb-btn.danger {
		color: rgb(225 29 72);
		border-color: rgb(225 29 72 / 0.35);
	}
	.tb-btn.danger:hover:not(:disabled) {
		background: rgb(225 29 72 / 0.08);
	}

	.bar-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.5rem 0.75rem;
		border-radius: 0.625rem;
		font-size: 0.8125rem;
		font-weight: 500;
		background: hsl(var(--muted));
		color: hsl(var(--foreground));
		transition: background-color 0.15s ease, opacity 0.15s ease;
		white-space: nowrap;
	}
	.bar-btn:hover:not(:disabled) {
		background: hsl(var(--muted) / 0.7);
	}
	.bar-btn:disabled {
		opacity: 0.45;
	}
	.bar-btn.danger {
		background: rgb(225 29 72);
		color: #fff;
	}
	.bar-btn.danger:hover:not(:disabled) {
		background: rgb(190 18 60);
	}
	@media (max-width: 340px) {
		.bar-btn span {
			display: none;
		}
	}

	.info-panel {
		max-width: 56rem;
		margin: 0 auto;
		padding: 12px 14px;
		border-radius: 14px;
		background: rgb(255 255 255 / 0.08);
		border: 1px solid rgb(255 255 255 / 0.08);
		backdrop-filter: blur(12px);
		font-size: 14px;
	}

	.info-btn {
		display: inline-flex;
		align-items: center;
		padding: 5px 12px;
		border-radius: 999px;
		border: 1px solid rgb(255 255 255 / 0.25);
		background: transparent;
		color: #fff;
		font-size: 12px;
		font-weight: 500;
		cursor: pointer;
		transition: background 0.15s ease;
	}
	.info-btn:hover:not(:disabled) {
		background: rgb(255 255 255 / 0.12);
	}
	.info-btn:disabled {
		opacity: 0.6;
		cursor: default;
	}
	.info-btn.danger {
		color: rgb(252 165 165);
		border-color: rgb(248 113 113 / 0.5);
	}
</style>
