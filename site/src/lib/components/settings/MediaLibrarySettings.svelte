<script lang="ts">
	/**
	 * Media library housekeeping (Settings > Image Upload, for local and S3
	 * storage): statistics, trash retention, the unused image scan and
	 * automatic cleaning of unused images.
	 */
	import { onMount } from 'svelte';
	import {
		cleanUnlinkedMedia,
		getMediaLibrarySettings,
		getMediaStats,
		getMediaFileUrl,
		saveMediaLibrarySettings,
		scanUnlinkedMedia,
		type MediaLibrarySettings,
		type MediaLibraryStats,
		type MediaWithDiary
	} from '$lib/api/media';
	import { variantUrl } from '$lib/utils/imageDisplay';
	import { t } from '$lib/i18n';

	/** The saved storage provider: local or s3. */
	export let provider: string;

	const PRESETS = [7, 30, 90, 365];
	const PREVIEW_COUNT = 12;

	let stats: MediaLibraryStats | null = null;
	let statsLoading = false;
	let statsError = '';

	let settings: MediaLibrarySettings = { trash_retention_days: 30, auto_clean_unlinked: false };
	let original = JSON.stringify(settings);
	let customDays = '';
	let saving = false;
	let saveMessage: { ok: boolean; text: string } | null = null;

	let scanning = false;
	let scanned: MediaWithDiary[] | null = null;
	let scanError = '';
	let cleaning = false;
	let cleanMessage: { ok: boolean; text: string } | null = null;

	$: providerLabel = provider === 's3' ? 'S3' : 'Local';
	$: isPreset = settings.trash_retention_days === 0 || PRESETS.includes(settings.trash_retention_days);
	$: changed = JSON.stringify(settings) !== original;
	$: otherStorage = stats
		? Object.entries(stats.stats.byStorage)
				.filter(([key, count]) => key !== stats?.provider && count > 0)
				.map(([key, count]) => `${key === 's3' ? 'S3' : key === 'local' ? 'Local' : key} ${count}`)
				.join(' · ')
		: '';

	async function loadStats() {
		statsLoading = true;
		statsError = '';
		try {
			stats = await getMediaStats();
		} catch (error) {
			statsError = error instanceof Error ? error.message : $t('mediaLib.settings.stats.failed');
		} finally {
			statsLoading = false;
		}
	}

	async function loadSettings() {
		try {
			settings = await getMediaLibrarySettings();
			original = JSON.stringify(settings);
			customDays = isPresetValue(settings.trash_retention_days) ? '' : String(settings.trash_retention_days);
		} catch {
			// Keep the defaults; saving will surface any real problem.
		}
	}

	function isPresetValue(days: number) {
		return days === 0 || PRESETS.includes(days);
	}

	function pick(days: number) {
		settings = { ...settings, trash_retention_days: days };
		customDays = '';
	}

	function onCustomInput() {
		const days = Number(customDays);
		if (customDays !== '' && Number.isInteger(days) && days >= 1 && days <= 3650) {
			settings = { ...settings, trash_retention_days: days };
		}
	}

	async function save() {
		saveMessage = null;
		if (customDays !== '') {
			const days = Number(customDays);
			if (!Number.isInteger(days) || days < 1 || days > 3650) {
				saveMessage = { ok: false, text: $t('mediaLib.settings.invalidDays') };
				return;
			}
		}
		saving = true;
		try {
			settings = await saveMediaLibrarySettings(settings);
			original = JSON.stringify(settings);
			saveMessage = { ok: true, text: $t('mediaLib.settings.saved') };
			setTimeout(() => (saveMessage = null), 2500);
		} catch (error) {
			saveMessage = { ok: false, text: error instanceof Error ? error.message : $t('mediaLib.settings.saveFailed') };
		} finally {
			saving = false;
		}
	}

	async function scan() {
		scanning = true;
		scanError = '';
		cleanMessage = null;
		try {
			scanned = (await scanUnlinkedMedia()).items;
		} catch (error) {
			scanError = error instanceof Error ? error.message : 'Scan failed';
			scanned = null;
		} finally {
			scanning = false;
		}
	}

	async function clean() {
		if (!scanned?.length) return;
		cleaning = true;
		cleanMessage = null;
		try {
			const result = await cleanUnlinkedMedia(scanned.map((media) => media.id!).filter(Boolean));
			const kept = Object.keys(result.failed ?? {}).length;
			cleanMessage = {
				ok: true,
				text: $t('mediaLib.settings.cleaned', { count: result.done.length }) + (kept ? ` · ${$t('mediaLib.settings.cleanPartial', { count: kept })}` : '')
			};
			scanned = null;
			await loadStats();
		} catch (error) {
			cleanMessage = { ok: false, text: error instanceof Error ? error.message : 'Failed' };
		} finally {
			cleaning = false;
		}
	}

	function thumbOf(media: MediaWithDiary) {
		const src = getMediaFileUrl(media);
		return variantUrl(src, 'th') ?? src;
	}

	onMount(() => {
		loadStats();
		loadSettings();
	});
