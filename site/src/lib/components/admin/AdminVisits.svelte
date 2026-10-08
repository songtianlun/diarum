<script lang="ts">
	/**
	 * Admin → Visitor statistics: switch recording on and off, retention and
	 * flood protection, the S3 archive (pull days back in one click), and every
	 * owner's statistics and logs.
	 */
	import { onMount } from 'svelte';
	import {
		getVisitSettings,
		saveVisitSettings,
		testVisitArchive,
		getVisitOverview,
		listVisitOwners,
		listVisitArchives,
		runVisitArchive,
		runVisitCleanup,
		pullVisitArchives,
		unloadPulledVisits,
		type VisitSettings,
		type VisitSettingsResponse,
		type VisitOverview,
		type OwnerStat,
		type VisitArchive,
		type RunReport,
		type PullReport
	} from '$lib/api/visits';
	import { formatBytes } from '$lib/api/admin';
	import { getIntlLocale, t } from '$lib/i18n';
	import VisitStats from '$lib/components/visits/VisitStats.svelte';
	import RetentionInput from '$lib/components/visits/RetentionInput.svelte';
	import { relativeTime, formatNumber, dayLabel } from '$lib/components/visits/visitFormat';

	let response = $state<VisitSettingsResponse | null>(null);
	let form = $state<VisitSettings | null>(null);
	let saved = $state('');
	let overview = $state<VisitOverview | null>(null);
	let owners = $state<OwnerStat[]>([]);
	let ownersTotal = $state(0);
	let ownerSort = $state('views');
	let archives = $state<VisitArchive[]>([]);
	let archivesEnabled = $state(false);
	let archivesError = $state('');
	let archiveFilter = $state('');
	let pullFrom = $state('');
	let pullTo = $state('');

	let loading = $state(true);
	let error = $state('');
	let saving = $state(false);
	let testing = $state(false);
	let running = $state<'' | 'archive' | 'cleanup' | 'pull' | 'unload'>('');
	let busyDay = $state('');
	let message = $state<{ ok: boolean; text: string } | null>(null);
	let selected = $state<{ owner: string; name: string } | null>(null);

	onMount(() => {
		void loadAll();
	});

	function clone(value: VisitSettingsResponse): VisitSettings {
		return {
			enabled: value.enabled,
			retention_days: value.retention_days,
			dedupe_seconds: value.dedupe_seconds,
			max_records: value.max_records,
			ip_limit_per_minute: value.ip_limit_per_minute,
			global_failed_per_minute: value.global_failed_per_minute,
			owner_limit_per_minute: value.owner_limit_per_minute,
			archive: {
				enabled: value.archive.enabled,
				source: value.archive.source,
				s3: { ...value.archive.s3, secret: '' },
				prefix: value.archive.prefix,
				retention_days: value.archive.retention_days
			}
		};
	}

	function apply(next: VisitSettingsResponse) {
		response = next;
		form = clone(next);
		saved = JSON.stringify(form);
	}

	async function loadAll() {
		loading = true;
		error = '';
		try {
			apply(await getVisitSettings());
			await Promise.all([loadOverview(), loadOwners(), loadArchives()]);
		} catch (e) {
			error = e instanceof Error ? e.message : $t('admin.loadFailed');
		}
		loading = false;
	}

	async function loadOverview() {
		try {
			overview = await getVisitOverview();
		} catch {
			overview = null;
		}
	}

	async function loadOwners() {
		try {
			const data = await listVisitOwners({ sort: ownerSort, limit: 200 });
			owners = data.owners;
			ownersTotal = data.total;
		} catch {
			owners = [];
		}
	}

	async function loadArchives() {
		archivesError = '';
		try {
			const data = await listVisitArchives();
			archivesEnabled = data.enabled;
			archives = data.archives;
		} catch (e) {
			archivesEnabled = true;
			archives = [];
			archivesError = e instanceof Error ? e.message : $t('admin.loadFailed');
		}
	}

	function flash(ok: boolean, text: string) {
		message = { ok, text };
		setTimeout(() => {
			if (message?.text === text) message = null;
		}, ok ? 5000 : 12000);
	}

	async function save() {
		if (!form) return;
		saving = true;
		try {
			const body: VisitSettings = {
				...form,
				retention_days: Number(form.retention_days),
				dedupe_seconds: Number(form.dedupe_seconds),
				max_records: Number(form.max_records),
				ip_limit_per_minute: Number(form.ip_limit_per_minute),
				global_failed_per_minute: Number(form.global_failed_per_minute),
				owner_limit_per_minute: Number(form.owner_limit_per_minute),
				archive: { ...form.archive, retention_days: Number(form.archive.retention_days) }
			};
			apply(await saveVisitSettings(body));
			flash(true, $t('admin.visits.saved'));
			await Promise.all([loadOverview(), loadArchives()]);
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.visits.saveFailed'));
		}
		saving = false;
	}

	async function test() {
		if (!form) return;
		testing = true;
		try {
			await testVisitArchive(form.archive.source, form.archive.s3, form.archive.prefix);
			flash(true, $t('admin.visits.testOk'));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.visits.testFailed'));
		}
		testing = false;
	}

	function describeReport(report: RunReport): string {
		const parts = [];
		if (report.archived.length) parts.push($t('admin.visits.report.archived', { count: report.archived.length }));
		if (report.removed_remote.length) parts.push($t('admin.visits.report.removedRemote', { count: report.removed_remote.length }));
		if (report.deleted) parts.push($t('admin.visits.report.deleted', { count: formatNumber(report.deleted) }));
		if (report.trimmed) parts.push($t('admin.visits.report.trimmed', { count: formatNumber(report.trimmed) }));
		if (report.unloaded) parts.push($t('admin.visits.report.unloaded', { count: formatNumber(report.unloaded) }));
		if (report.kept.length) parts.push($t('admin.visits.report.kept', { count: report.kept.length }));
		return parts.join(' · ') || $t('admin.visits.report.nothing');
	}

	function describePull(report: PullReport): string {
		const parts = [$t('admin.visits.pulled', { records: formatNumber(report.records), days: report.days.length })];
		if (report.skipped.length) parts.push($t('admin.visits.pullSkipped', { count: report.skipped.length }));
		if (report.errors.length) parts.push($t('admin.visits.pullErrors', { errors: report.errors.slice(0, 3).join('; ') }));
		return parts.join(' ');
	}

	async function run(kind: 'archive' | 'cleanup') {
		running = kind;
		try {
			const { report } = kind === 'archive' ? await runVisitArchive() : await runVisitCleanup();
			flash(true, describeReport(report));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.visits.runFailed'));
		}
		running = '';
		await Promise.all([loadOverview(), loadArchives(), loadOwners()]);
	}

	async function pull(params: { days?: string[]; start?: string; end?: string }, day = '') {
		running = 'pull';
		busyDay = day;
		try {
			const { report } = await pullVisitArchives(params);
			flash(report.errors.length === 0, describePull(report));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.visits.pullFailed'));
		}
		running = '';
		busyDay = '';
		await Promise.all([loadOverview(), loadArchives(), loadOwners()]);
	}

	async function unload() {
		running = 'unload';
		try {
			const { removed } = await unloadPulledVisits();
			flash(true, $t('admin.visits.unloaded', { count: formatNumber(removed) }));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.loadFailed'));
		}
		running = '';
		await Promise.all([loadOverview(), loadArchives(), loadOwners()]);
	}

	function when(value?: string): string {
		if (!value) return '—';
		const date = new Date(value);
		if (Number.isNaN(date.getTime()) || date.getFullYear() < 2000) return '—';
		return date.toLocaleString(getIntlLocale(), { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
	}

	function ownerName(o: OwnerStat): string {
		if (!o.owner) return $t('admin.visits.unattributedOwner');
		return o.username || o.owner.slice(0, 10);
	}

	function openOwner(owner: string, name: string) {
		selected = { owner, name };
		window.scrollTo({ top: 0, behavior: 'smooth' });
	}

	let dirty = $derived(form !== null && JSON.stringify(form) !== saved);
	let limits = $derived(response?.limits ?? {});
	let sched = $derived(overview?.state ?? response?.state);
	let storage = $derived(overview?.storage);
	let usage = $derived(storage && overview ? Math.min(100, (storage.records / Math.max(1, overview.max_records)) * 100) : 0);
	let customComplete = $derived(!!form && !!(form.archive.s3.bucket && form.archive.s3.region && form.archive.s3.access_key && (form.archive.s3.secret || response?.secret_set)));
	let canTest = $derived(!!form && (form.archive.source === 'audit' ? !!response?.shared_s3_available : customComplete));
	let visibleArchives = $derived(archiveFilter ? archives.filter((a) => a.day.includes(archiveFilter)) : archives);
	let remoteOnly = $derived(archives.filter((a) => !a.local && !a.pulled).length);
	let pulledDays = $derived(archives.filter((a) => a.pulled).length);
	const input = 'px-3 py-2 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary/40';
	const btn = 'px-3 py-1.5 text-sm rounded-lg border border-border/60 hover:bg-muted/50 disabled:opacity-40 disabled:cursor-not-allowed';
</script>

<div class="space-y-4">
	{#if message}
		<div class="sticky top-12 z-10 p-3 rounded-lg text-sm shadow-sm animate-fade-in break-words {message.ok ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border border-emerald-500/20' : 'bg-destructive/10 text-destructive border border-destructive/20'}">{message.text}</div>
	{/if}

	{#if error}
		<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm flex items-center justify-between gap-3"><span>{error}</span><button onclick={loadAll} class="underline">{$t('admin.retry')}</button></div>
	{:else if loading && !response}
		<div class="space-y-3">{#each Array(3) as _}<div class="h-32 rounded-xl bg-muted/40 animate-pulse"></div>{/each}</div>
	{:else if response && form}
		{#if selected}
			<!-- One owner's statistics -->
			<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6 animate-fade-in">
				<button type="button" onclick={() => (selected = null)} class="mb-4 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" /></svg>
					{$t('admin.visits.backToOwners')}
				</button>
				{#key selected.owner}
					<VisitStats
						scope={{ kind: 'admin', owner: selected.owner }}
						heading={$t('admin.visits.viewing', { name: selected.name })}
						description={selected.owner === '-' ? $t('admin.visits.unattributedDesc') : $t('visits.adminDescription')}
						retentionDays={response.retention_days}
						dedupeSeconds={response.dedupe_seconds}
					/>
				{/key}
			</section>
		{:else}
			<!-- Status -->
			<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6 animate-fade-in">
				<div class="flex items-start justify-between gap-3 mb-4">
					<div class="min-w-0">
						<div class="flex flex-wrap items-center gap-2">
							<h2 class="text-lg font-semibold text-foreground">{$t('admin.visits.heading')}</h2>
							<span class="px-2 py-0.5 rounded-full text-[11px] font-medium ring-1 ring-inset {response.enabled ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 ring-emerald-500/20' : 'bg-muted text-muted-foreground ring-border/60'}">
								{#if response.enabled}<span class="inline-block w-1.5 h-1.5 rounded-full bg-emerald-500 mr-1 align-middle animate-pulse"></span>{/if}{response.enabled ? $t('admin.visits.recording') : $t('admin.visits.notRecording')}
							</span>
						</div>
						<p class="text-sm text-muted-foreground mt-1">{$t('admin.visits.description')}</p>
					</div>
					<button onclick={loadAll} disabled={loading} title={$t('admin.refresh')} aria-label={$t('admin.refresh')} class="flex-shrink-0 p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50">
						<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
					</button>
				</div>
				<div class="grid grid-cols-2 lg:grid-cols-4 gap-2">
					<div class="rounded-lg bg-muted/40 px-3 py-2.5">
						<div class="text-[11px] text-muted-foreground">{$t('admin.visits.records')}</div>
						<div class="text-lg font-semibold tabular-nums">{formatNumber(storage?.records)}</div>
						<div class="mt-1 h-1.5 rounded-full bg-muted overflow-hidden" title={$t('admin.visits.ofLimit', { percent: usage.toFixed(1) })}><div class="h-full rounded-full {usage > 90 ? 'bg-rose-500' : usage > 70 ? 'bg-amber-500' : 'bg-primary/70'}" style="width: {Math.max(usage, storage?.records ? 1 : 0)}%"></div></div>
						<div class="text-[11px] text-muted-foreground mt-0.5">{$t('admin.visits.ofLimit', { percent: usage.toFixed(1) })}{#if storage?.pulled} · {$t('admin.visits.pulledRecords', { count: formatNumber(storage.pulled) })}{/if}</div>
					</div>
					<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('admin.visits.dbSize')}</div><div class="text-lg font-semibold tabular-nums">{storage ? formatBytes(storage.bytes) : '—'}</div><div class="text-[11px] text-muted-foreground truncate">{$t('admin.visits.oldest')}: {storage?.oldest ? relativeTime(storage.oldest) : '—'}</div></div>
					<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('admin.visits.nextCleanup')}</div><div class="text-sm font-medium mt-1">{sched?.running ? $t('admin.visits.running') : when(sched?.next_cleanup)}</div>{#if sched?.next_retry}<div class="text-[11px] text-amber-600 dark:text-amber-400">{$t('admin.visits.retryAt', { time: when(sched.next_retry) })}</div>{/if}</div>
					<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('admin.visits.lastCleanup')}</div><div class="text-sm font-medium mt-1">{when(sched?.last_cleanup?.finished)}</div>{#if sched?.last_cleanup}<div class="text-[11px] truncate {sched.last_cleanup.error ? 'text-rose-600 dark:text-rose-400' : 'text-muted-foreground'}" title={sched.last_cleanup.error || describeReport(sched.last_cleanup)}>{sched.last_cleanup.error || describeReport(sched.last_cleanup)}</div>{/if}</div>
				</div>
				{#if overview}
					<p class="mt-3 text-xs text-muted-foreground">
						{$t('admin.visits.writer', { written: formatNumber(overview.writer.written), deduped: formatNumber(overview.writer.deduped), suppressed: formatNumber(overview.writer.suppressed), dropped: formatNumber(overview.writer.dropped) })}
						{#if storage?.suppressed}· {$t('admin.visits.suppressed')}: {formatNumber(storage.suppressed)} ({$t('admin.visits.suppressedHint')}){/if}
					</p>
				{/if}
				{#if sched?.last_archive?.error}
					<div class="mt-3 p-2.5 rounded-lg bg-destructive/10 text-destructive text-xs break-words">{sched.last_archive.error}</div>
				{/if}
				<div class="mt-4 flex flex-wrap gap-2">
					<button onclick={() => run('archive')} disabled={!!running || !response.archive.enabled} class={btn}>{running === 'archive' ? $t('admin.visits.running') : $t('admin.visits.runArchive')}</button>
					<button onclick={() => run('cleanup')} disabled={!!running} class={btn}>{running === 'cleanup' ? $t('admin.visits.running') : $t('admin.visits.runCleanup')}</button>
				</div>
				<p class="mt-2 text-xs text-muted-foreground">{$t('admin.visits.scheduleHint', { tz: response.server_timezone })}</p>
			</section>

			<!-- Owners -->
			<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6">
				<div class="flex flex-wrap items-start justify-between gap-2 mb-3">
					<div class="min-w-0">
						<h3 class="font-semibold text-foreground">{$t('admin.visits.owners')}</h3>
						<p class="text-xs text-muted-foreground mt-1">{$t('admin.visits.ownersDesc')}</p>
					</div>
					<div class="flex items-center gap-2">
						<select bind:value={ownerSort} onchange={loadOwners} class="px-2.5 py-1.5 bg-background rounded-lg text-xs border border-border/60 focus:outline-none focus:ring-2 focus:ring-primary/40">
							{#each ['views', 'failed', 'others', 'last'] as s}<option value={s}>{$t(`admin.visits.ownerSort.${s}`)}</option>{/each}
						</select>
						<button type="button" onclick={() => openOwner('*', $t('admin.visits.allOwners'))} class="px-2.5 py-1.5 text-xs rounded-lg bg-primary text-primary-foreground hover:bg-primary/90">{$t('admin.visits.allOwners')}</button>
					</div>
				</div>
				{#if owners.length === 0}
					<div class="py-6 text-center text-sm text-muted-foreground">{$t('admin.visits.noOwners')}</div>
				{:else}
					<ul class="divide-y divide-border/50 rounded-xl border border-border/50 overflow-hidden max-h-[32rem] overflow-y-auto">
						{#each owners as o (o.owner)}
							<li>
								<button type="button" onclick={() => openOwner(o.owner || '-', ownerName(o))} class="w-full text-left px-3 py-2.5 flex items-center gap-3 bg-card hover:bg-muted/40 transition-colors">
									<span class="flex-shrink-0 w-8 h-8 rounded-full flex items-center justify-center text-xs font-semibold {o.owner ? 'bg-primary/10 text-primary' : 'bg-rose-500/10 text-rose-700 dark:text-rose-300'}" aria-hidden="true">{o.owner ? ownerName(o).charAt(0).toUpperCase() : '?'}</span>
									<span class="min-w-0 flex-1">
										<span class="block text-sm font-medium text-foreground truncate">{ownerName(o)}</span>
										<span class="block text-xs text-muted-foreground truncate">
											{#if o.owner}{$t('visits.diariesCount', { count: o.diaries })} · {/if}{$t('visits.visitorsCount', { count: o.visitors })}
											{#if o.others && o.owner}· <span class="text-amber-600 dark:text-amber-400">{$t('visits.othersCount', { count: o.others })}</span>{/if}
											{#if o.failed}· <span class="text-rose-600 dark:text-rose-400">{$t('visits.failedCount', { count: o.failed })}</span>{/if}
											· {$t('visits.lastSeen', { when: relativeTime(o.last) })}
										</span>
									</span>
									<span class="text-right flex-shrink-0">
										<span class="block text-base font-semibold tabular-nums">{formatNumber(o.views)}</span>
										<span class="block text-[11px] text-muted-foreground">{$t('visits.cards.views')}</span>
									</span>
									<svg class="w-4 h-4 text-muted-foreground flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
								</button>
							</li>
						{/each}
					</ul>
					{#if ownersTotal > owners.length}<p class="mt-2 text-xs text-muted-foreground">{$t('visits.showing', { shown: owners.length, total: ownersTotal })}</p>{/if}
				{/if}
			</section>

			<!-- Settings -->
			<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6">
				<h3 class="font-semibold text-foreground mb-4">{$t('admin.visits.settings')}</h3>
				<label class="flex items-start justify-between gap-4 cursor-pointer">
					<span>
						<span class="block text-sm font-medium text-foreground">{$t('admin.visits.enable')}</span>
						<span class="block text-xs text-muted-foreground mt-0.5">{$t('admin.visits.enableDesc')}</span>
					</span>
					<input type="checkbox" bind:checked={form.enabled} class="mt-1 h-5 w-5 rounded border-border accent-[hsl(var(--primary))]" />
				</label>

				<div class="mt-5 grid gap-4 sm:grid-cols-2">
					<div>
						<label for="visit-retention" class="text-sm font-medium text-foreground">{$t('admin.visits.retention')}</label>
						<div class="mt-1"><RetentionInput id="visit-retention" bind:value={form.retention_days} max={limits.retention_max} /></div>
						<span class="mt-1 block text-xs text-muted-foreground">{$t('admin.visits.retentionHint')}</span>
					</div>
					<label class="block">
						<span class="text-sm font-medium text-foreground">{$t('admin.visits.dedupe')}</span>
						<span class="mt-1 flex items-center gap-2">
							<input type="number" min="0" max={limits.dedupe_max} bind:value={form.dedupe_seconds} class="{input} w-28 tabular-nums" />
							<span class="text-sm text-muted-foreground">{$t('admin.visits.seconds')}</span>
						</span>
						<span class="mt-1 block text-xs text-muted-foreground">{$t('admin.visits.dedupeHint')}</span>
					</label>
				</div>

				<details class="mt-5 pt-4 border-t border-border/50 group">
					<summary class="cursor-pointer list-none flex items-center justify-between gap-2">
						<span>
							<span class="block text-sm font-medium text-foreground">{$t('admin.visits.protection')}</span>
							<span class="block text-xs text-muted-foreground mt-0.5">{$t('admin.visits.protectionDesc')}</span>
						</span>
						<svg class="w-4 h-4 text-muted-foreground transition-transform group-open:rotate-180" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" /></svg>
					</summary>
					<div class="mt-4 grid gap-4 sm:grid-cols-2">
						<label class="block">
							<span class="text-xs text-muted-foreground">{$t('admin.visits.maxRecords')}</span>
							<input type="number" min={limits.max_records_min} max={limits.max_records_max} step="10000" bind:value={form.max_records} class="{input} mt-1 w-full tabular-nums" />
							<span class="mt-1 block text-[11px] text-muted-foreground">{$t('admin.visits.maxRecordsHint')}</span>
						</label>
						<label class="block">
							<span class="text-xs text-muted-foreground">{$t('admin.visits.ipLimit')}</span>
							<input type="number" min="1" max={limits.rate_max} bind:value={form.ip_limit_per_minute} class="{input} mt-1 w-full tabular-nums" />
						</label>
						<label class="block">
							<span class="text-xs text-muted-foreground">{$t('admin.visits.globalLimit')}</span>
							<input type="number" min="1" max={limits.rate_max} bind:value={form.global_failed_per_minute} class="{input} mt-1 w-full tabular-nums" />
						</label>
						<label class="block">
							<span class="text-xs text-muted-foreground">{$t('admin.visits.ownerLimit')}</span>
							<input type="number" min="1" max={limits.rate_max} bind:value={form.owner_limit_per_minute} class="{input} mt-1 w-full tabular-nums" />
						</label>
					</div>
				</details>

				<div class="mt-5 pt-4 border-t border-border/50">
					<label class="flex items-start justify-between gap-4 cursor-pointer">
						<span>
							<span class="block text-sm font-medium text-foreground">{$t('admin.visits.archiveEnable')}</span>
							<span class="block text-xs text-muted-foreground mt-0.5">{$t('admin.visits.archiveEnableDesc')}</span>
						</span>
						<input type="checkbox" bind:checked={form.archive.enabled} class="mt-1 h-5 w-5 rounded border-border accent-[hsl(var(--primary))]" />
					</label>

					<div class="mt-4 space-y-4 {form.archive.enabled ? '' : 'opacity-60'}">
						<fieldset>
							<legend class="text-xs text-muted-foreground mb-1.5">{$t('admin.visits.sourceLabel')}</legend>
							<div class="grid gap-2 sm:grid-cols-2">
								<label class="flex items-start gap-2 p-3 rounded-lg border cursor-pointer transition-colors {form.archive.source === 'audit' ? 'border-primary/60 bg-primary/5' : 'border-border/60 hover:bg-muted/40'}">
									<input type="radio" bind:group={form.archive.source} value="audit" class="mt-0.5" />
									<span class="min-w-0">
										<span class="block text-sm text-foreground">{$t('admin.visits.sourceShared')}</span>
										<span class="block text-xs mt-0.5 {response.shared_s3_available ? 'text-muted-foreground' : 'text-amber-600 dark:text-amber-400'}">{response.shared_s3_available ? $t('admin.visits.sourceSharedOk', { bucket: response.shared_s3_bucket }) : $t('admin.visits.sourceSharedMissing')}</span>
									</span>
								</label>
								<label class="flex items-start gap-2 p-3 rounded-lg border cursor-pointer transition-colors {form.archive.source === 'custom' ? 'border-primary/60 bg-primary/5' : 'border-border/60 hover:bg-muted/40'}">
									<input type="radio" bind:group={form.archive.source} value="custom" class="mt-0.5" />
									<span class="block text-sm text-foreground">{$t('admin.visits.sourceCustom')}</span>
								</label>
							</div>
						</fieldset>

						{#if form.archive.source === 'custom'}
							<div class="grid gap-3 sm:grid-cols-2">
								<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('admin.visits.bucket')}</span><input bind:value={form.archive.s3.bucket} autocomplete="off" class="{input} mt-1 w-full" /></label>
								<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('admin.visits.region')}</span><input bind:value={form.archive.s3.region} autocomplete="off" placeholder="us-east-1" class="{input} mt-1 w-full" /></label>
								<label class="block text-sm sm:col-span-2"><span class="text-muted-foreground text-xs">{$t('admin.visits.endpoint')}</span><input bind:value={form.archive.s3.endpoint} autocomplete="off" placeholder="https://s3.example.com" class="{input} mt-1 w-full" /></label>
								<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('admin.visits.accessKey')}</span><input bind:value={form.archive.s3.access_key} autocomplete="off" class="{input} mt-1 w-full font-mono" /></label>
								<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('admin.visits.secret')}</span><input type="password" bind:value={form.archive.s3.secret} autocomplete="new-password" placeholder={response.secret_set ? $t('admin.visits.secretKeep') : ''} class="{input} mt-1 w-full font-mono" /></label>
								<label class="flex items-center gap-2 text-sm sm:col-span-2"><input type="checkbox" bind:checked={form.archive.s3.force_path_style} class="rounded border-border" />{$t('admin.visits.pathStyle')}</label>
							</div>
						{/if}

						<div class="grid gap-4 sm:grid-cols-2">
							<label class="block text-sm">
								<span class="text-muted-foreground text-xs">{$t('admin.visits.prefix')}</span>
								<input bind:value={form.archive.prefix} autocomplete="off" placeholder="diarum-visits" class="{input} mt-1 w-full font-mono" />
								<span class="mt-1 block text-[11px] text-muted-foreground break-all">{$t('admin.visits.layoutHint', { path: `${form.archive.prefix || 'diarum-visits'}/YYYY/YYYY-MM-DD.jsonl.gz` })}</span>
							</label>
							<div>
								<label for="visit-archive-retention" class="text-xs text-muted-foreground">{$t('admin.visits.archiveRetention')}</label>
								<div class="mt-1"><RetentionInput id="visit-archive-retention" bind:value={form.archive.retention_days} presets={[1825, 3650, 7300]} max={limits.archive_retention_max} /></div>
								<span class="mt-1 block text-[11px] text-muted-foreground">{$t('admin.visits.archiveRetentionHint')}</span>
							</div>
						</div>
					</div>
				</div>

				<div class="mt-5 flex flex-wrap items-center gap-2">
					<button onclick={save} disabled={saving || !dirty} class="px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed">{saving ? $t('admin.visits.saving') : $t('admin.visits.save')}</button>
					<button onclick={test} disabled={testing || !canTest} class="px-4 py-2 rounded-lg border border-border/60 text-sm hover:bg-muted/50 disabled:opacity-50 disabled:cursor-not-allowed">{testing ? $t('admin.visits.testing') : $t('admin.visits.test')}</button>
					{#if dirty}<span class="text-xs text-amber-600 dark:text-amber-400">{$t('admin.visits.unsaved')}</span>{/if}
				</div>
			</section>

			<!-- Archives -->
			<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6">
				<div class="flex flex-wrap items-start justify-between gap-2 mb-3">
					<div class="min-w-0">
						<h3 class="font-semibold text-foreground">{$t('admin.visits.archives')}</h3>
						<p class="text-xs text-muted-foreground mt-1">{$t('admin.visits.archivesDesc', { days: limits.pulled_keep_days ?? 7 })}</p>
					</div>
					{#if archivesEnabled && archives.length}
						<div class="flex flex-wrap gap-2">
							<button onclick={() => pull({})} disabled={!!running || remoteOnly === 0} class="px-3 py-1.5 text-sm rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-40 disabled:cursor-not-allowed">{running === 'pull' && !busyDay ? $t('admin.visits.pulling') : `${$t('admin.visits.pullAll')} (${remoteOnly})`}</button>
							{#if pulledDays || storage?.pulled}<button onclick={unload} disabled={!!running} class="px-3 py-1.5 text-sm rounded-lg text-rose-600 dark:text-rose-400 hover:bg-rose-500/10 disabled:opacity-40">{$t('admin.visits.unloadAll')}</button>{/if}
						</div>
					{/if}
				</div>
				{#if !archivesEnabled}
					<div class="py-6 text-center text-sm text-muted-foreground">{$t('admin.visits.archiveOff')}</div>
				{:else if archivesError}
					<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm break-words">{archivesError}</div>
				{:else if archives.length === 0}
					<div class="py-6 text-center text-sm text-muted-foreground">{$t('admin.visits.noArchives')}</div>
				{:else}
					<div class="mb-3 flex flex-wrap items-center gap-2 p-2.5 rounded-lg bg-muted/40">
						<input type="date" bind:value={pullFrom} max={pullTo || undefined} aria-label={$t('visits.filters.from')} class="{input} py-1.5 text-xs flex-1 sm:flex-none min-w-0" />
						<span class="text-xs text-muted-foreground">–</span>
						<input type="date" bind:value={pullTo} min={pullFrom || undefined} aria-label={$t('visits.filters.to')} class="{input} py-1.5 text-xs flex-1 sm:flex-none min-w-0" />
						<button onclick={() => pull({ start: pullFrom, end: pullTo })} disabled={!!running || (!pullFrom && !pullTo)} class="{btn} text-xs">{$t('admin.visits.pullRange')}</button>
						<input type="search" bind:value={archiveFilter} placeholder={$t('admin.visits.filterArchives')} class="{input} py-1.5 text-xs w-full sm:w-40 sm:ml-auto font-mono" />
					</div>
					<ul class="divide-y divide-border/50 max-h-[28rem] overflow-y-auto">
						{#each visibleArchives as archive (archive.key)}
							<li class="flex flex-wrap items-center gap-2 py-2">
								<div class="min-w-0 flex-1">
									<div class="text-sm text-foreground">{dayLabel(archive.day, true)}</div>
									<div class="text-xs text-muted-foreground tabular-nums">{formatBytes(archive.size)} gz
										{#if archive.local}· <span class="text-emerald-600 dark:text-emerald-400">{$t('admin.visits.stillLocal')}</span>{/if}
										{#if archive.pulled}· <span class="text-sky-600 dark:text-sky-400">{$t('admin.visits.pulledBadge')}</span>{/if}
									</div>
								</div>
								{#if !archive.local && !archive.pulled}
									<button onclick={() => pull({ days: [archive.day] }, archive.day)} disabled={!!running} class="px-2.5 py-1 text-xs rounded-md border border-border/60 hover:bg-muted/50 disabled:opacity-50">{busyDay === archive.day ? $t('admin.visits.pulling') : $t('admin.visits.pull')}</button>
								{/if}
							</li>
						{/each}
					</ul>
				{/if}
			</section>
		{/if}
	{/if}
</div>
