<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { isAuthenticated } from '$lib/api/client';
	import { t, getIntlLocale } from '$lib/i18n';
	import { deleteMediaById } from '$lib/api/media';
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

	const feed: GalleryFeed = createGalleryFeed({ pageSize: 30 });

	// Lightbox
	let viewerOpen = false;
	let viewerIndex = 0;
	let deleting = false;
	let confirmDelete = false;
	let actionError = '';

	function openViewer(item: GalleryItem) {
		viewerIndex = $feed.items.findIndex((candidate) => candidate.key === item.key);
		confirmDelete = false;
		actionError = '';
		viewerOpen = viewerIndex >= 0;
	}

	function closeViewer() {
		viewerOpen = false;
		confirmDelete = false;
	}

	async function handleDelete(item: GalleryItem) {
		if (!item.media.id) return;
		deleting = true;
		actionError = '';
		const ok = await deleteMediaById(item.media.id);
		deleting = false;
		confirmDelete = false;
		if (!ok) {
			actionError = $t('mediaLib.library.deleteFailed');
			return;
		}
		feed.remove(item.key);
		const remaining = $feed.items.length;
		if (remaining === 0) closeViewer();
		else viewerIndex = Math.min(viewerIndex, remaining - 1);
	}

	function formatTimestamp(value: string | undefined): string {
		if (!value) return '';
		const date = new Date(value.replace(' ', 'T'));
		if (Number.isNaN(date.getTime())) return value;
		return date.toLocaleString(getIntlLocale(), { year: 'numeric', month: 'long', day: 'numeric', hour: '2-digit', minute: '2-digit' });
	}

	function goToDiary(date: string) {
		closeViewer();
		goto(`/diary/${date}`);
	}

	function loadMoreIfIdle() {
		if (!$feed.loading && !$feed.error && $feed.hasMore) feed.loadMore();
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		feed.loadMore();
	});

	onDestroy(() => feed.destroy());

	$: state = $feed;
	$: groups = groupByDay(state.items);
	$: viewerItem = viewerOpen ? state.items[viewerIndex] : undefined;
	$: if (viewerOpen && viewerIndex >= state.items.length - 3) loadMoreIfIdle();
	$: if (viewerOpen && viewerIndex >= 0) {
		// Reset per-image actions when navigating.
		viewerIndex;
		confirmDelete = false;
	}
	$: countLabel = state.initial ? '' : `(${state.total ?? state.items.length})`;
</script>

<svelte:head>
	<title>{$t('mediaLib.library.title')} - Diarum</title>
</svelte:head>

