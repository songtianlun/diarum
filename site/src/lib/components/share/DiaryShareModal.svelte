<script lang="ts">
	import { onMount } from 'svelte';
	import DiarySharePreview from './DiarySharePreview.svelte';
	import ShareOptionsPanel from './ShareOptionsPanel.svelte';
	import {
		defaultShareOptions,
		generateImage,
		downloadImage,
		copyImageToClipboard,
		shareImage,
		canShare,
		type ShareOptions
	} from '$lib/utils/imageExport';

	export let isOpen = false;
	export let date: string;
	export let content: string;
	export let selectedContent: string = '';
	export let mood: string = '';
	export let weather: string = '';
	export let tags: string[] = [];
	export let onClose: () => void = () => {};

	$: effectiveContent = selectedContent || content;

	let options: ShareOptions = { ...defaultShareOptions };
	let previewElement: HTMLElement;
	let isGenerating = false;
	let error = '';
	let showOptions = false;
	let canUseShare = false;

	onMount(() => {
		canUseShare = canShare();
	});

	function handleClose() {
		isOpen = false;
		onClose();
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			handleClose();
		}
	}

	function handleOptionsChange(newOptions: ShareOptions) {
		options = newOptions;
	}

	async function handleDownload() {
		if (!previewElement || isGenerating) return;

		isGenerating = true;
		error = '';

		try {
			const dataUrl = await generateImage(previewElement, options);
			const filename = `diary-${date}.png`;
			downloadImage(dataUrl, filename);
		} catch (e) {
			error = 'Failed to generate image, please try again';
			console.error('Failed to generate image:', e);
		} finally {
			isGenerating = false;
		}
	}

	async function handleCopy() {
		if (!previewElement || isGenerating) return;

		isGenerating = true;
		error = '';

		try {
			const dataUrl = await generateImage(previewElement, options);
			const success = await copyImageToClipboard(dataUrl);
			if (!success) {
				error = 'Copy failed, please try download';
			}
		} catch (e) {
			error = 'Failed to generate image, please try again';
			console.error('Failed to copy image:', e);
		} finally {
			isGenerating = false;
		}
	}

	async function handleShare() {
		if (!previewElement || isGenerating || !canUseShare) return;

		isGenerating = true;
		error = '';

		try {
			const dataUrl = await generateImage(previewElement, options);
			const success = await shareImage(dataUrl, `diary-${date}`);
			if (!success) {
				error = 'Share failed, please try download';
			}
		} catch (e) {
			error = 'Failed to generate image, please try again';
			console.error('Failed to share image:', e);
		} finally {
			isGenerating = false;
		}
	}
</script>

<svelte:window on:keydown={handleKeydown} />

{#if isOpen}
	<!-- Backdrop -->
	<div
		class="fixed inset-0 bg-black/50 z-50 animate-fade-in-only"
		on:click={handleClose}
		on:keydown={(e) => e.key === 'Enter' && handleClose()}
		role="button"
		tabindex="0"
	></div>

	<!-- Modal -->
	<div class="fixed inset-4 md:inset-8 lg:inset-12 z-50 flex items-center justify-center pointer-events-none">
		<div
			class="bg-card rounded-xl shadow-2xl border border-border/50 w-full max-w-5xl max-h-full overflow-hidden flex flex-col pointer-events-auto animate-fade-in"
		>
			<!-- Header -->
			<div class="flex items-center justify-between px-4 py-3 border-b border-border/50">
				<h2 class="text-lg font-semibold text-foreground">Share Diary</h2>
				<div class="flex items-center gap-2">
					<button
						on:click={() => showOptions = !showOptions}
						class="p-2 hover:bg-muted/50 rounded-lg transition-colors {showOptions ? 'bg-muted/50' : ''}"
						title="Settings"
					>
						<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
						</svg>
					</button>
					<button
						on:click={handleClose}
						class="p-2 hover:bg-muted/50 rounded-lg transition-colors"
						title="Close"
					>
						<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
						</svg>
					</button>
				</div>
			</div>

			<!-- Content -->
			<div class="flex-1 overflow-hidden flex">
				<!-- Preview Area -->
				<div class="flex-1 overflow-auto p-4 bg-muted/30">
					<div class="flex justify-center">
						<div
							bind:this={previewElement}
							class="shadow-lg rounded-lg overflow-hidden"
							style="max-width: 100%;"
						>
							<DiarySharePreview
								{date}
								content={effectiveContent}
								{options}
								{mood}
								{weather}
								{tags}
							/>
						</div>
					</div>
				</div>

				<!-- Options Panel (collapsible) -->
				{#if showOptions}
					<div class="w-72 border-l border-border/50 overflow-auto p-4 bg-card animate-slide-in-right">
						<ShareOptionsPanel {options} onChange={handleOptionsChange} />
					</div>
				{/if}
			</div>

			<!-- Footer -->
			<div class="px-4 py-3 border-t border-border/50 flex items-center justify-between">
				{#if error}
					<div class="text-sm text-red-500">{error}</div>
				{:else if selectedContent}
					<div class="text-sm text-primary flex items-center gap-1.5">
						<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
						</svg>
						Exporting selected text
					</div>
				{:else}
					<div class="text-sm text-muted-foreground">
						Preview: {options.width}px × {options.scale}x
					</div>
				{/if}

				<div class="flex items-center gap-2">
					{#if canUseShare}
						<button
							on:click={handleShare}
							disabled={isGenerating}
							class="px-4 py-2 text-sm bg-secondary text-secondary-foreground rounded-lg hover:opacity-90 transition-opacity disabled:opacity-50 flex items-center gap-2"
						>
							{#if isGenerating}
								<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
								</svg>
							{:else}
								<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" />
								</svg>
							{/if}
							Share
						</button>
					{/if}

					<button
						on:click={handleCopy}
						disabled={isGenerating}
						class="px-4 py-2 text-sm bg-secondary text-secondary-foreground rounded-lg hover:opacity-90 transition-opacity disabled:opacity-50 flex items-center gap-2"
					>
						{#if isGenerating}
							<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
							</svg>
						{:else}
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3" />
							</svg>
						{/if}
						Copy
					</button>

					<button
						on:click={handleDownload}
						disabled={isGenerating}
						class="px-4 py-2 text-sm bg-primary text-primary-foreground rounded-lg hover:opacity-90 transition-opacity disabled:opacity-50 flex items-center gap-2"
					>
						{#if isGenerating}
							<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
							</svg>
						{:else}
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
							</svg>
						{/if}
						Download
					</button>
				</div>
			</div>
		</div>
	</div>
{/if}
