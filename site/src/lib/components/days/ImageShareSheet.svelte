<script lang="ts">
	import { onDestroy } from 'svelte';
	import Sheet from './Sheet.svelte';
	import { downloadImage } from '$lib/utils/imageExport';
	import { t } from '$lib/i18n';

	/**
	 * Shows an image being made, then lets it be copied, downloaded or (where
	 * the device offers it) handed to the system share sheet. The image is
	 * ready before any button is pressed, so copying works in Safari too,
	 * which only allows clipboard writes while a click is being handled.
	 */
	let {
		title,
		image,
		filename,
		onclose
	}: { title: string; image: Promise<Blob>; filename: string; onclose: () => void } = $props();

	let blob = $state<Blob | null>(null);
	let url = $state('');
	let failed = $state(false);
	let done = $state<'' | 'copy' | 'download'>('');
	let error = $state('');
	let doneTimer: ReturnType<typeof setTimeout> | undefined;

	const canCopy = typeof navigator !== 'undefined' && !!navigator.clipboard?.write && typeof ClipboardItem !== 'undefined';
	const touch = typeof matchMedia !== 'undefined' && matchMedia('(pointer: coarse)').matches;
	let file = $derived(blob ? new File([blob], filename, { type: 'image/png' }) : null);
	let canShare = $derived(touch && !!file && typeof navigator.canShare === 'function' && navigator.canShare({ files: [file] }));

	$effect(() => {
		let live = true;
		blob = null;
		failed = false;
		image.then(
			(result) => {
				if (!live) return;
				blob = result;
				url = URL.createObjectURL(result);
			},
			(e) => {
				console.error('Failed to render the image:', e);
				if (live) failed = true;
			}
		);
		return () => {
			live = false;
		};
	});

	onDestroy(() => {
		if (url) URL.revokeObjectURL(url);
		clearTimeout(doneTimer);
	});

	function mark(what: 'copy' | 'download') {
		done = what;
		error = '';
		clearTimeout(doneTimer);
		doneTimer = setTimeout(() => (done = ''), 2000);
	}

	async function copy() {
		if (!blob) return;
		try {
			await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })]);
			mark('copy');
		} catch (e) {
			console.error('Failed to copy the image:', e);
			error = $t('days.imageSheet.copyFailed');
		}
	}

	function download() {
		if (!url) return;
		downloadImage(url, filename);
		mark('download');
	}

	async function share() {
		if (!file) return;
		try {
			await navigator.share({ files: [file], title });
		} catch (e) {
			if ((e as Error).name !== 'AbortError') error = $t('days.imageSheet.shareFailed');
		}
	}
</script>

<Sheet title={$t('days.imageSheet.title')} {onclose} wide>
	<div class="rounded-xl bg-muted/50 border border-border/50 p-2 sm:p-3 flex items-center justify-center min-h-48">
		{#if url}
			<img src={url} alt={title} class="block max-w-full max-h-[56vh] w-auto h-auto rounded-lg shadow-md" />
		{:else if failed}
			<p class="text-sm text-muted-foreground">{$t('days.exportFailed')}</p>
		{:else}
			<span class="inline-flex items-center gap-2 text-sm text-muted-foreground">
				<span class="w-4 h-4 rounded-full border-2 border-current border-t-transparent animate-spin" aria-hidden="true"></span>
				{$t('days.exporting')}
			</span>
		{/if}
	</div>
	{#if error}
		<p class="mt-3 text-xs text-center text-destructive">{error}</p>
	{:else if touch && url}
		<p class="mt-3 text-xs text-center text-muted-foreground">{$t('days.imageSheet.hint')}</p>
	{/if}

	{#snippet footer()}
		{#if canCopy}
			<button
				type="button"
				onclick={copy}
				disabled={!blob}
				class="flex-1 inline-flex items-center justify-center gap-1.5 h-10 rounded-xl border border-border/60 text-sm hover:bg-muted/50 disabled:opacity-50 transition-colors"
			>
				{#if done === 'copy'}
					<svg class="w-4 h-4 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7" /></svg>
					{$t('days.imageSheet.copied')}
				{:else}
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2" stroke-width="2" /><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 15H4a1 1 0 01-1-1V4a1 1 0 011-1h10a1 1 0 011 1v1" /></svg>
					{$t('days.imageSheet.copy')}
				{/if}
			</button>
		{/if}
		{#if canShare}
			<button
				type="button"
				onclick={share}
				class="flex-1 inline-flex items-center justify-center gap-1.5 h-10 rounded-xl border border-border/60 text-sm hover:bg-muted/50 transition-colors"
			>
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 12v7a1 1 0 001 1h14a1 1 0 001-1v-7M16 6l-4-4m0 0L8 6m4-4v13" /></svg>
				{$t('days.imageSheet.share')}
			</button>
		{/if}
		<button
			type="button"
			onclick={download}
			disabled={!blob}
			class="flex-1 inline-flex items-center justify-center gap-1.5 h-10 rounded-xl bg-primary text-primary-foreground text-sm hover:bg-primary/90 disabled:opacity-50 transition-colors"
		>
			{#if done === 'download'}
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7" /></svg>
				{$t('days.imageSheet.saved')}
			{:else}
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v2a2 2 0 002 2h12a2 2 0 002-2v-2M7 10l5 5m0 0l5-5m-5 5V4" /></svg>
				{$t('days.imageSheet.download')}
			{/if}
		</button>
	{/snippet}
</Sheet>