</script>

<div id="media-library" class="mt-6 bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6 animate-fade-in scroll-mt-16">
	<div class="flex items-start justify-between gap-3 mb-1">
		<h2 class="text-lg font-semibold text-foreground">{$t('mediaLib.settings.title')}</h2>
		<button
			class="p-1.5 -mr-1 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors disabled:opacity-50"
			on:click={loadStats}
			disabled={statsLoading}
			title={$t('mediaLib.settings.stats.refresh')}
			aria-label={$t('mediaLib.settings.stats.refresh')}
		>
			<svg class="w-4 h-4 {statsLoading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
			</svg>
		</button>
	</div>
	<p class="text-sm text-muted-foreground mb-5">{$t('mediaLib.settings.description', { provider: providerLabel })}</p>

	<!-- Statistics -->
	{#if statsError}
		<div class="mb-4 p-3 rounded-lg text-sm bg-rose-500/10 text-rose-600 dark:text-rose-400">{statsError}</div>
	{/if}
	<div class="grid grid-cols-2 sm:grid-cols-4 gap-2 sm:gap-3">
		{#each [
			{ key: 'current', label: $t('mediaLib.settings.stats.current', { provider: providerLabel }), value: stats?.current, accent: true },
			{ key: 'total', label: $t('mediaLib.settings.stats.total'), value: stats?.stats.total },
			{ key: 'linked', label: $t('mediaLib.settings.stats.linked'), value: stats?.stats.linked },
			{ key: 'trash', label: $t('mediaLib.settings.stats.trash'), value: stats?.stats.trash, href: '/media/trash' }
		] as card (card.key)}
			<svelte:element
				this={card.href ? 'a' : 'div'}
				href={card.href}
				class="rounded-xl border px-3 py-3 {card.accent ? 'border-primary/30 bg-primary/5' : 'border-border/50 bg-muted/30'} {card.href ? 'hover:border-primary/40 transition-colors' : ''}"
			>
				<div class="text-2xl font-semibold tabular-nums text-foreground">
					{#if stats}{(card.value ?? 0).toLocaleString()}{:else}<span class="inline-block w-10 h-6 rounded bg-muted animate-pulse align-middle"></span>{/if}
				</div>
				<div class="text-xs text-muted-foreground mt-0.5 truncate">{card.label}</div>
			</svelte:element>
		{/each}
	</div>
	{#if otherStorage}
		<p class="text-xs text-muted-foreground mt-2">{$t('mediaLib.settings.stats.other', { detail: otherStorage })}</p>
	{/if}
	{#if stats?.stats.external}
		<p class="text-xs text-muted-foreground mt-2">{$t('mediaLib.settings.stats.external', { count: stats.stats.external })}</p>
	{/if}

	<!-- Trash retention -->
	<div class="pt-6 mt-6 border-t border-border/50">
		<div class="flex items-center justify-between gap-3 mb-1">
			<div class="font-medium text-foreground">{$t('mediaLib.settings.trashHeading')}</div>
			<a href="/media/trash" class="text-sm text-primary hover:underline whitespace-nowrap">{$t('mediaLib.settings.openTrash')} →</a>
		</div>
		<p class="text-sm text-muted-foreground mb-3">{$t('mediaLib.settings.trashDesc')}</p>
		<div class="flex flex-wrap gap-2" role="radiogroup" aria-label={$t('mediaLib.settings.trashHeading')}>
			{#each PRESETS as days}
				<button
					type="button"
					role="radio"
					aria-checked={settings.trash_retention_days === days && customDays === ''}
					class="chip {settings.trash_retention_days === days && customDays === '' ? 'active' : ''}"
					on:click={() => pick(days)}
				>{$t('mediaLib.settings.days', { days })}</button>
			{/each}
			<button
				type="button"
				role="radio"
				aria-checked={settings.trash_retention_days === 0}
				class="chip {settings.trash_retention_days === 0 ? 'active' : ''}"
				on:click={() => pick(0)}
			>{$t('mediaLib.settings.never')}</button>
			<label class="chip custom {customDays !== '' || !isPreset ? 'active' : ''}">
				<span class="text-muted-foreground">{$t('mediaLib.settings.customDays')}</span>
				<input
					type="number"
					min="1"
					max="3650"
					inputmode="numeric"
					bind:value={customDays}
					on:input={onCustomInput}
					placeholder="—"
					class="w-14 bg-transparent text-right tabular-nums focus:outline-none"
				/>
				<span class="text-muted-foreground">{$t('mediaLib.settings.daysUnit')}</span>
			</label>
		</div>
	</div>

	<!-- Unused images -->
	<div class="pt-6 mt-6 border-t border-border/50">
		<div class="font-medium text-foreground mb-1">{$t('mediaLib.settings.unlinkedHeading')}</div>
		<p class="text-sm text-muted-foreground mb-3">{$t('mediaLib.settings.unlinkedDesc')}</p>

		<div class="rounded-xl bg-muted/40 p-3 sm:p-4 space-y-3">
			<div class="flex flex-wrap items-center gap-3">
				<button
					class="px-4 py-2 text-sm bg-background hover:bg-background/80 border border-border/60 rounded-lg transition-colors disabled:opacity-50 flex items-center gap-2"
					on:click={scan}
					disabled={scanning || cleaning}
				>
					{#if scanning}
						<span class="w-4 h-4 rounded-full border-2 border-muted-foreground/30 border-t-primary animate-spin"></span>
						{$t('mediaLib.settings.scanning')}
					{:else}
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
						{$t('mediaLib.settings.scan')}
					{/if}
				</button>
				<span class="text-xs text-muted-foreground">{$t('mediaLib.settings.scanNote')}</span>
			</div>

			{#if scanError}
				<div class="text-sm text-rose-600 dark:text-rose-400">{scanError}</div>
			{/if}
			{#if cleanMessage}
				<div class="text-sm {cleanMessage.ok ? 'text-green-600' : 'text-rose-600 dark:text-rose-400'}">{cleanMessage.text}</div>
			{/if}

			{#if scanned}
				{#if scanned.length === 0}
					<div class="text-sm text-muted-foreground flex items-center gap-1.5">
						<svg class="w-4 h-4 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" /></svg>
						{$t('mediaLib.settings.scanNone')}
					</div>
				{:else}
					<div class="text-sm font-medium text-foreground">{$t('mediaLib.settings.scanFound', { count: scanned.length })}</div>
					<div class="grid grid-cols-6 sm:grid-cols-8 md:grid-cols-12 gap-1.5">
						{#each scanned.slice(0, PREVIEW_COUNT) as media (media.id)}
							<img src={thumbOf(media)} alt={media.name || ''} loading="lazy" class="aspect-square w-full object-cover rounded-md bg-muted border border-border/40" />
						{/each}
						{#if scanned.length > PREVIEW_COUNT}
							<div class="aspect-square rounded-md bg-muted flex items-center justify-center text-xs text-muted-foreground tabular-nums">+{scanned.length - PREVIEW_COUNT}</div>
						{/if}
					</div>
					<button
						class="px-4 py-2 text-sm rounded-lg bg-rose-600 text-white hover:bg-rose-700 transition-colors disabled:opacity-50 flex items-center gap-2"
						on:click={clean}
						disabled={cleaning}
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>
						{cleaning ? $t('mediaLib.settings.cleaning') : $t('mediaLib.settings.clean')}
					</button>
				{/if}
			{/if}
		</div>

		<label class="mt-4 flex items-start justify-between gap-4 cursor-pointer">
			<span>
				<span class="block text-sm font-medium text-foreground">{$t('mediaLib.settings.autoClean')}</span>
				<span class="block text-xs text-muted-foreground mt-0.5">{$t('mediaLib.settings.autoCleanDesc')}</span>
			</span>
			<span class="relative inline-flex flex-shrink-0 mt-0.5">
				<input type="checkbox" class="peer sr-only" bind:checked={settings.auto_clean_unlinked} />
				<span class="w-10 h-6 rounded-full bg-muted-foreground/30 peer-checked:bg-primary transition-colors peer-focus-visible:ring-2 peer-focus-visible:ring-primary/50"></span>
				<span class="absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform peer-checked:translate-x-4"></span>
			</span>
		</label>
	</div>

	<!-- Save -->
	<div class="pt-5 mt-5 border-t border-border/50 flex flex-wrap items-center gap-3">
		<button
			on:click={save}
			disabled={saving || !changed}
			class="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
		>
			{saving ? $t('mediaLib.settings.saving') : $t('mediaLib.settings.save')}
		</button>
		{#if saveMessage}
			<span class="text-sm animate-fade-in {saveMessage.ok ? 'text-green-600' : 'text-rose-600 dark:text-rose-400'}">{saveMessage.text}</span>
		{/if}
	</div>
</div>

<style>
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		padding: 0.375rem 0.75rem;
		border-radius: 999px;
		border: 1px solid hsl(var(--border));
		font-size: 0.8125rem;
		color: hsl(var(--foreground));
		transition: border-color 0.15s ease, background-color 0.15s ease;
		cursor: pointer;
	}
	.chip:hover {
		border-color: hsl(var(--primary) / 0.5);
	}
	.chip.active {
		border-color: hsl(var(--primary));
		background: hsl(var(--primary) / 0.08);
		color: hsl(var(--primary));
	}
	.chip.custom input::-webkit-outer-spin-button,
	.chip.custom input::-webkit-inner-spin-button {
		-webkit-appearance: none;
		margin: 0;
	}
	.chip.custom input {
		-moz-appearance: textfield;
		appearance: textfield;
		color: hsl(var(--foreground));
	}
</style>
