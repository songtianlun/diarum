<script lang="ts">
	import { getMediaByDiaryId, getMediaFileUrl, deleteMediaById, type MediaWithDiary } from '$lib/api/media';
	import { uploadImage, getOrCreateDiaryId, isCheveretoResult } from '$lib/utils/uploadImage';

	export let diaryDate: string;

	let attachments: MediaWithDiary[] = [];
	let loading = false;
	let uploading = false;
	let uploadError = '';
	let diaryId: string | undefined = undefined;
	let fileInput: HTMLInputElement;
	let previewMedia: MediaWithDiary | null = null;
	let showDeleteConfirm: string | null = null;

	const IMAGE_CONFIG = {
		maxSize: 50 * 1024 * 1024,
		allowedTypes: ['image/jpeg', 'image/png', 'image/gif', 'image/webp'],
	};

	async function ensureDiaryId(): Promise<string | undefined> {
		if (!diaryId) {
			diaryId = await getOrCreateDiaryId(diaryDate);
		}
		return diaryId;
	}

	async function loadAttachments() {
		if (!diaryDate) return;
		loading = true;
		try {
			const id = await ensureDiaryId();
			if (id) {
				attachments = await getMediaByDiaryId(id);
			}
		} catch (error) {
			console.error('Failed to load attachments:', error);
		}
		loading = false;
	}

	async function handleFiles(files: FileList) {
		if (!files || files.length === 0) return;

		uploading = true;
		uploadError = '';

		const id = await ensureDiaryId();

		for (const file of Array.from(files)) {
			if (!IMAGE_CONFIG.allowedTypes.includes(file.type)) {
				uploadError = `${file.name}: unsupported format (use JPG, PNG, GIF, WebP)`;
				continue;
			}
			if (file.size > IMAGE_CONFIG.maxSize) {
				uploadError = `${file.name}: file exceeds 50 MB limit`;
				continue;
			}

			try {
				const result = await uploadImage(file, { diaryDate, diaryId: id });

				if (isCheveretoResult(result)) {
					// Chevereto doesn't give us a media record; refresh from the server.
					await loadAttachments();
				} else {
					// Cast to MediaWithDiary (no expand needed)
					attachments = [result as MediaWithDiary, ...attachments];
				}
			} catch (err) {
				console.error('Upload failed:', err);
				uploadError = `Failed to upload ${file.name}`;
			}
		}

		uploading = false;
		if (uploadError) {
			setTimeout(() => (uploadError = ''), 4000);
		}

		// Reset input so the same files can be re-selected
		if (fileInput) fileInput.value = '';
	}

	function handleFileChange(event: Event) {
		const input = event.target as HTMLInputElement;
		if (input.files) {
			handleFiles(input.files);
		}
	}

	function handleDrop(event: DragEvent) {
		event.preventDefault();
		dragOver = false;
		if (event.dataTransfer?.files) {
			handleFiles(event.dataTransfer.files);
		}
	}

	let dragOver = false;

	function handleDragOver(event: DragEvent) {
		event.preventDefault();
		dragOver = true;
	}

	function handleDragLeave() {
		dragOver = false;
	}

	async function handleDelete(media: MediaWithDiary) {
		if (!media.id) return;
		const success = await deleteMediaById(media.id);
		if (success) {
			attachments = attachments.filter((m) => m.id !== media.id);
		}
		showDeleteConfirm = null;
		if (previewMedia?.id === media.id) {
			previewMedia = null;
		}
	}

	function openPreview(media: MediaWithDiary) {
		previewMedia = media;
		showDeleteConfirm = null;
	}

	function closePreview() {
		previewMedia = null;
		showDeleteConfirm = null;
	}

	function handlePreviewKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape') closePreview();
	}

	function handlePreviewBackdrop(event: MouseEvent) {
		if (event.target === event.currentTarget) closePreview();
	}

	// Reload when date changes (also fires on initial mount)
	$: if (diaryDate) {
		diaryId = undefined;
		loadAttachments();
	}
</script>

