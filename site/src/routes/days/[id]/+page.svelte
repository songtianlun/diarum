<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { toPng } from 'html-to-image';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import Blackboard from '$lib/components/days/Blackboard.svelte';
	import DayFormModal from '$lib/components/days/DayFormModal.svelte';
	import { getDay, deleteDay, type Day } from '$lib/api/days';
	import { isAuthenticated } from '$lib/api/client';
	import { downloadImage, copyImageToClipboard, shareImage, canShare } from '$lib/utils/imageExport';
	import { t } from '$lib/i18n';

	let id = $derived($page.params.id ?? '');
	let day = $state<Day | null>(null);
	let loading = $state(true);
	let missing = $state(false);
	let editing = $state(false);
	let board: HTMLDivElement | undefined = $state();
	let busy: '' | 'share' | 'export' = $state('');
	let notice = $state('');
	let noticeTimer: ReturnType<typeof setTimeout> | undefined;

	async function load() {
		loading = true;
		missing = false;
		try {
			day = (await getDay(id)).day;
		} catch {
			missing = true;
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		load();
		return () => clearTimeout(noticeTimer);
	});

	function flash(message: string) {
		notice = message;
		clearTimeout(noticeTimer);
		noticeTimer = setTimeout(() => (notice = ''), 2400);
	}

	async function render(): Promise<string | null> {
		if (!board) return null;
		try {
			// Fonts must be ready or the chalk falls back mid-render.
			await document.fonts?.ready;
			return await toPng(board, { pixelRatio: 2, cacheBust: false });
		} catch (e) {
			console.error('Failed to render day card:', e);
			flash($t('days.exportFailed'));
			return null;
		}
	}

	async function exportImage() {
		if (!day || busy) return;
		busy = 'export';
		const url = await render();
		if (url) downloadImage(url, `${day.name}.png`);
		busy = '';
	}

	async function share() {
		if (!day || busy) return;
		busy = 'share';
		const url = await render();
		if (url) {
			const shared = canShare() && (await shareImage(url, day.name));
			if (!shared && (await copyImageToClipboard(url))) flash($t('days.copied'));
			else if (!shared) downloadImage(url, `${day.name}.png`);
		}
		busy = '';
	}

	async function remove() {
		if (!day || !confirm($t('days.deleteConfirm', { name: day.name }))) return;
		await deleteDay(day.id);
		goto('/days');
	}
</script>

<svelte:head>
	<title>{day?.name ?? $t('days.title')} - Diarum</title>
</svelte:head>

<div class="min-h-screen bg-background flex flex-col">
	<PageHeader title={day?.name ?? $t('days.title')} />

	<main class="flex-1 w-full max-w-[400px] mx-auto px-4 py-5 sm:py-8">
		<a href="/days" class="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors mb-4">
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" /></svg>
			{$t('days.back')}
		</a>

		{#if loading}
			<div class="aspect-[4/5] rounded-3xl bg-muted/40 animate-pulse"></div>
		{:else if missing || !day}
			<div class="py-16 text-center text-sm text-muted-foreground">{$t('days.notFound')}</div>
		{:else}
			<div class="animate-fade-in-only">
				<Blackboard {day} bind:element={board} />

				<div class="mt-5 grid grid-cols-2 gap-2">
					<a
						href="/days/{day.id}/milestones"
						class="col-span-2 inline-flex items-center justify-center gap-2 py-2.5 rounded-xl bg-primary text-primary-foreground text-sm font-medium shadow-sm hover:opacity-90 transition-opacity"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 21V4m0 0h11l-1.5 4L15 12H4" /></svg>
						{$t('days.milestones.open')}
						{#if day.milestones > 0}<span class="px-1.5 rounded-md bg-primary-foreground/20 text-xs tabular-nums">{day.milestones}</span>{/if}
					</a>
					<button
						type="button"
						onclick={share}
						disabled={!!busy}
						class="inline-flex items-center justify-center gap-1.5 py-2.5 rounded-xl bg-card border border-border/60 text-sm hover:bg-muted/50 disabled:opacity-60 transition-colors"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" /></svg>
						{busy === 'share' ? $t('days.exporting') : $t('days.share')}
					</button>
					<button
						type="button"
						onclick={exportImage}
						disabled={!!busy}
						class="inline-flex items-center justify-center gap-1.5 py-2.5 rounded-xl bg-card border border-border/60 text-sm hover:bg-muted/50 disabled:opacity-60 transition-colors"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v2a2 2 0 002 2h12a2 2 0 002-2v-2M7 10l5 5m0 0l5-5m-5 5V4" /></svg>
						{busy === 'export' ? $t('days.exporting') : $t('days.export')}
					</button>
				</div>

				<div class="mt-3 flex items-center justify-center gap-1 text-sm">
					<button type="button" onclick={() => (editing = true)} class="px-3 py-1.5 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors">{$t('days.edit')}</button>
					<span class="text-border">·</span>
					<button type="button" onclick={remove} class="px-3 py-1.5 rounded-lg text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors">{$t('days.delete')}</button>
				</div>
			</div>
		{/if}
	</main>

	<Footer maxWidth="6xl" />
</div>

{#if notice}
	<div class="fixed left-1/2 -translate-x-1/2 bottom-6 z-50 px-4 py-2 rounded-full bg-foreground text-background text-sm shadow-lg animate-fade-in">{notice}</div>
{/if}

{#if editing && day}
	<DayFormModal
		{day}
		onclose={() => (editing = false)}
		onsaved={(saved) => {
			editing = false;
			day = saved;
		}}
	/>
{/if}
