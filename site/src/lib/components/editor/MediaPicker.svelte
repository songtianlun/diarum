<script lang="ts">
	import { portal } from '$lib/utils/portal';
	import { onMount, onDestroy } from 'svelte';
	import MediaThumb from '$lib/components/media/MediaThumb.svelte';
	import {
		createGalleryFeed,
		groupByDay,
		formatDayLabel,
		inView,
		type GalleryFeed,
		type GalleryItem
	} from '$lib/components/media/galleryFeed';
	import type { ImageInsert } from './ImageNodeView';

	export let onSelect: (images: ImageInsert[]) => void;
	export let onClose: () => void;

	const feed: GalleryFeed = createGalleryFeed({ pageSize: 48 });
	let searchQuery = '';
	let contentEl: HTMLDivElement;
	let searchInput: HTMLInputElement;

	// Selection keeps click order.
	let selected: GalleryItem[] = [];
	$: selectedKeys = new Map(selected.map((item, i) => [item.key, i + 1]));

	function toggle(item: GalleryItem) {
		selected = selectedKeys.has(item.key) ? selected.filter((s) => s.key !== item.key) : [...selected, item];
	}

	function toInsert(item: GalleryItem): ImageInsert {
		return { src: item.src, alt: item.title };
	}

	function insert(items: GalleryItem[]) {
		if (items.length === 0) return;
		onSelect(items.map(toInsert));
		onClose();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			onClose();
		} else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && selected.length > 0) {
			e.preventDefault();
			insert(selected);
		}
	}

	function handleOverlayClick(e: MouseEvent) {
		if (e.target === e.currentTarget) {
			onClose();
		}
	}

	function loadMoreIfIdle() {
		if (!$feed.loading && !$feed.error && $feed.hasMore) feed.loadMore();
	}

	onMount(() => {
		const { overflow } = document.body.style;
		document.body.style.overflow = 'hidden';
		searchInput?.focus({ preventScroll: true });

		feed.loadMore();

		return () => {
			document.body.style.overflow = overflow;
		};
	});

	onDestroy(() => feed.destroy());

	$: state = $feed;
	$: query = searchQuery.trim().toLowerCase();
	$: visible = query
		? state.items.filter((item) => item.title.toLowerCase().includes(query) || item.date.includes(query))
		: state.items;
	$: groups = groupByDay(visible);
</script>

<svelte:window on:keydown={handleKeydown} />

<div
	class="media-picker-overlay"
	use:portal
	on:click={handleOverlayClick}
	role="presentation"