<div class="attachments-panel">
	<!-- Header -->
	<div class="attachments-header">
		<div class="attachments-title">
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
					d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
			</svg>
			<span>Photos</span>
			{#if attachments.length > 0}
				<span class="count-badge">{attachments.length}</span>
			{/if}
		</div>

		<!-- Upload button -->
		<button
			class="upload-btn"
			on:click={() => fileInput?.click()}
			disabled={uploading}
			title="Add photos"
		>
			{#if uploading}
				<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
				</svg>
				<span>Uploading…</span>
			{:else}
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
				</svg>
				<span>Add Photos</span>
			{/if}
		</button>
	</div>

	<!-- Error message -->
	{#if uploadError}
		<div class="upload-error">{uploadError}</div>
	{/if}

	<!-- Drop zone / grid -->
	{#if loading}
		<div class="loading-state">
			<svg class="w-5 h-5 animate-spin text-muted-foreground" fill="none" viewBox="0 0 24 24">
				<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
				<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
			</svg>
		</div>
	{:else if attachments.length === 0}
		<!-- Drop zone when empty -->
		<div
			class="drop-zone {dragOver ? 'drag-active' : ''}"
			on:drop={handleDrop}
			on:dragover={handleDragOver}
			on:dragleave={handleDragLeave}
			role="button"
			tabindex="0"
			aria-label="Drop images here or click to upload"
			on:click={() => fileInput?.click()}
			on:keydown={(e) => e.key === 'Enter' && fileInput?.click()}
		>
			<svg class="w-8 h-8 text-muted-foreground/50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
					d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
			</svg>
			<p class="drop-hint">Tap to add photos, or drag &amp; drop</p>
		</div>
	{:else}
		<!-- Photo grid -->
		<div
			class="photo-grid"
			role="group"
			aria-label="Photo attachments"
			on:drop={handleDrop}
			on:dragover={handleDragOver}
			on:dragleave={handleDragLeave}
		>
			{#each attachments as media (media.id)}
				<button
					class="photo-item"
					on:click={() => openPreview(media)}
					title={media.name || 'View photo'}
				>
					<img
						src={getMediaFileUrl(media, '300x300')}
						alt={media.alt || media.name || 'Attachment'}
						loading="lazy"
					/>
				</button>
			{/each}

			<!-- Add more button inside grid -->
			<button
				class="photo-add-more"
				on:click={() => fileInput?.click()}
				disabled={uploading}
				title="Add more photos"
			>
				<svg class="w-6 h-6 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
				</svg>
			</button>
		</div>
	{/if}

	<!-- Hidden file input: multiple allows selecting several images at once -->
	<input
		type="file"
		accept="image/*"
		multiple
		bind:this={fileInput}
		on:change={handleFileChange}
		style="display:none;"
	/>
</div>

<!-- Preview modal -->
{#if previewMedia}
	<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
	<div
		class="preview-overlay"
		role="dialog"
		aria-modal="true"
		on:click={handlePreviewBackdrop}
		on:keydown={handlePreviewKeydown}
		tabindex="-1"
	>
		<div class="preview-modal">
			<!-- Close button -->
			<button class="preview-close" on:click={closePreview} title="Close">
				<svg width="20" height="20" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
				</svg>
			</button>

			<!-- Image -->
			<img
				src={getMediaFileUrl(previewMedia)}
				alt={previewMedia.alt || previewMedia.name || 'Attachment'}
				class="preview-img"
			/>

			<!-- Actions -->
			<div class="preview-actions">
				{#if showDeleteConfirm === previewMedia.id}
					<span class="delete-confirm-text">Delete this photo?</span>
					<button class="btn-danger" on:click={() => previewMedia && handleDelete(previewMedia)}>
						Yes, delete
					</button>
					<button class="btn-secondary" on:click={() => (showDeleteConfirm = null)}>
						Cancel
					</button>
				{:else}
					<button
						class="btn-danger-outline"
						on:click={() => (showDeleteConfirm = previewMedia?.id ?? null)}
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
						</svg>
						Delete
					</button>
				{/if}
			</div>
		</div>
	</div>
{/if}

<style>
	.attachments-panel {
		border-top: 1px solid hsl(var(--border) / 0.5);
		padding: 1rem 1.25rem;
	}

	.attachments-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 0.75rem;
	}

	.attachments-title {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.875rem;
		font-weight: 600;
		color: hsl(var(--foreground));
	}

	.count-badge {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-width: 1.25rem;
		height: 1.25rem;
		padding: 0 0.35rem;
		font-size: 0.7rem;
		font-weight: 600;
		background: hsl(var(--primary) / 0.15);
		color: hsl(var(--primary));
		border-radius: 999px;
	}

	.upload-btn {
		display: flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.4rem 0.75rem;
		font-size: 0.8rem;
		font-weight: 500;
		background: hsl(var(--primary));
		color: hsl(var(--primary-foreground));
		border: none;
		border-radius: 6px;
		cursor: pointer;
		transition: opacity 0.15s;
		/* Large enough for mobile tap target */
		min-height: 36px;
	}

	.upload-btn:hover:not(:disabled) {
		opacity: 0.88;
	}

	.upload-btn:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.upload-error {
		margin-bottom: 0.75rem;
		padding: 0.5rem 0.75rem;
		font-size: 0.8rem;
		color: hsl(0 84% 60%);
		background: hsl(0 84% 60% / 0.1);
		border-radius: 6px;
	}

	.loading-state {
		display: flex;
		justify-content: center;
		padding: 1.5rem;
	}

	.drop-zone {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.5rem;
		padding: 2rem 1rem;
		border: 2px dashed hsl(var(--border));
		border-radius: 10px;
		cursor: pointer;
		transition: background 0.15s, border-color 0.15s;
		text-align: center;
	}

	.drop-zone:hover,
	.drop-zone.drag-active {
		background: hsl(var(--primary) / 0.05);
		border-color: hsl(var(--primary) / 0.5);
	}

	.drop-hint {
		font-size: 0.8rem;
		color: hsl(var(--muted-foreground));
		margin: 0;
	}

	.photo-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(90px, 1fr));
		gap: 0.5rem;
	}

	@media (max-width: 480px) {
		.photo-grid {
			grid-template-columns: repeat(3, 1fr);
		}
	}

	.photo-item {
		position: relative;
		aspect-ratio: 1;
		border-radius: 8px;
		overflow: hidden;
		background: hsl(var(--muted) / 0.3);
		border: none;
		cursor: pointer;
		padding: 0;
		transition: transform 0.15s, box-shadow 0.15s;
	}

	.photo-item:hover {
		transform: scale(1.03);
		box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
	}

	.photo-item img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}

	.photo-add-more {
		aspect-ratio: 1;
		border-radius: 8px;
		border: 2px dashed hsl(var(--border));
		background: transparent;
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
		transition: background 0.15s, border-color 0.15s;
	}

	.photo-add-more:hover:not(:disabled) {
		background: hsl(var(--primary) / 0.05);
		border-color: hsl(var(--primary) / 0.5);
	}

	.photo-add-more:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	/* Preview overlay */
	.preview-overlay {
		position: fixed;
		inset: 0;
		z-index: 200;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 1rem;
		background: rgba(0, 0, 0, 0.75);
		backdrop-filter: blur(4px);
		animation: fadeIn 0.15s ease;
	}

	.preview-modal {
		position: relative;
		max-width: min(90vw, 800px);
		max-height: 90vh;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.75rem;
	}

	.preview-close {
		position: absolute;
		top: -2.5rem;
		right: 0;
		background: rgba(255, 255, 255, 0.15);
		border: none;
		border-radius: 50%;
		width: 2rem;
		height: 2rem;
		display: flex;
		align-items: center;
		justify-content: center;
		cursor: pointer;
		color: white;
		transition: background 0.15s;
	}

	.preview-close:hover {
		background: rgba(255, 255, 255, 0.25);
	}

	.preview-img {
		max-width: 100%;
		max-height: 75vh;
		border-radius: 8px;
		object-fit: contain;
		box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4);
	}

	.preview-actions {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.delete-confirm-text {
		font-size: 0.85rem;
		color: white;
	}

	.btn-danger,
	.btn-secondary,
	.btn-danger-outline {
		display: flex;
		align-items: center;
		gap: 0.35rem;
		padding: 0.4rem 0.9rem;
		font-size: 0.8rem;
		font-weight: 500;
		border-radius: 6px;
		cursor: pointer;
		transition: opacity 0.15s;
		min-height: 36px;
	}

	.btn-danger {
		background: hsl(0 84% 60%);
		color: white;
		border: none;
	}

	.btn-danger:hover {
		opacity: 0.88;
	}

	.btn-secondary {
		background: rgba(255, 255, 255, 0.15);
		color: white;
		border: 1px solid rgba(255, 255, 255, 0.3);
	}

	.btn-secondary:hover {
		background: rgba(255, 255, 255, 0.25);
	}

	.btn-danger-outline {
		background: transparent;
		color: white;
		border: 1px solid rgba(255, 255, 255, 0.4);
	}

	.btn-danger-outline:hover {
		background: hsl(0 84% 60% / 0.2);
		border-color: hsl(0 84% 60%);
	}

	@keyframes fadeIn {
		from { opacity: 0; }
		to { opacity: 1; }
	}
</style>
