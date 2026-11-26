<script lang="ts">
	import { createEventDispatcher } from 'svelte';
	import { goto } from '$app/navigation';
	import { t, getIntlLocale } from '$lib/i18n';
	import { getDiaryByDateResult } from '$lib/api/diaries';
	import type { Diary } from '$lib/api/client';
	import { sanitizeDiaryHtml } from '$lib/utils/chatMarkdown';

	export let date: string | null = null;

	const dispatch = createEventDispatcher<{ close: void }>();

	let loading = false;
	let diary: Diary | null = null;
	let status: 'idle' | 'found' | 'not_found' | 'error' = 'idle';
	let lastLoadedDate: string | null = null;

	$: if (date && date !== lastLoadedDate) {
		loadDiary(date);
	} else if (!date) {
		lastLoadedDate = null;
		diary = null;
		status = 'idle';
	}

	async function loadDiary(targetDate: string) {
		lastLoadedDate = targetDate;
		loading = true;
		diary = null;
		status = 'idle';
		const res = await getDiaryByDateResult(targetDate);
		if (lastLoadedDate !== targetDate) return;
		status = res.status;
		diary = res.diary;
		loading = false;
	}

	function formatHeading(dateStr: string): string {
		const parts = dateStr.split('-').map(Number);
		if (parts.length !== 3 || parts.some(Number.isNaN)) return dateStr;
		const d = new Date(parts[0], parts[1] - 1, parts[2]);
		return d.toLocaleDateString(getIntlLocale(), {
			year: 'numeric',
			month: 'long',
			day: 'numeric',
			weekday: 'short'
		});
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && date) {
			dispatch('close');
		}
	}

	function openFullDiary() {
		if (!date) return;
		const target = date;
		dispatch('close');
		goto(`/diary/${target}`);
	}
</script>

<svelte:window on:keydown={handleKeydown} />

{#if date}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<button
			type="button"
			class="fixed inset-0 bg-black/45 backdrop-blur-sm"
			on:click={() => dispatch('close')}
			aria-label={$t('assistant.closePreview')}
		></button>

		<div
			role="dialog"
			aria-modal="true"
			aria-label={$t('assistant.previewDiary')}
			class="relative z-10 w-full max-w-2xl max-h-[82dvh] flex flex-col rounded-2xl
				bg-card border border-border shadow-2xl overflow-hidden animate-fade-in"
		>
			<!-- Header -->
			<div class="flex items-center justify-between gap-3 px-5 py-3.5 border-b border-border/60">
				<div class="min-w-0">
					<div class="text-sm font-semibold text-foreground truncate">
						{formatHeading(date)}
					</div>
					{#if diary && (diary.mood || diary.weather)}
						<div class="flex items-center gap-2 mt-0.5 text-xs text-muted-foreground">
							{#if diary.mood}<span>{diary.mood}</span>{/if}
							{#if diary.mood && diary.weather}<span>·</span>{/if}
							{#if diary.weather}<span>{diary.weather}</span>{/if}
						</div>
					{/if}
				</div>

				<div class="flex items-center gap-2 flex-shrink-0">
					<button
						type="button"
						on:click={openFullDiary}
						class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium
							bg-primary text-primary-foreground hover:opacity-90 transition-opacity"
					>
						<span>{$t('assistant.openDiary')}</span>
						<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14 5l7 7m0 0l-7 7m7-7H3" />
						</svg>
					</button>
					<button
						type="button"
						on:click={() => dispatch('close')}
						class="p-1.5 rounded-lg hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
						aria-label={$t('assistant.closePreview')}
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
						</svg>
					</button>
				</div>
			</div>

			<!-- Body -->
			<div class="flex-1 overflow-y-auto px-5 py-4">
				{#if loading}
					<div class="flex items-center justify-center py-12">
						<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
						</svg>
					</div>
				{:else if status === 'found' && diary}
					<div class="diary-preview-prose text-sm leading-7 text-foreground/95">
						{@html sanitizeDiaryHtml(diary.content)}
					</div>
				{:else if status === 'not_found'}
					<div class="py-12 text-center text-sm text-muted-foreground">
						{$t('assistant.diaryNotFound', { date })}
					</div>
				{:else if status === 'error'}
					<div class="py-12 text-center text-sm text-red-500">
						{$t('assistant.diaryLoadError')}
					</div>
				{/if}
			</div>
		</div>
	</div>
{/if}

<style>
	.diary-preview-prose :global(p) {
		margin-bottom: 0.75rem;
	}
	.diary-preview-prose :global(p:last-child) {
		margin-bottom: 0;
	}
	.diary-preview-prose :global(h1),
	.diary-preview-prose :global(h2),
	.diary-preview-prose :global(h3) {
		font-weight: 600;
		margin-top: 1rem;
		margin-bottom: 0.5rem;
	}
	.diary-preview-prose :global(ul),
	.diary-preview-prose :global(ol) {
		margin-left: 1.25rem;
		margin-bottom: 0.75rem;
	}
	.diary-preview-prose :global(ul) {
		list-style-type: disc;
	}
	.diary-preview-prose :global(ol) {
		list-style-type: decimal;
	}
	.diary-preview-prose :global(img) {
		max-width: 100%;
		border-radius: 0.75rem;
		margin: 0.75rem 0;
	}
	.diary-preview-prose :global(blockquote) {
		border-left: 3px solid hsl(var(--primary) / 0.4);
		padding-left: 0.85rem;
		color: hsl(var(--muted-foreground));
		margin: 0.75rem 0;
	}
</style>