>
	<div class="media-picker-modal" role="dialog" aria-modal="true" aria-label="Insert from gallery">
		<!-- Header -->
		<div class="media-picker-header">
			<h3>Insert from Gallery</h3>
			<button class="close-btn" on:click={onClose} title="Close (Esc)" aria-label="Close">
				<svg width="20" height="20" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
				</svg>
			</button>
		</div>

		<!-- Search -->
		<div class="media-picker-search">
			<svg class="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path stroke-linecap="round" d="M20 20l-3.5-3.5" /></svg>
			<input
				type="text"
				placeholder="Filter loaded images by name or date…"
				bind:value={searchQuery}
				bind:this={searchInput}
			/>
		</div>

		<!-- Content -->
		<div class="media-picker-content" bind:this={contentEl}>
			{#if state.initial && state.loading}
				<div class="media-grid" aria-busy="true">
					{#each Array(12) as _}
						<div class="media-skeleton"></div>
					{/each}
				</div>
			{:else if state.items.length === 0 && state.error}
				<div class="empty-state">
					<p class="error-text">{state.error}</p>
					<button class="text-btn" on:click={() => feed.reload()}>Try again</button>
				</div>
			{:else if visible.length === 0}
				<div class="empty-state">
					<svg width="48" height="48" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
					</svg>
					<p>{searchQuery ? 'No matching images' : 'No images in gallery'}</p>
				</div>
			{:else}
				{#each groups as group (group.date)}
					<div class="group-label">{formatDayLabel(group.date)}</div>
					<div class="media-grid">
						{#each group.items as item (item.key)}
							{@const order = selectedKeys.get(item.key)}
							<button
								class="media-item"
								class:selected={order}
								on:click={() => toggle(item)}
								on:dblclick|preventDefault={() => insert([item])}
								title="{item.title} — double-click to insert"
								aria-pressed={!!order}
							>
								<MediaThumb src={item.thumb} fallback={item.src} alt={item.title} />
								<span class="check" aria-hidden="true">
									{#if order}{order}{/if}
								</span>
							</button>
						{/each}
					</div>
				{/each}

				<div class="load-more" use:inView={{ callback: loadMoreIfIdle, root: contentEl, key: `${state.items.length}:${state.loading}` }}>
					{#if state.loading}
						<span class="spinner"></span>
					{:else if state.error}
						<span class="error-text">{state.error}</span>
						<button class="text-btn" on:click={() => feed.loadMore()}>Retry</button>
					{/if}
				</div>
			{/if}
		</div>

		<!-- Footer -->
		<div class="media-picker-footer">
			<span class="footer-hint">
				{#if selected.length > 0}
					{selected.length} selected
					<button class="text-btn" on:click={() => (selected = [])}>Clear</button>
				{:else}
					Click to select, double-click to insert one
				{/if}
			</span>
			<div class="footer-actions">
				<button class="btn" on:click={onClose}>Cancel</button>
				<button class="btn primary" disabled={selected.length === 0} on:click={() => insert(selected)}>
					Insert{selected.length > 1 ? ` ${selected.length} images` : ''}
				</button>
			</div>
		</div>
	</div>
</div>

<style>
	.media-picker-overlay {
		position: fixed;
		inset: 0;
		z-index: 100;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 1rem;
		background: rgba(0, 0, 0, 0.6);
		backdrop-filter: blur(4px);
		animation: fadeIn 0.15s ease;
	}

	.media-picker-modal {
		background: hsl(var(--card, 0 0% 100%));
		border-radius: 14px;
		box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
		width: 100%;
		max-width: 760px;
		height: min(80vh, 760px);
		display: flex;
		flex-direction: column;
		overflow: hidden;
		animation: scaleIn 0.18s cubic-bezier(0.2, 0.9, 0.3, 1.1);
	}

	.media-picker-header {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.875rem 1rem 0.875rem 1.25rem;
		border-bottom: 1px solid hsl(var(--border, 0 0% 90%) / 0.5);
	}

	.media-picker-header h3 {
		flex: 1;
		font-size: 1rem;
		font-weight: 600;
		color: hsl(var(--foreground, 0 0% 10%));
		margin: 0;
		white-space: nowrap;
	}

	.close-btn {
		padding: 0.375rem;
		background: transparent;
		border: none;
		border-radius: 6px;
		color: hsl(var(--muted-foreground, 0 0% 50%));
		cursor: pointer;
		transition: all 0.15s ease;
	}

	.close-btn:hover {
		background: hsl(var(--muted, 0 0% 95%) / 0.5);
		color: hsl(var(--foreground, 0 0% 10%));
	}

	.media-picker-search {
		position: relative;
		padding: 0.75rem 1.25rem;
		border-bottom: 1px solid hsl(var(--border, 0 0% 90%) / 0.5);
	}

	.search-icon {
		position: absolute;
		left: 1.85rem;
		top: 50%;
		width: 16px;
		height: 16px;
		margin-top: -8px;
		color: hsl(var(--muted-foreground));
		pointer-events: none;
	}

	.media-picker-search input {
		width: 100%;
		padding: 0.5rem 0.75rem 0.5rem 2rem;
		font-size: 0.875rem;
		border: 1px solid hsl(var(--border, 0 0% 90%));
		border-radius: 8px;
		background: hsl(var(--background, 0 0% 100%));
		color: hsl(var(--foreground, 0 0% 10%));
		outline: none;
		transition: border-color 0.15s ease;
	}

	.media-picker-search input:focus {
		border-color: hsl(var(--primary, 220 90% 56%));
	}

	.media-picker-search input::placeholder {
		color: hsl(var(--muted-foreground, 0 0% 50%));
	}

	.media-picker-content {
		flex: 1;
		overflow-y: auto;
		padding: 0.25rem 1rem 1rem;
		min-height: 200px;
		overscroll-behavior: contain;
	}

	.group-label {
		position: sticky;
		top: 0;
		z-index: 1;
		padding: 0.625rem 0.25rem 0.5rem;
		font-size: 0.75rem;
		font-weight: 500;
		color: hsl(var(--muted-foreground));
		background: hsl(var(--card) / 0.92);
		backdrop-filter: blur(4px);
	}

	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.75rem;
		padding: 3rem 1rem;
		text-align: center;
		color: hsl(var(--muted-foreground, 0 0% 50%));
	}

	.empty-state p {
		margin: 0;
	}

	.error-text {
		color: hsl(var(--destructive, 0 84% 60%));
		font-size: 0.85rem;
	}

	.media-grid {
		display: grid;
		grid-template-columns: repeat(5, 1fr);
		gap: 0.5rem;
		padding-top: 0.25rem;
	}

	@media (max-width: 640px) {
		.media-grid {
			grid-template-columns: repeat(4, 1fr);
		}
	}

	@media (max-width: 420px) {
		.media-grid {
			grid-template-columns: repeat(3, 1fr);
		}
	}

	.media-skeleton {
		aspect-ratio: 1;
		border-radius: 8px;
		background: hsl(var(--muted) / 0.7);
		animation: pulse 1.4s ease-in-out infinite;
	}

	.media-item {
		position: relative;
		aspect-ratio: 1;
		border-radius: 8px;
		overflow: hidden;
		border: 0;
		cursor: pointer;
		padding: 0;
		background: transparent;
		transition: transform 0.15s ease;
	}

	.media-item::after {
		content: '';
		position: absolute;
		inset: 0;
		border-radius: 8px;
		box-shadow: inset 0 0 0 1px hsl(var(--border) / 0.6);
		transition: box-shadow 0.15s ease, background 0.15s ease;
		pointer-events: none;
	}

	.media-item:hover::after {
		box-shadow: inset 0 0 0 2px hsl(var(--primary) / 0.6);
	}

	.media-item:focus-visible {
		outline: 2px solid hsl(var(--primary));
		outline-offset: 2px;
	}

	.media-item.selected {
		transform: scale(0.94);
	}

	.media-item.selected::after {
		box-shadow: inset 0 0 0 3px hsl(var(--primary));
		background: hsl(var(--primary) / 0.12);
	}

	.check {
		position: absolute;
		top: 6px;
		right: 6px;
		z-index: 1;
		display: flex;
		align-items: center;
		justify-content: center;
		width: 22px;
		height: 22px;
		border-radius: 50%;
		border: 2px solid rgb(255 255 255 / 0.9);
		background: rgb(0 0 0 / 0.25);
		color: #fff;
		font-size: 11px;
		font-weight: 700;
		opacity: 0;
		transition: opacity 0.15s ease, background 0.15s ease;
		box-shadow: 0 1px 3px rgb(0 0 0 / 0.3);
	}

	.media-item:hover .check {
		opacity: 1;
	}

	.media-item.selected .check {
		opacity: 1;
		background: hsl(var(--primary));
		border-color: hsl(var(--primary));
	}

	.load-more {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 0.75rem;
		min-height: 3rem;
		padding-top: 0.5rem;
	}

	.spinner {
		width: 18px;
		height: 18px;
		border-radius: 50%;
		border: 2px solid hsl(var(--muted-foreground) / 0.3);
		border-top-color: hsl(var(--primary));
		animation: spin 0.8s linear infinite;
	}

	.media-picker-footer {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		padding: 0.75rem 1.25rem;
		border-top: 1px solid hsl(var(--border, 0 0% 90%) / 0.5);
	}

	.footer-hint {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.8rem;
		color: hsl(var(--muted-foreground, 0 0% 50%));
	}

	.footer-actions {
		display: flex;
		gap: 0.5rem;
	}

	.text-btn {
		padding: 0;
		border: 0;
		background: transparent;
		color: hsl(var(--primary));
		font-size: 0.8rem;
		cursor: pointer;
	}

	.text-btn:hover {
		text-decoration: underline;
	}

	.btn {
		padding: 0.45rem 0.9rem;
		font-size: 0.85rem;
		font-weight: 500;
		border: 1px solid hsl(var(--border, 0 0% 90%));
		border-radius: 8px;
		background: transparent;
		color: hsl(var(--foreground, 0 0% 10%));
		cursor: pointer;
		transition: all 0.15s ease;
		white-space: nowrap;
	}

	.btn:hover:not(:disabled) {
		background: hsl(var(--muted, 0 0% 95%) / 0.5);
	}

	.btn.primary {
		border-color: hsl(var(--primary));
		background: hsl(var(--primary));
		color: hsl(var(--primary-foreground));
	}

	.btn.primary:hover:not(:disabled) {
		opacity: 0.9;
		background: hsl(var(--primary));
	}

	.btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	@media (max-width: 520px) {
		.media-picker-overlay {
			padding: 0;
			align-items: flex-end;
		}
		.media-picker-modal {
			height: 88vh;
			border-radius: 16px 16px 0 0;
			animation: slideUp 0.22s cubic-bezier(0.2, 0.9, 0.3, 1);
		}
		.media-picker-header h3 {
			font-size: 0.95rem;
		}
		.footer-hint {
			display: none;
		}
		.media-picker-footer {
			justify-content: flex-end;
		}
	}

	@keyframes fadeIn {
		from { opacity: 0; }
		to { opacity: 1; }
	}

	@keyframes scaleIn {
		from { transform: scale(0.96); opacity: 0; }
		to { transform: scale(1); opacity: 1; }
	}

	@keyframes slideUp {
		from { transform: translateY(24px); opacity: 0; }
		to { transform: none; opacity: 1; }
	}

	@keyframes pulse {
		0%, 100% { opacity: 1; }
		50% { opacity: 0.55; }
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}
</style>
