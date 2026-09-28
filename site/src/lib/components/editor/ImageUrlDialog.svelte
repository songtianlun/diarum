<script lang="ts">
	import { portal } from '$lib/utils/portal';
	import { onMount } from 'svelte';
	import type { ImageInsert } from './ImageNodeView';

	export let onInsert: (image: ImageInsert) => void;
	export let onClose: () => void;

	let url = '';
	let alt = '';
	let urlInput: HTMLInputElement;
	let previewState: 'idle' | 'loading' | 'ok' | 'error' = 'idle';
	let previewSrc = '';
	let debounce: ReturnType<typeof setTimeout> | undefined;

	$: trimmed = url.trim();
	$: schedulePreview(trimmed);

	// Wait until typing pauses before loading, so each keystroke is not a request.
	function schedulePreview(value: string) {
		clearTimeout(debounce);
		if (!value) {
			previewState = 'idle';
			previewSrc = '';
			return;
		}
		previewState = 'loading';
		debounce = setTimeout(() => (previewSrc = value), 350);
	}

	function submit() {
		if (!trimmed) return;
		onInsert({ src: trimmed, alt: alt.trim() || undefined });
		onClose();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			onClose();
		}
	}

	function handleOverlayClick(e: MouseEvent) {
		if (e.target === e.currentTarget) onClose();
	}

	onMount(() => {
		urlInput?.focus();
		return () => clearTimeout(debounce);
	});
</script>

<svelte:window on:keydown={handleKeydown} />

<div class="url-dialog-overlay" use:portal on:click={handleOverlayClick} role="presentation">
	<div role="dialog" aria-modal="true" aria-label="Insert image from URL" class="url-dialog-frame">
	<form class="url-dialog" on:submit|preventDefault={submit}>
		<div class="url-dialog-header">
			<h3>Insert image from URL</h3>
			<button type="button" class="close-btn" on:click={onClose} title="Close (Esc)" aria-label="Close">
				<svg width="20" height="20" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
				</svg>
			</button>
		</div>

		<div class="url-dialog-body">
			<label class="field">
				<span>Image URL</span>
				<!-- Not type="url": relative paths such as built-in media links are valid here. -->
				<input
					bind:this={urlInput}
					bind:value={url}
					type="text"
					inputmode="url"
					spellcheck="false"
					autocomplete="off"
					placeholder="https://example.com/photo.jpg"
				/>
			</label>
			<label class="field">
				<span>Description <em>(optional)</em></span>
				<input bind:value={alt} type="text" placeholder="Shown when the image can't load" />
			</label>

			<div class="preview" class:has-image={previewState === 'ok'}>
				{#if previewState === 'idle'}
					<span class="preview-hint">A preview appears here</span>
				{:else}
					{#if previewState === 'loading'}
						<span class="preview-spinner" aria-label="Loading preview"></span>
					{:else if previewState === 'error'}
						<span class="preview-error">Couldn't load this image. Check the link, or insert it anyway.</span>
					{/if}
					{#if previewSrc}
						{#key previewSrc}
							<img
								src={previewSrc}
								alt=""
								class:visible={previewState === 'ok'}
								on:load={() => previewSrc === trimmed && (previewState = 'ok')}
								on:error={() => previewSrc === trimmed && (previewState = 'error')}
							/>
						{/key}
					{/if}
				{/if}
			</div>
		</div>

		<div class="url-dialog-footer">
			<button type="button" class="btn" on:click={onClose}>Cancel</button>
			<button type="submit" class="btn primary" disabled={!trimmed}>
				{previewState === 'error' ? 'Insert anyway' : 'Insert'}
			</button>
		</div>
	</form>
	</div>
</div>

<style>
	.url-dialog-overlay {
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

	.url-dialog-frame {
		width: 100%;
		max-width: 480px;
	}

	.url-dialog {
		width: 100%;
		max-width: 480px;
		display: flex;
		flex-direction: column;
		background: hsl(var(--card));
		border-radius: 14px;
		box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
		overflow: hidden;
		animation: scaleIn 0.18s cubic-bezier(0.2, 0.9, 0.3, 1.1);
	}

	.url-dialog-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.875rem 1rem 0.875rem 1.25rem;
		border-bottom: 1px solid hsl(var(--border) / 0.5);
	}

	.url-dialog-header h3 {
		margin: 0;
		font-size: 1rem;
		font-weight: 600;
		color: hsl(var(--foreground));
	}

	.close-btn {
		padding: 0.375rem;
		border: none;
		border-radius: 6px;
		background: transparent;
		color: hsl(var(--muted-foreground));
		cursor: pointer;
	}

	.close-btn:hover {
		background: hsl(var(--muted) / 0.5);
		color: hsl(var(--foreground));
	}

	.url-dialog-body {
		display: flex;
		flex-direction: column;
		gap: 0.875rem;
		padding: 1rem 1.25rem;
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: 0.375rem;
		font-size: 0.8rem;
		font-weight: 500;
		color: hsl(var(--foreground));
	}

	.field em {
		font-style: normal;
		font-weight: 400;
		color: hsl(var(--muted-foreground));
	}

	.field input {
		padding: 0.5rem 0.75rem;
		font-size: 0.875rem;
		font-weight: 400;
		border: 1px solid hsl(var(--border));
		border-radius: 8px;
		background: hsl(var(--background));
		color: hsl(var(--foreground));
		outline: none;
		transition: border-color 0.15s ease;
	}

	.field input:focus {
		border-color: hsl(var(--primary));
	}

	.preview {
		position: relative;
		display: flex;
		align-items: center;
		justify-content: center;
		min-height: 140px;
		max-height: 240px;
		padding: 0.75rem;
		border-radius: 10px;
		border: 1px dashed hsl(var(--border));
		background: hsl(var(--muted) / 0.4);
		overflow: hidden;
		text-align: center;
	}

	.preview.has-image {
		border-style: solid;
	}

	.preview img {
		display: none;
		max-width: 100%;
		max-height: 216px;
		object-fit: contain;
		border-radius: 6px;
	}

	.preview img.visible {
		display: block;
		animation: fadeIn 0.25s ease;
	}

	.preview-hint {
		font-size: 0.8rem;
		color: hsl(var(--muted-foreground));
	}

	.preview-error {
		font-size: 0.8rem;
		color: hsl(var(--destructive, 0 84% 60%));
	}

	.preview-spinner {
		width: 22px;
		height: 22px;
		border-radius: 50%;
		border: 2px solid hsl(var(--muted-foreground) / 0.3);
		border-top-color: hsl(var(--primary));
		animation: spin 0.8s linear infinite;
	}

	.url-dialog-footer {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
		padding: 0.75rem 1.25rem;
		border-top: 1px solid hsl(var(--border) / 0.5);
	}

	.btn {
		padding: 0.45rem 0.9rem;
		font-size: 0.85rem;
		font-weight: 500;
		border: 1px solid hsl(var(--border));
		border-radius: 8px;
		background: transparent;
		color: hsl(var(--foreground));
		cursor: pointer;
		transition: all 0.15s ease;
	}

	.btn:hover:not(:disabled) {
		background: hsl(var(--muted) / 0.5);
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

	@keyframes fadeIn {
		from { opacity: 0; }
		to { opacity: 1; }
	}

	@keyframes scaleIn {
		from { transform: scale(0.96); opacity: 0; }
		to { transform: scale(1); opacity: 1; }
	}

	@keyframes spin {
		to { transform: rotate(360deg); }
	}
</style>