<div class="min-h-screen bg-background">
	<PageHeader title={$t('mediaLib.library.title')}>
		<span slot="subtitle" class="ml-1 text-sm text-muted-foreground tabular-nums">{countLabel}</span>
		<a
			slot="actions"
			href="/media/trash"
			class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 text-muted-foreground hover:text-foreground"
			title={$t('mediaLib.trash.entryHint')}
			aria-label={$t('mediaLib.trash.entry')}
		>
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
			</svg>
		</a>
	</PageHeader>

	<main class="max-w-5xl mx-auto px-4 py-6">
		{#if state.initial && state.loading}
			<!-- Skeleton timeline -->
			<div class="space-y-8" aria-busy="true" aria-label={$t('mediaLib.library.loading')}>
				{#each [8, 5] as count}
					<div>
						<div class="h-4 w-40 mb-4 rounded bg-muted animate-pulse"></div>
						<div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-3">
							{#each Array(count) as _}
								<div class="aspect-square rounded-lg bg-muted/60 animate-pulse"></div>
							{/each}
						</div>
					</div>
				{/each}
			</div>
		{:else if state.items.length === 0 && state.error}
			<div class="flex flex-col items-center justify-center py-20 gap-4 text-center">
				<div class="w-12 h-12 rounded-full bg-destructive/10 text-destructive flex items-center justify-center text-xl font-semibold">!</div>
				<div>
					<p class="text-lg font-medium text-foreground">{$t('mediaLib.library.loadFailed')}</p>
					<p class="text-sm text-muted-foreground mt-1 max-w-md">{state.error}</p>
				</div>
				<button class="px-4 py-2 text-sm rounded-lg border border-border hover:bg-muted/50 transition-colors" on:click={() => feed.reload()}>
					{$t('mediaLib.library.tryAgain')}
				</button>
			</div>
		{:else if state.items.length === 0}
			<div class="flex flex-col items-center justify-center py-20 gap-4">
				<svg class="w-16 h-16 text-muted-foreground/30" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
				</svg>
				<div class="text-muted-foreground text-center">
					<p class="text-lg font-medium">{$t('mediaLib.library.empty')}</p>
					<p class="text-sm mt-1">{$t('mediaLib.library.emptyHint')}</p>
				</div>
			</div>
		{:else}
			<!-- Timeline -->
			<div class="space-y-8">
				{#each groups as group (group.date)}
					<section class="animate-fade-in">
						<div class="sticky top-11 z-10 -mx-4 px-4 py-2 mb-2 flex items-center gap-3 bg-background/85 backdrop-blur-sm">
							<h2 class="text-sm font-medium text-foreground">{formatDayLabel(group.date)}</h2>
							<div class="flex-1 h-px bg-border/50"></div>
							<div class="text-xs text-muted-foreground tabular-nums">{$t(group.items.length === 1 ? 'mediaLib.library.itemCountOne' : 'mediaLib.library.itemCount', { count: group.items.length })}</div>
						</div>

						<div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-3">
							{#each group.items as item (item.key)}
								<button
									class="tile group relative aspect-square rounded-lg overflow-hidden border border-border/50 hover:border-primary/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary transition-colors duration-200"
									on:click={() => openViewer(item)}
									title={item.title}
								>
									<MediaThumb src={item.thumb} fallback={item.src} alt={item.title} />
									<div class="absolute inset-0 bg-gradient-to-t from-black/40 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-200"></div>
									{#if item.media.expand?.diary && item.media.expand.diary.length > 0}
										<div class="absolute bottom-2 left-2 px-2 py-0.5 bg-black/60 rounded text-xs text-white">
											{item.media.expand.diary[0].date?.split(' ')[0]}{item.media.expand.diary.length > 1 ? ` +${item.media.expand.diary.length - 1}` : ''}
										</div>
									{/if}
								</button>
							{/each}
						</div>
					</section>
				{/each}
			</div>

			<!-- Infinite scroll -->
			<div
				class="flex justify-center py-8"
				use:inView={{ callback: loadMoreIfIdle, key: `${state.items.length}:${state.loading}` }}
			>
				{#if state.loading}
					<div class="flex items-center gap-2 text-sm text-muted-foreground">
						<span class="w-4 h-4 rounded-full border-2 border-muted-foreground/30 border-t-primary animate-spin"></span>
						{$t('mediaLib.library.loadingMore')}
					</div>
				{:else if state.error}
					<div class="flex flex-col items-center gap-2 text-sm">
						<span class="text-destructive">{state.error}</span>
						<button class="px-3 py-1.5 rounded-lg border border-border hover:bg-muted/50 transition-colors" on:click={() => feed.loadMore()}>
							{$t('mediaLib.library.retry')}
						</button>
					</div>
				{:else if !state.hasMore}
					<span class="text-xs text-muted-foreground/70">{$t('mediaLib.library.end')}</span>
				{/if}
			</div>
		{/if}
	</main>

	<Footer maxWidth="4xl" tagline={$t('mediaLib.library.tagline')} />
</div>

{#if viewerOpen && state.items.length > 0}
	<Lightbox
		items={state.items.map((item) => ({ src: item.src, thumb: item.thumb, title: item.title }))}
		bind:index={viewerIndex}
		onClose={closeViewer}
		hasInfo
	>
		<div slot="info" class="info-panel">
			{#if viewerItem}
				<div class="flex flex-wrap items-start justify-between gap-3">
					<div class="min-w-0">
						<div class="font-medium truncate">{viewerItem.title}</div>
						<div class="text-xs text-white/60 mt-0.5">
							{#if viewerItem.media.date}{formatDayLabel(viewerItem.date)} · {/if}{$t('mediaLib.library.uploaded', { time: formatTimestamp(viewerItem.media.created) })}
						</div>
					</div>

					<div class="flex flex-wrap items-center gap-2">
						{#if !confirmDelete}
							<button class="info-btn danger" on:click={() => (confirmDelete = true)}>{$t('mediaLib.library.moveToTrash')}</button>
						{:else}
							<span class="text-xs text-red-300">
								{viewerItem.media.expand?.diary?.length ? $t('mediaLib.library.confirmUsed', { count: viewerItem.media.expand.diary.length }) : $t('mediaLib.library.confirm')}
							</span>
							<button class="info-btn danger solid" disabled={deleting} on:click={() => viewerItem && handleDelete(viewerItem)}>
								{deleting ? $t('mediaLib.library.moving') : $t('mediaLib.library.moveToTrash')}
							</button>
							<button class="info-btn" on:click={() => (confirmDelete = false)}>{$t('mediaLib.library.cancel')}</button>
						{/if}
					</div>
				</div>

				{#if viewerItem.media.expand?.diary && viewerItem.media.expand.diary.length > 0}
					<div class="flex flex-wrap items-center gap-2 mt-3">
						<span class="text-xs text-white/60">{$t('mediaLib.library.linkedDiaries')}</span>
						{#each viewerItem.media.expand.diary as diary}
							<button class="info-chip" on:click={() => goToDiary(diary.date.split(' ')[0])}>
								{diary.date?.split(' ')[0]}
							</button>
						{/each}
					</div>
				{/if}

				{#if actionError}
					<div class="text-xs text-red-300 mt-2">{actionError}</div>
				{/if}
			{/if}
		</div>
	</Lightbox>
{/if}

<style>
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
		transition: background 0.15s ease, border-color 0.15s ease;
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

	.info-btn.danger.solid {
		background: rgb(220 38 38);
		border-color: rgb(220 38 38);
		color: #fff;
	}

	.info-chip {
		padding: 3px 10px;
		border-radius: 999px;
		border: 0;
		background: rgb(255 255 255 / 0.12);
		color: #fff;
		font-size: 12px;
		cursor: pointer;
		transition: background 0.15s ease;
	}

	.info-chip:hover {
		background: rgb(255 255 255 / 0.22);
	}

	.tile :global(.thumb img) {
		transition: opacity 0.3s ease, transform 0.35s ease;
	}

	.tile:hover :global(.thumb.loaded img) {
		transform: scale(1.04);
	}
</style>
