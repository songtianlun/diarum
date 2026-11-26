<script lang="ts">
	/**
	 * Admin → Holidays: global switch for China mainland statutory holidays &
	 * lieu workdays, official policy sync, and custom manual date overrides.
	 */
	import { onMount } from 'svelte';
	import {
		getAdminCNHolidays,
		getAdminCNHolidayYear,
		setAdminCNHolidaysEnabled,
		refreshAdminCNHolidays,
		importAdminCNHolidayJSON,
		upsertAdminCNHolidayOverride,
		deleteAdminCNHolidayOverride,
		type AdminCNHolidayOverview,
		type CNHolidayDay,
		type CNHolidayYearResponse
	} from '$lib/api/holidays';
	import {
		invalidateHolidayCache,
		setCNHolidaysGlobalEnabledLocal
	} from '$lib/stores/holidays';
	import { getIntlLocale, t } from '$lib/i18n';

	let overview = $state<AdminCNHolidayOverview | null>(null);
	let loading = $state(true);
	let error = $state('');
	let toggling = $state(false);
	let refreshing = $state(false);
	let importing = $state(false);
	let importInput = $state<HTMLInputElement | null>(null);
	let message = $state<{ ok: boolean; text: string } | null>(null);

	// Manual override form
	let formDate = $state('');
	let formName = $state('');
	let formIsOffDay = $state(true);
	let savingOverride = $state(false);
	let deletingDate = $state('');

	// Year schedule preview
	const currentYear = new Date().getFullYear();
	let previewYear = $state(currentYear);
	let previewData = $state<CNHolidayYearResponse | null>(null);
	let previewLoading = $state(false);

	let yearOptions = $derived.by(() => {
		const set = new Set<number>([currentYear, currentYear + 1]);
		for (const y of overview?.years ?? []) {
			if (y.published) set.add(y.year);
		}
		for (const ov of overview?.overrides ?? []) {
			const y = Number(ov.date.slice(0, 4));
			if (Number.isInteger(y)) set.add(y);
		}
		return Array.from(set).sort((a, b) => b - a);
	});

	let coveredRange = $derived.by(() => {
		const published = (overview?.years ?? []).filter((y) => y.published).map((y) => y.year);
		if (published.length === 0) return '';
		const min = published[0];
		const max = published[published.length - 1];
		return min === max ? `${min}` : `${min}–${max}`;
	});

	onMount(() => {
		void loadAll();
	});

	function flash(ok: boolean, text: string) {
		message = { ok, text };
		setTimeout(() => {
			if (message?.text === text) message = null;
		}, ok ? 4000 : 8000);
	}

	function formatTime(iso?: string): string {
		if (!iso) return '';
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return d.toLocaleString(getIntlLocale(), {
			year: 'numeric',
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	async function loadAll() {
		loading = true;
		error = '';
		try {
			overview = await getAdminCNHolidays();
			setCNHolidaysGlobalEnabledLocal(overview.enabled);
			await loadPreview(previewYear);
		} catch (e) {
			error = e instanceof Error ? e.message : $t('admin.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function loadPreview(year: number) {
		previewLoading = true;
		try {
			previewData = await getAdminCNHolidayYear(year);
		} catch {
			previewData = null;
		} finally {
			previewLoading = false;
		}
	}

	async function handleToggle() {
		if (!overview || toggling) return;
		const next = !overview.enabled;
		toggling = true;
		try {
			await setAdminCNHolidaysEnabled(next);
			overview = { ...overview, enabled: next };
			setCNHolidaysGlobalEnabledLocal(next);
			flash(true, next ? $t('admin.holidays.enabledSaved') : $t('admin.holidays.disabledSaved'));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.holidays.toggleFailed'));
		} finally {
			toggling = false;
		}
	}

	async function handleRefresh() {
		if (refreshing) return;
		refreshing = true;
		try {
			overview = await refreshAdminCNHolidays();
			invalidateHolidayCache();
			await loadPreview(previewYear);
			flash(true, $t('admin.holidays.refreshSuccess'));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.holidays.refreshFailed'));
		} finally {
			refreshing = false;
		}
	}

	async function handleImportFiles(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const files = Array.from(input.files ?? []);
		input.value = '';
		if (files.length === 0 || importing) return;

		importing = true;
		try {
			const importedYears: number[] = [];
			let lastDaysCount = 0;
			let latestOverview: AdminCNHolidayOverview | null = null;
			for (const file of files) {
				if (file.size > 512 * 1024) {
					throw new Error($t('admin.holidays.importTooLarge', { name: file.name }));
				}
				const res = await importAdminCNHolidayJSON(file);
				importedYears.push(res.imported.year);
				lastDaysCount = res.imported.days.length;
				latestOverview = res.overview;
			}
			if (latestOverview) {
				overview = latestOverview;
			}
			invalidateHolidayCache();
			const targetYear = Math.max(...importedYears);
			previewYear = targetYear;
			await loadPreview(targetYear);
			if (importedYears.length === 1) {
				flash(true, $t('admin.holidays.importSuccessSingle', { year: importedYears[0], count: lastDaysCount }));
			} else {
				flash(true, $t('admin.holidays.importSuccessMulti', { years: importedYears.sort((a, b) => a - b).join(', ') }));
			}
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.holidays.importFailed'));
		} finally {
			importing = false;
		}
	}

	function editDay(day: CNHolidayDay) {
		formDate = day.date;
		formName = day.name;
		formIsOffDay = day.isOffDay;
	}

	async function handleSaveOverride(event: SubmitEvent) {
		event.preventDefault();
		const date = formDate.trim();
		const name = formName.trim();
		if (!date || !name || savingOverride) return;

		savingOverride = true;
		try {
			const saved = await upsertAdminCNHolidayOverride({ date, name, isOffDay: formIsOffDay });
			if (overview) {
				const filtered = overview.overrides.filter((item) => item.date !== saved.date);
				const nextOverrides = [...filtered, saved].sort((a, b) => a.date.localeCompare(b.date));
				overview = { ...overview, overrides: nextOverrides };
			}
			invalidateHolidayCache();
			const savedYear = Number(saved.date.slice(0, 4));
			if (savedYear === previewYear) {
				await loadPreview(previewYear);
			}
			formDate = '';
			formName = '';
			formIsOffDay = true;
			flash(true, $t('admin.holidays.overrideSaved', { date: saved.date }));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.holidays.overrideSaveFailed'));
		} finally {
			savingOverride = false;
		}
	}

	async function handleDeleteOverride(date: string) {
		if (deletingDate) return;
		deletingDate = date;
		try {
			await deleteAdminCNHolidayOverride(date);
			if (overview) {
				overview = {
					...overview,
					overrides: overview.overrides.filter((item) => item.date !== date)
				};
			}
			invalidateHolidayCache();
			if (Number(date.slice(0, 4)) === previewYear) {
				await loadPreview(previewYear);
			}
			flash(true, $t('admin.holidays.overrideDeleted', { date }));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.holidays.overrideDeleteFailed'));
		} finally {
			deletingDate = '';
		}
	}

	function handlePreviewYearChange(event: Event) {
		const nextYear = Number((event.currentTarget as HTMLSelectElement).value);
		if (Number.isInteger(nextYear)) {
			previewYear = nextYear;
			void loadPreview(nextYear);
		}
	}
</script>

<div class="space-y-4">
	{#if message}
		<div
			class="sticky top-12 z-10 p-3 rounded-lg text-sm shadow-sm animate-fade-in break-words {message.ok
				? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border border-emerald-500/20'
				: 'bg-destructive/10 text-destructive border border-destructive/20'}"
		>
			{message.text}
		</div>
	{/if}

	{#if error}
		<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm flex items-center justify-between gap-3">
			<span>{error}</span>
			<button onclick={loadAll} class="underline">{$t('admin.retry')}</button>
		</div>
	{:else if loading && !overview}
		<div class="space-y-3">
			{#each Array(3) as _}
				<div class="h-32 rounded-xl bg-muted/40 animate-pulse"></div>
			{/each}
		</div>
	{:else if overview}
		<!-- Card 1: Global toggle & official policy sync -->
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6 animate-fade-in">
			<div class="flex items-start justify-between gap-3">
				<div class="min-w-0">
					<h2 class="text-lg font-semibold text-foreground flex flex-wrap items-center gap-2">
						{$t('admin.holidays.heading')}
						<span
							class="px-2 py-0.5 rounded-full text-[11px] font-medium ring-1 ring-inset {overview.enabled
								? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 ring-emerald-500/20'
								: 'bg-muted text-muted-foreground ring-border'}"
						>
							{overview.enabled ? $t('admin.holidays.statusOn') : $t('admin.holidays.statusOff')}
						</span>
					</h2>
					<p class="text-sm text-muted-foreground mt-1">{$t('admin.holidays.description')}</p>
				</div>
				<button
					onclick={loadAll}
					disabled={loading}
					title={$t('admin.refresh')}
					aria-label={$t('admin.refresh')}
					class="flex-shrink-0 p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50"
				>
					<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
					</svg>
				</button>
			</div>

			<!-- Global Switch -->
			<div class="mt-5 pt-4 border-t border-border/50 flex items-center justify-between gap-4">
				<div class="min-w-0">
					<div class="text-sm font-medium text-foreground">{$t('admin.holidays.globalSwitch')}</div>
					<p class="text-xs text-muted-foreground mt-0.5">{$t('admin.holidays.globalSwitchDesc')}</p>
				</div>
				<button
					type="button"
					role="switch"
					aria-checked={overview.enabled}
					aria-label={$t('admin.holidays.globalSwitch')}
					disabled={toggling}
					onclick={handleToggle}
					class="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-primary/40 disabled:opacity-50 {overview.enabled ? 'bg-primary' : 'bg-muted-foreground/30'}"
				>
					<span
						class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition duration-200 {overview.enabled ? 'translate-x-5' : 'translate-x-0'}"
					></span>
				</button>
			</div>

			<!-- Official Policy Sync -->
			<div class="mt-4 pt-4 border-t border-border/50 flex flex-wrap items-center justify-between gap-3">
				<div class="text-xs text-muted-foreground space-y-1">
					<div class="flex flex-wrap items-center gap-2">
						{#if coveredRange}
							<span class="text-foreground font-medium">{$t('admin.holidays.coveredYears', { range: coveredRange })}</span>
							<span>·</span>
						{/if}
						{#if overview.last_fetched_at}
							<span>{$t('admin.holidays.lastSynced', { time: formatTime(overview.last_fetched_at) })}</span>
						{:else}
							<span>{$t('admin.holidays.builtInBaseline')}</span>
						{/if}
					</div>
					<div>{$t('admin.holidays.syncNote')}</div>
				</div>
				<button
					type="button"
					onclick={handleRefresh}
					disabled={refreshing}
					class="px-3 py-1.5 text-sm rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
				>
					{refreshing ? $t('admin.holidays.refreshing') : $t('admin.holidays.refreshButton')}
				</button>
			</div>

			<!-- Offline JSON Import -->
			<div class="mt-4 rounded-lg bg-muted/40 border border-border/50 p-3.5 flex flex-wrap items-center justify-between gap-3">
				<div class="min-w-0 flex-1 text-xs text-muted-foreground space-y-1">
					<div class="font-medium text-foreground">{$t('admin.holidays.offlineTitle')}</div>
					<p class="leading-relaxed">
						{$t('admin.holidays.offlineDescPrefix')}
						<a
							href="https://github.com/NateScarlet/holiday-cn"
							target="_blank"
							rel="noopener noreferrer"
							class="text-primary hover:underline font-mono"
						>NateScarlet/holiday-cn</a>
						{$t('admin.holidays.offlineDescSuffix')}
					</p>
				</div>
				<div class="flex-shrink-0">
					<input
						bind:this={importInput}
						type="file"
						accept=".json,application/json"
						multiple
						onchange={handleImportFiles}
						class="sr-only"
					/>
					<button
						type="button"
						disabled={importing}
						onclick={() => importInput?.click()}
						class="px-3 py-1.5 text-xs font-medium rounded-lg border border-border bg-background text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50"
					>
						{importing ? $t('admin.holidays.importing') : $t('admin.holidays.importButton')}
					</button>
				</div>
			</div>
		</section>

		<!-- Card 2: Manual Custom Overrides -->
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6">
			<div class="mb-4">
				<h3 class="font-semibold text-foreground">{$t('admin.holidays.overridesHeading')}</h3>
				<p class="text-xs text-muted-foreground mt-1">{$t('admin.holidays.overridesDesc')}</p>
			</div>

			<form onsubmit={handleSaveOverride} class="grid grid-cols-1 sm:grid-cols-12 gap-3 items-end">
				<div class="sm:col-span-3">
					<label for="holiday-override-date" class="block text-xs font-medium text-muted-foreground mb-1">
						{$t('admin.holidays.fieldDate')}
					</label>
					<input
						id="holiday-override-date"
						type="date"
						required
						bind:value={formDate}
						class="w-full px-3 py-2 text-sm bg-background border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-primary/40"
					/>
				</div>
				<div class="sm:col-span-4">
					<label for="holiday-override-name" class="block text-xs font-medium text-muted-foreground mb-1">
						{$t('admin.holidays.fieldName')}
					</label>
					<input
						id="holiday-override-name"
						type="text"
						required
						maxlength="40"
						placeholder={$t('admin.holidays.fieldNamePlaceholder')}
						bind:value={formName}
						class="w-full px-3 py-2 text-sm bg-background border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-primary/40"
					/>
				</div>
				<div class="sm:col-span-3">
					<span class="block text-xs font-medium text-muted-foreground mb-1">
						{$t('admin.holidays.fieldType')}
					</span>
					<div class="grid grid-cols-2 rounded-lg border border-border p-0.5 bg-muted/40">
						<button
							type="button"
							onclick={() => (formIsOffDay = true)}
							class="py-1.5 text-xs font-medium rounded-md transition-colors {formIsOffDay ? 'bg-rose-500/15 text-rose-600 dark:text-rose-400 shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
						>
							{$t('admin.holidays.typeOff')}
						</button>
						<button
							type="button"
							onclick={() => (formIsOffDay = false)}
							class="py-1.5 text-xs font-medium rounded-md transition-colors {!formIsOffDay ? 'bg-slate-500/20 text-slate-700 dark:text-slate-200 shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
						>
							{$t('admin.holidays.typeWork')}
						</button>
					</div>
				</div>
				<div class="sm:col-span-2">
					<button
						type="submit"
						disabled={savingOverride || !formDate || !formName.trim()}
						class="w-full px-3 py-2 text-sm font-medium rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
					>
						{savingOverride ? $t('common.saving') : $t('common.save')}
					</button>
				</div>
			</form>

			<!-- List of custom overrides -->
			<div class="mt-5 pt-4 border-t border-border/50">
				{#if overview.overrides.length === 0}
					<div class="text-xs text-muted-foreground py-3 text-center">
						{$t('admin.holidays.overridesEmpty')}
					</div>
				{:else}
					<div class="divide-y divide-border/40">
						{#each overview.overrides as ov (ov.date)}
							<div class="py-2.5 flex items-center justify-between gap-3">
								<div class="flex items-center gap-2.5 min-w-0 flex-wrap">
									<span class="font-mono text-xs text-foreground">{ov.date}</span>
									<span
										class="px-1.5 py-0.5 rounded text-[11px] font-semibold {ov.isOffDay
											? 'bg-rose-500/10 text-rose-600 dark:text-rose-400'
											: 'bg-slate-500/15 text-slate-600 dark:text-slate-300'}"
									>
										{ov.isOffDay ? $t('calendar.holidayBadgeOff') : $t('calendar.holidayBadgeWork')}
									</span>
									<span class="text-sm text-foreground truncate">{ov.name}</span>
								</div>
								<div class="flex items-center gap-2 flex-shrink-0">
									<button
										type="button"
										onclick={() => editDay(ov)}
										class="text-xs px-2 py-1 rounded-md border border-border/60 text-foreground hover:bg-muted/50 transition-colors"
									>
										{$t('admin.holidays.edit')}
									</button>
									<button
										type="button"
										disabled={deletingDate === ov.date}
										onclick={() => handleDeleteOverride(ov.date)}
										class="text-xs px-2 py-1 rounded-md text-rose-600 dark:text-rose-400 hover:bg-rose-500/10 transition-colors disabled:opacity-50"
									>
										{$t('admin.holidays.delete')}
									</button>
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		</section>

		<!-- Card 3: Yearly Effective Schedule Preview -->
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6">
			<div class="flex flex-wrap items-center justify-between gap-3 mb-4">
				<div>
					<h3 class="font-semibold text-foreground">{$t('admin.holidays.previewHeading')}</h3>
					<p class="text-xs text-muted-foreground mt-0.5">{$t('admin.holidays.previewDesc')}</p>
				</div>
				<select
					value={previewYear}
					onchange={handlePreviewYearChange}
					aria-label={$t('admin.holidays.selectYear')}
					class="px-3 py-1.5 text-sm bg-background border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-primary/40"
				>
					{#each yearOptions as y}
						<option value={y}>{y}</option>
					{/each}
				</select>
			</div>

			{#if previewLoading}
				<div class="py-8 text-center text-xs text-muted-foreground">{$t('common.loading')}</div>
			{:else if previewData && previewData.days.length > 0}
				{#if previewData.papers && previewData.papers.length > 0}
					<div class="mb-3 text-xs text-muted-foreground flex flex-wrap items-center gap-1.5">
						<span>{$t('admin.holidays.policySource')}:</span>
						{#each previewData.papers as paper, idx}
							<a
								href={paper}
								target="_blank"
								rel="noopener noreferrer"
								class="text-primary hover:underline break-all"
							>
								{$t('admin.holidays.policyLink', { n: idx + 1 })}
							</a>
						{/each}
					</div>
				{/if}
				<div class="max-h-72 overflow-y-auto divide-y divide-border/40 border border-border/40 rounded-lg px-3">
					{#each previewData.days as day (day.date)}
						<div class="py-2 flex items-center justify-between gap-2 text-sm">
							<div class="flex items-center gap-2 min-w-0 flex-wrap">
								<span class="font-mono text-xs text-muted-foreground">{day.date}</span>
								<span
									class="px-1.5 py-px rounded text-[10px] font-semibold {day.isOffDay
										? 'bg-rose-500/10 text-rose-600 dark:text-rose-400'
										: 'bg-slate-500/15 text-slate-600 dark:text-slate-300'}"
								>
									{day.isOffDay ? $t('calendar.holidayBadgeOff') : $t('calendar.holidayBadgeWork')}
								</span>
								<span class="text-foreground truncate">{day.name}</span>
								{#if day.custom}
									<span class="px-1.5 py-px rounded text-[10px] font-medium bg-primary/10 text-primary">
										{$t('admin.holidays.customTag')}
									</span>
								{/if}
							</div>
							<button
								type="button"
								onclick={() => editDay(day)}
								class="text-xs text-muted-foreground hover:text-foreground px-2 py-0.5 rounded hover:bg-muted/60 flex-shrink-0"
							>
								{$t('admin.holidays.overrideAction')}
							</button>
						</div>
					{/each}
				</div>
			{:else}
				<div class="py-6 text-center text-xs text-muted-foreground">{$t('admin.holidays.previewEmpty')}</div>
			{/if}
		</section>
	{/if}
</div>
