<script lang="ts">
	import { onMount } from 'svelte';
	import { getAllMedia, getMediaFileUrl, type MediaWithDiary } from '$lib/api/media';

	export let onSelect: (media: MediaWithDiary) => void;
	export let onClose: () => void;

	let mediaList: MediaWithDiary[] = [];
	let loading = true;
	let currentPage = 1;
	let totalPages = 1;
	let searchQuery = '';

	async function loadMedia() {
		loading = true;
		const result = await getAllMedia(currentPage, 24);
		mediaList = result.items;
		totalPages = result.totalPages;
		loading = false;
	}

	function handleSelect(media: MediaWithDiary) {
		onSelect(media);
		onClose();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			onClose();
		}
	}

	function handleOverlayClick(e: MouseEvent) {
		if (e.target === e.currentTarget) {
			onClose();
		}
	}

	function formatDate(dateStr: string): string {
		const date = new Date(dateStr);
		return date.toLocaleDateString('zh-CN', {
			month: 'short',
			day: 'numeric'
		});
	}

	$: filteredMedia = searchQuery
		? mediaList.filter(m =>
			m.name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
			m.alt?.toLowerCase().includes(searchQuery.toLowerCase())
		)
		: mediaList;

	onMount(() => {
		loadMedia();
	});
</script>

<svelte:window on:keydown={handleKeydown} />

<div
	class="media-picker-overlay"
	on:click={handleOverlayClick}
	on:keydown={handleKeydown}
	role="dialog"
	tabindex="-1"
>
	<div
		class="media-picker-modal"
		role="document"
	>
		<!-- Header -->
		<div class="media-picker-header">
			<h3>Select from Gallery</h3>
			<button class="close-btn" on:click={onClose} title="Close">
				<svg width="20" height="20" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
				</svg>
			</button>
		</div>

		<!-- Search -->
		<div class="media-picker-search">
			<input
				type="text"
				placeholder="Search images..."
				bind:value={searchQuery}
			/>
		</div>

		<!-- Content -->
		<div class="media-picker-content">
			{#if loading}
				<div class="loading-state">
					<svg class="spinner" viewBox="0 0 24 24">
						<circle class="spinner-bg" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
						<path class="spinner-fg" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
					</svg>
					<span>Loading...</span>
				</div>
			{:else if filteredMedia.length === 0}
				<div class="empty-state">
					<svg width="48" height="48" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
					</svg>
					<p>{searchQuery ? 'No matching images' : 'No images in gallery'}</p>
				</div>
			{:else}
				<div class="media-grid">
					{#each filteredMedia as media}
						<button
							class="media-item"
							on:click={() => handleSelect(media)}
							title={media.name || 'Select image'}
						>
							<img
								src={getMediaFileUrl(media, '200x200')}
								alt={media.alt || media.name || 'Media'}
								loading="lazy"
							/>
							<div class="media-item-overlay">
								<span class="media-date">{formatDate(media.created || '')}</span>
							</div>
						</button>
					{/each}
				</div>
			{/if}
		</div>

		<!-- Pagination -->
		{#if totalPages > 1 && !loading}
			<div class="media-picker-footer">
				<button
					disabled={currentPage === 1}
					on:click={() => { currentPage--; loadMedia(); }}
				>
					Previous
				</button>
				<span>{currentPage} / {totalPages}</span>
				<button
					disabled={currentPage === totalPages}
					on:click={() => { currentPage++; loadMedia(); }}
				>
					Next
				</button>
			</div>
		{/if}
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
		border-radius: 12px;
		box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
		width: 100%;
		max-width: 640px;
		max-height: 80vh;
		display: flex;
		flex-direction: column;
		animation: scaleIn 0.15s ease;
	}

	.media-picker-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 1rem 1.25rem;
		border-bottom: 1px solid hsl(var(--border, 0 0% 90%) / 0.5);
	}

	.media-picker-header h3 {
		font-size: 1rem;
		font-weight: 600;
		color: hsl(var(--foreground, 0 0% 10%));
		margin: 0;
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
		padding: 0.75rem 1.25rem;
		border-bottom: 1px solid hsl(var(--border, 0 0% 90%) / 0.5);
	}

	.media-picker-search input {
		width: 100%;
		padding: 0.5rem 0.75rem;
		font-size: 0.875rem;
		border: 1px solid hsl(var(--border, 0 0% 90%));
		border-radius: 6px;
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
		padding: 1rem;
		min-height: 200px;
	}

	.loading-state,
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.75rem;
		padding: 3rem 1rem;
		color: hsl(var(--muted-foreground, 0 0% 50%));
	}

	.spinner {
		width: 24px;
		height: 24px;
		animation: spin 1s linear infinite;
	}

	.spinner-bg {
		opacity: 0.25;
	}

	.spinner-fg {
		opacity: 0.75;
	}

	.media-grid {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: 0.5rem;
	}

	@media (max-width: 480px) {
		.media-grid {
			grid-template-columns: repeat(3, 1fr);
		}
	}

	.media-item {
		position: relative;
		aspect-ratio: 1;
		border-radius: 8px;
		overflow: hidden;
		background: hsl(var(--muted, 0 0% 95%) / 0.3);
		border: 2px solid transparent;
		cursor: pointer;
		transition: all 0.15s ease;
		padding: 0;
	}

	.media-item:hover {
		border-color: hsl(var(--primary, 220 90% 56%));
		transform: scale(1.02);
	}

	.media-item img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}

	.media-item-overlay {
		position: absolute;
		inset: 0;
		background: linear-gradient(to top, rgba(0,0,0,0.5) 0%, transparent 50%);
		opacity: 0;
		transition: opacity 0.15s ease;
		display: flex;
		align-items: flex-end;
		padding: 0.5rem;
	}

	.media-item:hover .media-item-overlay {
		opacity: 1;
	}

	.media-date {
		font-size: 0.7rem;
		color: white;
	}

	.media-picker-footer {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 0.75rem;
		padding: 0.75rem 1.25rem;
		border-top: 1px solid hsl(var(--border, 0 0% 90%) / 0.5);
	}

	.media-picker-footer button {
		padding: 0.375rem 0.75rem;
		font-size: 0.8rem;
		border: 1px solid hsl(var(--border, 0 0% 90%));
		border-radius: 6px;
		background: transparent;
		color: hsl(var(--foreground, 0 0% 10%));
		cursor: pointer;
		transition: all 0.15s ease;
	}

	.media-picker-footer button:hover:not(:disabled) {
		background: hsl(var(--muted, 0 0% 95%) / 0.5);
	}

	.media-picker-footer button:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.media-picker-footer span {
		font-size: 0.8rem;
		color: hsl(var(--muted-foreground, 0 0% 50%));
	}

	@keyframes fadeIn {
		from { opacity: 0; }
		to { opacity: 1; }
	}

	@keyframes scaleIn {
		from { transform: scale(0.95); opacity: 0; }
		to { transform: scale(1); opacity: 1; }
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to { transform: rotate(360deg); }
	}
</style>
