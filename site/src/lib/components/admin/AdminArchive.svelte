<script lang="ts">
	/**
	 * Admin → Archive & retention: how long logs stay on disk, the optional
	 * S3 archive of finished days, the cleanup schedule, and the days that can
	 * be downloaded or pulled back for analysis.
	 */
	import { onMount } from 'svelte';
	import {
		getAuditSettings,
		saveAuditSettings,
		testAuditArchive,
		listAuditFiles,
		listArchives,
		runArchive,
		runCleanup,
		pullArchive,
		removePulled,
		downloadAuditFile,
		formatBytes,
		type AuditSettings,
		type AuditFile,
		type ArchiveInfo,
		type RunReport,
		type S3Config
	} from '$lib/api/admin';
	import { getIntlLocale, t } from '$lib/i18n';

	let { onview }: { onview?: (day: string) => void } = $props();

	let settings = $state<AuditSettings | null>(null);
	let retention = $state(3);
	let archiveEnabled = $state(false);
	let archiveRetention = $state(30);
	let s3 = $state<S3Config>({ bucket: '', region: '', endpoint: '', access_key: '', secret: '', force_path_style: false, prefix: 'diarum-audit' });
	let saved = $state('');

	let files = $state<AuditFile[]>([]);
	let archives = $state<ArchiveInfo[]>([]);
	let archivesEnabled = $state(false);
	let archivesError = $state('');

	let loading = $state(true);
	let error = $state('');
	let saving = $state(false);
	let testing = $state(false);
	let running = $state<'' | 'archive' | 'cleanup'>('');
	let busyDate = $state('');
	let message = $state<{ ok: boolean; text: string } | null>(null);
	let archiveFilter = $state('');

	onMount(() => {
		void loadAll();
	});

	function snapshot() {
		return JSON.stringify({ retention, archiveEnabled, archiveRetention, s3 });
	}

	function applySettings(next: AuditSettings) {
		settings = next;
		retention = next.retention_days;
		archiveEnabled = next.archive.enabled;
		archiveRetention = next.archive.retention_days;
		s3 = { ...next.archive.s3, secret: '' };
		saved = snapshot();
	}

	async function loadAll() {
		loading = true;
		error = '';
		try {
			applySettings(await getAuditSettings());
			await Promise.all([loadFiles(), loadArchives()]);
		} catch (e) {
			error = e instanceof Error ? e.message : $t('admin.loadFailed');
		}
		loading = false;
	}

	async function loadFiles() {
		try {
			const result = await listAuditFiles();
			files = result.files;
			if (settings) settings = { ...settings, state: result.state };
		} catch {
			files = [];
		}
	}

	async function loadArchives() {
		archivesError = '';
		try {
			const result = await listArchives();
			archivesEnabled = result.enabled;
			archives = result.archives;
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
		}, ok ? 4000 : 10000);
	}

	async function save() {
		saving = true;
		try {
			applySettings(
				await saveAuditSettings({
					retention_days: Number(retention),
					archive: { enabled: archiveEnabled, retention_days: Number(archiveRetention), s3 }
				})
			);
			flash(true, $t('admin.archive.saved'));
			await loadArchives();
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.archive.saveFailed'));
		}
		saving = false;
	}

	async function test() {
		testing = true;
		try {
			await testAuditArchive(s3);
			flash(true, $t('admin.archive.testOk'));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.archive.testFailed'));
		}
		testing = false;
	}

	function describeReport(report: RunReport): string {
		const parts = [];
		if (report.archived.length) parts.push($t('admin.archive.report.archived', { count: report.archived.length }));
		if (report.removed_local.length) parts.push($t('admin.archive.report.removedLocal', { count: report.removed_local.length }));
		if (report.removed_remote.length) parts.push($t('admin.archive.report.removedRemote', { count: report.removed_remote.length }));
		if (report.removed_pulled.length) parts.push($t('admin.archive.report.removedPulled', { count: report.removed_pulled.length }));
		if (report.kept.length) parts.push($t('admin.archive.report.kept', { count: report.kept.length }));
		return parts.join(' · ') || $t('admin.archive.report.nothing');
	}

	async function run(kind: 'archive' | 'cleanup') {
		running = kind;
		try {
			const { report } = kind === 'archive' ? await runArchive() : await runCleanup();
			flash(true, describeReport(report));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.archive.runFailed'));
		}
		running = '';
		await Promise.all([loadFiles(), loadArchives()]);
	}

	async function pull(date: string) {
		busyDate = date;
		try {
			const { file } = await pullArchive(date);
			flash(true, $t('admin.archive.pulled', { date, size: formatBytes(file.size) }));
			await Promise.all([loadFiles(), loadArchives()]);
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.archive.pullFailed'));
		}
		busyDate = '';
	}

	async function drop(date: string) {
		busyDate = date;
		try {
			await removePulled(date);
			await Promise.all([loadFiles(), loadArchives()]);
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.loadFailed'));
		}
		busyDate = '';
	}

	async function download(file: AuditFile) {
		try {
			await downloadAuditFile(file.date, file.pulled);
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('admin.loadFailed'));
		}
	}

	function when(value?: string): string {
		if (!value) return '—';
		const date = new Date(value);
		if (Number.isNaN(date.getTime()) || date.getFullYear() < 2000) return '—';
		return date.toLocaleString(getIntlLocale(), { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
	}

	function dayLabel(day: string): string {
		const [y, m, d] = day.split('-').map(Number);
		return new Date(y, m - 1, d).toLocaleDateString(getIntlLocale(), { year: 'numeric', month: 'short', day: 'numeric', weekday: 'short' });
	}

	let dirty = $derived(settings !== null && snapshot() !== saved);
	let s3Complete = $derived(!!(s3.bucket && s3.region && s3.access_key && (s3.secret || settings?.secret_set)));
	let localFiles = $derived(files.filter((f) => !f.pulled));
	let pulledFiles = $derived(files.filter((f) => f.pulled));
	let localBytes = $derived(localFiles.reduce((sum, f) => sum + f.size, 0));
	let visibleArchives = $derived(archiveFilter ? archives.filter((a) => a.date.includes(archiveFilter)) : archives);
	let sched = $derived(settings?.state);
	let limits = $derived(settings?.limits);
</script>

<div class="space-y-4">
	{#if message}
		<div class="sticky top-12 z-10 p-3 rounded-lg text-sm shadow-sm animate-fade-in {message.ok ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border border-emerald-500/20' : 'bg-destructive/10 text-destructive border border-destructive/20'}">{message.text}</div>
	{/if}

	{#if error}
		<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm flex items-center justify-between gap-3"><span>{error}</span><button onclick={loadAll} class="underline">{$t('admin.retry')}</button></div>
	{:else if loading && !settings}
		<div class="space-y-3">{#each Array(3) as _}<div class="h-32 rounded-xl bg-muted/40 animate-pulse"></div>{/each}</div>
	{:else if settings}
		{#if !settings.enabled}
			<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm">{$t('admin.archive.disabledServer')}</div>
		{/if}

		<!-- Schedule status -->
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6 animate-fade-in">
			<div class="flex items-start justify-between gap-3 mb-4">
				<div class="min-w-0">
					<h2 class="text-lg font-semibold text-foreground">{$t('admin.archive.heading')}</h2>
					<p class="text-sm text-muted-foreground mt-1">{$t('admin.archive.description')}</p>
				</div>
				<button onclick={loadAll} disabled={loading} title={$t('admin.refresh')} aria-label={$t('admin.refresh')} class="flex-shrink-0 p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50">
					<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
				</button>
			</div>
			<div class="grid grid-cols-2 lg:grid-cols-4 gap-2">
				<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('admin.archive.localDays')}</div><div class="text-lg font-semibold tabular-nums">{localFiles.length}</div><div class="text-[11px] text-muted-foreground">{formatBytes(localBytes)}</div></div>
				<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('admin.archive.archivedDays')}</div><div class="text-lg font-semibold tabular-nums">{archivesEnabled ? archives.length : '—'}</div><div class="text-[11px] text-muted-foreground">{archivesEnabled ? formatBytes(archives.reduce((s, a) => s + a.size, 0)) : $t('admin.archive.off')}</div></div>
				<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('admin.archive.nextCleanup')}</div><div class="text-sm font-medium mt-1">{sched?.running ? $t('admin.archive.running') : when(sched?.next_cleanup)}</div>{#if sched?.next_retry}<div class="text-[11px] text-amber-600 dark:text-amber-400">{$t('admin.archive.retryAt', { time: when(sched.next_retry) })}</div>{/if}</div>
				<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('admin.archive.lastCleanup')}</div><div class="text-sm font-medium mt-1">{when(sched?.last_cleanup?.finished)}</div>{#if sched?.last_cleanup}<div class="text-[11px] truncate {sched.last_cleanup.error ? 'text-rose-600 dark:text-rose-400' : 'text-muted-foreground'}" title={sched.last_cleanup.error || describeReport(sched.last_cleanup)}>{sched.last_cleanup.error || describeReport(sched.last_cleanup)}</div>{/if}</div>
			</div>
			{#if sched?.last_archive?.error}
				<div class="mt-3 p-2.5 rounded-lg bg-destructive/10 text-destructive text-xs break-words">{$t('admin.archive.lastArchiveFailed', { time: when(sched.last_archive.finished) })}: {sched.last_archive.error}</div>
			{/if}
			<div class="mt-4 flex flex-wrap gap-2">
				<button onclick={() => run('archive')} disabled={!!running || !settings.archive.enabled} class="px-3 py-1.5 text-sm rounded-lg border border-border/60 hover:bg-muted/50 disabled:opacity-40 disabled:cursor-not-allowed">{running === 'archive' ? $t('admin.archive.running') : $t('admin.archive.runArchive')}</button>
				<button onclick={() => run('cleanup')} disabled={!!running} class="px-3 py-1.5 text-sm rounded-lg border border-border/60 hover:bg-muted/50 disabled:opacity-40">{running === 'cleanup' ? $t('admin.archive.running') : $t('admin.archive.runCleanup')}</button>
			</div>
			<p class="mt-2 text-xs text-muted-foreground">{$t('admin.archive.scheduleHint', { tz: settings.server_timezone })}</p>
		</section>

		<!-- Settings -->
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6">
			<h3 class="font-semibold text-foreground mb-4">{$t('admin.archive.settings')}</h3>
			<div class="grid gap-4 sm:grid-cols-2">
				<label class="block">
					<span class="text-sm font-medium text-foreground">{$t('admin.archive.retention')}</span>
					<div class="mt-1 flex items-center gap-2">
						<input type="number" min={limits?.retention_min} max={limits?.retention_max} bind:value={retention} class="w-24 px-3 py-2 bg-background border border-border rounded-lg text-sm tabular-nums focus:outline-none focus:ring-2 focus:ring-primary/40" />
						<span class="text-sm text-muted-foreground">{$t('admin.archive.days')}</span>
					</div>
					<span class="mt-1 block text-xs text-muted-foreground">{$t('admin.archive.retentionHint', { min: limits?.retention_min ?? 1, max: limits?.retention_max ?? 90, def: limits?.retention_default ?? 3 })}</span>
				</label>
				<label class="block">
					<span class="text-sm font-medium text-foreground">{$t('admin.archive.archiveRetention')}</span>
					<div class="mt-1 flex items-center gap-2">
						<input type="number" min={limits?.archive_retention_min} max={limits?.archive_retention_max} bind:value={archiveRetention} class="w-24 px-3 py-2 bg-background border border-border rounded-lg text-sm tabular-nums focus:outline-none focus:ring-2 focus:ring-primary/40" />
						<span class="text-sm text-muted-foreground">{$t('admin.archive.days')}</span>
					</div>
					<span class="mt-1 block text-xs text-muted-foreground">{$t('admin.archive.archiveRetentionHint', { def: limits?.archive_retention_def ?? 30 })}</span>
				</label>
			</div>

			<div class="mt-5 pt-4 border-t border-border/50">
				<label class="flex items-start justify-between gap-4 cursor-pointer">
					<span>
						<span class="block text-sm font-medium text-foreground">{$t('admin.archive.enable')}</span>
						<span class="block text-xs text-muted-foreground mt-0.5">{$t('admin.archive.enableDesc')}</span>
					</span>
					<input type="checkbox" bind:checked={archiveEnabled} class="mt-1 h-5 w-5 rounded border-border accent-[hsl(var(--primary))]" />
				</label>

				<div class="mt-4 grid gap-3 sm:grid-cols-2 {archiveEnabled ? '' : 'opacity-60'}">
					<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('admin.archive.bucket')}</span><input bind:value={s3.bucket} autocomplete="off" class="mt-1 w-full px-3 py-2 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary/40" /></label>
					<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('admin.archive.region')}</span><input bind:value={s3.region} autocomplete="off" placeholder="us-east-1" class="mt-1 w-full px-3 py-2 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary/40" /></label>
					<label class="block text-sm sm:col-span-2"><span class="text-muted-foreground text-xs">{$t('admin.archive.endpoint')}</span><input bind:value={s3.endpoint} autocomplete="off" placeholder="https://s3.example.com" class="mt-1 w-full px-3 py-2 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary/40" /></label>
					<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('admin.archive.accessKey')}</span><input bind:value={s3.access_key} autocomplete="off" class="mt-1 w-full px-3 py-2 bg-background border border-border rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary/40" /></label>
					<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('admin.archive.secret')}</span><input type="password" bind:value={s3.secret} autocomplete="new-password" placeholder={settings.secret_set ? $t('admin.archive.secretKeep') : ''} class="mt-1 w-full px-3 py-2 bg-background border border-border rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary/40" /></label>
					<label class="block text-sm"><span class="text-muted-foreground text-xs">{$t('admin.archive.prefix')}</span><input bind:value={s3.prefix} autocomplete="off" class="mt-1 w-full px-3 py-2 bg-background border border-border rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary/40" /></label>
					<label class="flex items-center gap-2 text-sm sm:mt-6"><input type="checkbox" bind:checked={s3.force_path_style} class="rounded border-border" />{$t('admin.archive.pathStyle')}</label>
				</div>
				<p class="mt-2 text-xs text-muted-foreground">{$t('admin.archive.layoutHint', { path: `${s3.prefix || 'diarum-audit'}/YYYY-MM-DD.log.gz` })}</p>
			</div>

			<div class="mt-5 flex flex-wrap items-center gap-2">
				<button onclick={save} disabled={saving || !dirty} class="px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed">{saving ? $t('common.saving') : $t('common.save')}</button>
				<button onclick={test} disabled={testing || !s3Complete} class="px-4 py-2 rounded-lg border border-border/60 text-sm hover:bg-muted/50 disabled:opacity-50 disabled:cursor-not-allowed">{testing ? $t('admin.archive.testing') : $t('admin.archive.test')}</button>
				{#if dirty}<span class="text-xs text-amber-600 dark:text-amber-400">{$t('admin.archive.unsaved')}</span>{/if}
			</div>
		</section>

		<!-- Local and pulled files -->
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6">
			<h3 class="font-semibold text-foreground">{$t('admin.archive.filesHeading')}</h3>
			<p class="text-xs text-muted-foreground mt-1 mb-3">{$t('admin.archive.filesDesc')}</p>
			{#if files.length === 0}
				<div class="py-6 text-center text-sm text-muted-foreground">{$t('admin.archive.noFiles')}</div>
			{:else}
				<ul class="divide-y divide-border/50">
					{#each [...localFiles, ...pulledFiles] as file (file.date + file.pulled)}
						<li class="flex flex-wrap items-center gap-2 py-2">
							<div class="min-w-0 flex-1">
								<div class="text-sm text-foreground">{dayLabel(file.date)}
									{#if file.pulled}<span class="ml-1 px-1.5 py-px rounded text-[11px] bg-sky-500/10 text-sky-700 dark:text-sky-300 ring-1 ring-inset ring-sky-500/20">{$t('admin.archive.pulledBadge')}</span>{/if}
								</div>
								<div class="text-xs text-muted-foreground tabular-nums">{formatBytes(file.size)}{#if file.pulled} · {$t('admin.archive.removedNextCleanup')}{/if}</div>
							</div>
							<button onclick={() => onview?.(file.date)} class="px-2 py-1 text-xs rounded-md border border-border/60 hover:bg-muted/50">{$t('admin.archive.view')}</button>
							<button onclick={() => download(file)} class="px-2 py-1 text-xs rounded-md border border-border/60 hover:bg-muted/50">{$t('admin.archive.download')}</button>
							{#if file.pulled}<button onclick={() => drop(file.date)} disabled={busyDate === file.date} class="px-2 py-1 text-xs rounded-md text-rose-600 dark:text-rose-400 hover:bg-rose-500/10 disabled:opacity-50">{$t('admin.archive.remove')}</button>{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</section>

		<!-- Remote archives -->
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6">
			<div class="flex flex-wrap items-start justify-between gap-2 mb-3">
				<div class="min-w-0">
					<h3 class="font-semibold text-foreground">{$t('admin.archive.archivesHeading')}</h3>
					<p class="text-xs text-muted-foreground mt-1">{$t('admin.archive.archivesDesc')}</p>
				</div>
				{#if archives.length > 8}
					<input type="search" bind:value={archiveFilter} placeholder="2026-09" class="w-32 px-2 py-1 bg-muted/50 rounded-md text-sm font-mono focus:outline-none focus:ring-2 focus:ring-primary/30" />
				{/if}
			</div>
			{#if !archivesEnabled}
				<div class="py-6 text-center text-sm text-muted-foreground">{$t('admin.archive.archiveOff')}</div>
			{:else if archivesError}
				<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm">{archivesError}</div>
			{:else if archives.length === 0}
				<div class="py-6 text-center text-sm text-muted-foreground">{$t('admin.archive.noArchives')}</div>
			{:else}
				<ul class="divide-y divide-border/50 max-h-[28rem] overflow-y-auto">
					{#each visibleArchives as archive (archive.key)}
						<li class="flex flex-wrap items-center gap-2 py-2">
							<div class="min-w-0 flex-1">
								<div class="text-sm text-foreground">{dayLabel(archive.date)}</div>
								<div class="text-xs text-muted-foreground tabular-nums">{formatBytes(archive.size)} gz
									{#if archive.local}· <span class="text-emerald-600 dark:text-emerald-400">{$t('admin.archive.stillLocal')}</span>{/if}
									{#if archive.pulled}· <span class="text-sky-600 dark:text-sky-400">{$t('admin.archive.pulledBadge')}</span>{/if}
								</div>
							</div>
							{#if archive.local || archive.pulled}
								<button onclick={() => onview?.(archive.date)} class="px-2 py-1 text-xs rounded-md border border-border/60 hover:bg-muted/50">{$t('admin.archive.view')}</button>
							{:else}
								<button onclick={() => pull(archive.date)} disabled={!!busyDate} class="px-2.5 py-1 text-xs rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50">{busyDate === archive.date ? $t('admin.archive.pulling') : $t('admin.archive.pull')}</button>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/if}
</div>
