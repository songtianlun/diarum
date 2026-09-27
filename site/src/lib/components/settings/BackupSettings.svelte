<script lang="ts">
	/**
	 * Automatic backups to S3 (Settings > Data Management): destination,
	 * schedule and retention, plus the list of backups with their logs.
	 * Backups and restores run on the server; this polls their status.
	 */
	import { onDestroy, onMount } from 'svelte';
	import {
		defaultBackupSettings,
		deleteBackup,
		downloadBackup,
		getBackupLog,
		getBackupSettings,
		getBackupStatus,
		listBackups,
		previewSchedule,
		restoreBackup,
		saveBackupSettings,
		startBackup,
		testBackupDestination,
		MAX_BACKUP_KEEP,
		MIN_BACKUP_KEEP,
		type BackupEntry,
		type BackupJob,
		type BackupLog,
		type BackupSettings
	} from '$lib/api/backup';
	import { getImageUploadSettings } from '$lib/api/imageUpload';
	import { getIntlLocale, t } from '$lib/i18n';

	const POLL_MS = 2000;
	const PREVIEW_DELAY_MS = 400;

	const presets = [
		{ key: 'presetDaily2', cron: '0 2 * * *' },
		{ key: 'presetDaily4', cron: '0 4 * * *' },
		{ key: 'presetEvery6h', cron: '0 */6 * * *' },
		{ key: 'presetEvery12h', cron: '0 */12 * * *' },
		{ key: 'presetWeekly', cron: '0 3 * * 0' },
		{ key: 'presetMonthly', cron: '0 3 1 * *' }
	];

	const browserZone = typeof Intl !== 'undefined' ? Intl.DateTimeFormat().resolvedOptions().timeZone : '';
	const zones: string[] = (() => {
		try {
			return (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('timeZone') ?? [];
		} catch {
			return [];
		}
	})();

	let loading = true;
	let settings: BackupSettings = structuredClone(defaultBackupSettings);
	let original: BackupSettings = structuredClone(defaultBackupSettings);
	let serverZone = '';
	let nextRun = '';

	let saving = false;
	let saveError = '';
	let saveSuccess = '';
	let copyNote = '';

	let testing = false;
	let testResult: { success: boolean; message: string } | null = null;

	let preview: { valid: boolean; error?: string; runs: string[] } | null = null;
	let previewTimer: ReturnType<typeof setTimeout> | undefined;
	let previewKey = '';

	let backups: BackupEntry[] = [];
	let listLoading = false;
	let listError = '';
	let actionError = '';

	let job: BackupJob | null = null;
	let pollTimer: ReturnType<typeof setTimeout> | undefined;
	let destroyed = false;

	let openLogId = '';
	let logs = new Map<string, BackupLog | 'error'>();
	let confirming: { id: string; action: 'restore' | 'delete' } | null = null;
	let busyId = '';

	$: dirty = JSON.stringify(settings) !== JSON.stringify(original);
	$: active = original.enabled && !dirty;
	$: running = job?.status === 'running';
	$: storagePath = `s3://${settings.s3.bucket || 'bucket'}/${settings.s3.prefix.replace(/^\/+|\/+$/g, '') ? settings.s3.prefix.replace(/^\/+|\/+$/g, '') + '/' : ''}<user-id>/`;
	$: schedulePreviewNeeded = settings.enabled && settings.auto_enabled;
	$: if (schedulePreviewNeeded) queuePreview(settings.schedule, settings.timezone);

	onMount(async () => {
		try {
			const loaded = await getBackupSettings();
			const { next_run, server_timezone, ...rest } = loaded;
			settings = structuredClone(rest);
			original = structuredClone(rest);
			serverZone = server_timezone;
			nextRun = next_run ?? '';
		} catch (e) {
			saveError = e instanceof Error ? e.message : $t('backup.loadFailed');
		}
		loading = false;
		if (original.enabled) {
			await Promise.all([refreshStatus(), loadBackups()]);
		}
	});

	onDestroy(() => {
		destroyed = true;
		clearTimeout(pollTimer);
		clearTimeout(previewTimer);
	});

	function queuePreview(schedule: string, timezone: string) {
		const key = `${schedule}|${timezone}`;
		if (key === previewKey) return;
		previewKey = key;
		clearTimeout(previewTimer);
		previewTimer = setTimeout(async () => {
			try {
				const result = await previewSchedule(schedule, timezone);
				if (previewKey !== key) return;
				preview = { valid: result.valid, error: result.error, runs: result.next_runs };
			} catch (e) {
				if (previewKey !== key) return;
				preview = { valid: false, error: e instanceof Error ? e.message : String(e), runs: [] };
			}
		}, PREVIEW_DELAY_MS);
	}

	function formatTime(value: string | undefined): string {
		if (!value) return '—';
		const date = new Date(value);
		if (isNaN(date.getTime())) return value;
		return date.toLocaleString(getIntlLocale(), {
			year: 'numeric',
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			weekday: 'short'
		});
	}

	function formatZoneTime(value: string, zone: string): string {
		const date = new Date(value);
		if (isNaN(date.getTime())) return value;
		try {
			return date.toLocaleString(getIntlLocale(), {
				timeZone: zone || undefined,
				year: 'numeric',
				month: 'short',
				day: 'numeric',
				hour: '2-digit',
				minute: '2-digit',
				weekday: 'short'
			});
		} catch {
			return formatTime(value);
		}
	}

	function formatBytes(bytes: number): string {
		if (bytes < 1024) return `${bytes} B`;
		const units = ['KB', 'MB', 'GB', 'TB'];
		let value = bytes / 1024;
		let unit = 0;
		while (value >= 1024 && unit < units.length - 1) {
			value /= 1024;
			unit++;
		}
		return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unit]}`;
	}

	function formatDuration(ms: number): string {
		if (ms < 1000) return `${ms} ms`;
		const seconds = Math.round(ms / 1000);
		if (seconds < 60) return `${seconds} s`;
		const minutes = Math.floor(seconds / 60);
		return `${minutes} min ${seconds % 60} s`;
	}

	function stageLabel(stage: string): string {
		const key = `backup.stage.${stage}`;
		const label = $t(key);
		return label === key ? stage : label;
	}

	function setAuto(enabled: boolean) {
		settings.auto_enabled = enabled;
		if (enabled && !settings.timezone && browserZone) {
			settings.timezone = browserZone;
		}
	}

	async function copyFromImageUpload() {
		copyNote = '';
		const image = await getImageUploadSettings();
		if (!image.s3.bucket) {
			copyNote = $t('backup.noImageUploadS3');
			return;
		}
		settings.s3 = { ...image.s3, prefix: settings.s3.prefix };
		copyNote = $t('backup.copied');
	}

	async function handleTest() {
		testing = true;
		testResult = null;
		try {
			testResult = await testBackupDestination(settings.s3);
		} catch (e) {
			testResult = { success: false, message: e instanceof Error ? e.message : String(e) };
		}
		testing = false;
	}

	async function handleSave() {
		saving = true;
		saveError = '';
		saveSuccess = '';
		try {
			const keep = Math.round(Number(settings.keep));
			settings.keep = Math.min(MAX_BACKUP_KEEP, Math.max(MIN_BACKUP_KEEP, Number.isFinite(keep) ? keep : MIN_BACKUP_KEEP));
			const saved = await saveBackupSettings(settings);
			const { next_run, server_timezone, ...rest } = saved;
			settings = structuredClone(rest);
			original = structuredClone(rest);
			serverZone = server_timezone;
			nextRun = next_run ?? '';
			saveSuccess = $t('backup.saved');
			setTimeout(() => (saveSuccess = ''), 3000);
			if (original.enabled) {
				await Promise.all([refreshStatus(), loadBackups()]);
			}
		} catch (e) {
			saveError = e instanceof Error ? e.message : $t('backup.saveFailed');
		}
		saving = false;
	}

	async function loadBackups() {
		listLoading = true;
		listError = '';
		try {
			backups = await listBackups();
			logs = new Map();
		} catch (e) {
			listError = e instanceof Error ? e.message : $t('backup.loadFailed');
		}
		listLoading = false;
	}

	async function refreshStatus() {
		clearTimeout(pollTimer);
		try {
			const status = await getBackupStatus();
			const wasRunning = job?.status === 'running';
			job = status.job;
			nextRun = status.next_run ?? '';
			if (wasRunning && job?.status !== 'running') {
				await loadBackups();
			}
		} catch {
			// Keep polling through transient errors while a job runs.
		}
		if (!destroyed && job?.status === 'running') {
			pollTimer = setTimeout(refreshStatus, POLL_MS);
		}
	}

	async function handleBackupNow() {
		actionError = '';
		try {
			job = await startBackup();
			pollTimer = setTimeout(refreshStatus, POLL_MS);
		} catch (e) {
			actionError = e instanceof Error ? e.message : String(e);
		}
	}

	async function toggleLog(entry: BackupEntry) {
		if (openLogId === entry.id) {
			openLogId = '';
			return;
		}
		openLogId = entry.id;
		if (!entry.has_log || logs.has(entry.id)) return;
		try {
			const log = await getBackupLog(entry.id);
			logs = new Map(logs).set(entry.id, log);
		} catch {
			logs = new Map(logs).set(entry.id, 'error');
		}
	}

	async function handleDownload(id: string) {
		actionError = '';
		busyId = id;
		try {
			await downloadBackup(id);
		} catch (e) {
			actionError = e instanceof Error ? e.message : String(e);
		}
		busyId = '';
	}

	async function handleConfirm() {
		if (!confirming) return;
		const { id, action } = confirming;
		confirming = null;
		actionError = '';
		busyId = id;
		try {
			if (action === 'delete') {
				await deleteBackup(id);
				backups = backups.filter((b) => b.id !== id);
				if (openLogId === id) openLogId = '';
			} else {
				job = await restoreBackup(id);
				pollTimer = setTimeout(refreshStatus, POLL_MS);
			}
		} catch (e) {
			actionError = e instanceof Error ? e.message : String(e);
		}
		busyId = '';
	}

	const inputClass = 'w-full px-3 py-2 bg-muted rounded-lg text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary';
	const smallButton = 'px-3 py-1.5 text-xs rounded-lg bg-muted hover:bg-muted/70 text-foreground transition-colors disabled:opacity-50';
</script>

<div class="py-4 border-t border-border/50 mt-2" id="automatic-backup">
	<div class="flex items-start justify-between gap-4">
		<div>
			<div class="font-medium text-foreground">{$t('backup.title')}</div>
			<div class="text-sm text-muted-foreground mt-1 max-w-2xl">{$t('backup.description')}</div>
		</div>
		<label class="relative inline-flex items-center cursor-pointer shrink-0 mt-1" title={$t('backup.enableDesc')}>
			<span class="sr-only">{$t('backup.enable')}</span>
			<input type="checkbox" class="sr-only peer" bind:checked={settings.enabled} disabled={loading} />
			<div class="w-11 h-6 bg-muted rounded-full peer-checked:bg-primary transition-colors after:content-[''] after:absolute after:top-0.5 after:left-0.5 after:bg-background after:rounded-full after:h-5 after:w-5 after:transition-transform peer-checked:after:translate-x-5"></div>
		</label>
	</div>

	{#if loading}
		<div class="mt-4 text-sm text-muted-foreground">{$t('common.loading')}</div>
	{:else}
		{#if settings.enabled}
			<!-- Destination -->
			<div class="mt-5 rounded-xl border border-border/50 p-4 space-y-4">
				<div class="flex items-center justify-between gap-3 flex-wrap">
					<div class="font-medium text-foreground text-sm">{$t('backup.destination')}</div>
					<button type="button" on:click={copyFromImageUpload} class={smallButton}>{$t('backup.copyFromImageUpload')}</button>
				</div>
				{#if copyNote}
					<div class="text-xs text-muted-foreground">{copyNote}</div>
				{/if}
				<div class="grid gap-4 md:grid-cols-2">
					<div>
						<label for="backup-bucket" class="block text-sm font-medium text-foreground mb-1.5">{$t('backup.bucket')}</label>
						<input id="backup-bucket" type="text" bind:value={settings.s3.bucket} class={inputClass} autocomplete="off" />
					</div>
					<div>
						<label for="backup-region" class="block text-sm font-medium text-foreground mb-1.5">{$t('backup.region')}</label>
						<input id="backup-region" type="text" bind:value={settings.s3.region} placeholder="us-east-1" class={inputClass} autocomplete="off" />
					</div>
					<div>
						<label for="backup-endpoint" class="block text-sm font-medium text-foreground mb-1.5">{$t('backup.endpoint')}</label>
						<input id="backup-endpoint" type="text" bind:value={settings.s3.endpoint} placeholder="https://s3.amazonaws.com" class={inputClass} autocomplete="off" />
					</div>
					<div class="flex items-end">
						<label class="inline-flex items-center gap-2 text-sm text-foreground pb-2">
							<input type="checkbox" bind:checked={settings.s3.force_path_style} class="rounded border-border text-primary focus:ring-primary" />
							{$t('backup.pathStyle')}
						</label>
					</div>
					<div>
						<label for="backup-access-key" class="block text-sm font-medium text-foreground mb-1.5">{$t('backup.accessKey')}</label>
						<input id="backup-access-key" type="text" bind:value={settings.s3.access_key} class={inputClass} autocomplete="off" />
					</div>
					<div>
						<label for="backup-secret" class="block text-sm font-medium text-foreground mb-1.5">{$t('backup.secret')}</label>
						<input id="backup-secret" type="password" bind:value={settings.s3.secret} class={inputClass} autocomplete="new-password" />
					</div>
					<div class="md:col-span-2">
						<label for="backup-prefix" class="block text-sm font-medium text-foreground mb-1.5">{$t('backup.prefix')}</label>
						<input id="backup-prefix" type="text" bind:value={settings.s3.prefix} placeholder="diarum-backups" class={inputClass} autocomplete="off" />
						<p class="text-xs text-muted-foreground mt-1 break-all">{$t('backup.prefixHint', { path: storagePath })}</p>
					</div>
				</div>
				<div class="flex items-center justify-between gap-4 rounded-lg bg-muted/40 p-3">
					<div class="text-sm text-muted-foreground">{$t('backup.testDesc')}</div>
					<button
						type="button"
						on:click={handleTest}
						disabled={testing || !settings.s3.bucket || !settings.s3.region || !settings.s3.access_key || !settings.s3.secret}
						class="px-4 py-2 text-sm bg-background hover:bg-background/80 rounded-lg transition-colors disabled:opacity-50 shrink-0"
					>
						{testing ? $t('backup.testing') : $t('backup.test')}
					</button>
				</div>
				{#if testResult}
					<div class="p-3 rounded-lg text-sm break-words {testResult.success ? 'bg-green-500/10 text-green-600' : 'bg-destructive/10 text-destructive'}">
						{testResult.message}
					</div>
				{/if}
			</div>

			<!-- Schedule & retention -->
			<div class="mt-4 rounded-xl border border-border/50 p-4 space-y-4">
				<div class="flex items-start justify-between gap-4">
					<div>
						<div class="font-medium text-foreground text-sm">{$t('backup.autoEnable')}</div>
						<div class="text-xs text-muted-foreground mt-0.5">{$t('backup.autoEnableDesc')}</div>
					</div>
					<label class="relative inline-flex items-center cursor-pointer shrink-0">
						<span class="sr-only">{$t('backup.autoEnable')}</span>
						<input type="checkbox" class="sr-only peer" checked={settings.auto_enabled} on:change={(e) => setAuto(e.currentTarget.checked)} />
						<div class="w-11 h-6 bg-muted rounded-full peer-checked:bg-primary transition-colors after:content-[''] after:absolute after:top-0.5 after:left-0.5 after:bg-background after:rounded-full after:h-5 after:w-5 after:transition-transform peer-checked:after:translate-x-5"></div>
					</label>
				</div>

				{#if settings.auto_enabled}
					<div>
						<div class="text-xs text-muted-foreground mb-2">{$t('backup.presets')}</div>
						<div class="flex flex-wrap gap-2">
							{#each presets as preset}
								<button
									type="button"
									on:click={() => (settings.schedule = preset.cron)}
									class="px-3 py-1.5 rounded-lg text-xs border transition-colors {settings.schedule === preset.cron ? 'bg-primary text-primary-foreground border-primary' : 'bg-card text-foreground border-border/60 hover:bg-muted/50'}"
								>
									{$t(`backup.${preset.key}`)}
								</button>
							{/each}
						</div>
					</div>
					<div class="grid gap-4 md:grid-cols-2">
						<div>
							<label for="backup-cron" class="block text-sm font-medium text-foreground mb-1.5">{$t('backup.cron')}</label>
							<input id="backup-cron" type="text" bind:value={settings.schedule} spellcheck="false" autocomplete="off" class="{inputClass} font-mono" />
							<p class="text-xs text-muted-foreground mt-1">{$t('backup.cronHint')}</p>
						</div>
						<div>
							<label for="backup-timezone" class="block text-sm font-medium text-foreground mb-1.5">{$t('backup.timezone')}</label>
							<input id="backup-timezone" type="text" list="backup-timezones" bind:value={settings.timezone} placeholder={$t('backup.timezoneServer', { zone: serverZone })} class={inputClass} autocomplete="off" />
							<datalist id="backup-timezones">
								{#each zones as zone}<option value={zone}></option>{/each}
							</datalist>
							{#if browserZone && settings.timezone !== browserZone}
								<button type="button" class="text-xs text-primary hover:underline mt-1" on:click={() => (settings.timezone = browserZone)}>
									{$t('backup.useBrowserZone', { zone: browserZone })}
								</button>
							{/if}
						</div>
					</div>
					{#if preview}
						{#if preview.valid}
							<div class="rounded-lg bg-muted/40 p-3">
								<div class="text-xs font-medium text-muted-foreground mb-1.5">{$t('backup.nextRuns')}</div>
								<ul class="text-sm text-foreground space-y-0.5 tabular-nums">
									{#each preview.runs as runAt}
										<li>{formatZoneTime(runAt, settings.timezone || serverZone)}</li>
									{/each}
								</ul>
							</div>
						{:else}
							<div class="p-3 rounded-lg text-sm bg-destructive/10 text-destructive">{preview.error}</div>
						{/if}
					{/if}
				{/if}

				<div class="pt-1">
					<label for="backup-keep" class="block text-sm font-medium text-foreground mb-1.5">{$t('backup.keep')}</label>
					<input id="backup-keep" type="number" min={MIN_BACKUP_KEEP} max={MAX_BACKUP_KEEP} step="1" bind:value={settings.keep} class="{inputClass} max-w-[8rem] tabular-nums" />
					<p class="text-xs text-muted-foreground mt-1">{$t('backup.keepHint')}</p>
				</div>
			</div>
		{/if}

		{#if saveError}
			<div class="mt-4 p-3 bg-destructive/10 text-destructive rounded-lg text-sm break-words">{saveError}</div>
		{/if}
		{#if dirty || saveSuccess}
			<div class="mt-4 flex items-center gap-3">
				<button
					type="button"
					on:click={handleSave}
					disabled={saving || !dirty}
					class="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
				>
					{saving ? $t('common.saving') : $t('backup.save')}
				</button>
				{#if saveSuccess}
					<span class="text-sm text-green-600 animate-fade-in">{saveSuccess}</span>
				{:else if settings.enabled && !original.enabled}
					<span class="text-sm text-muted-foreground">{$t('backup.unsaved')}</span>
				{/if}
			</div>
		{/if}

		<!-- Backups -->
		{#if active}
			<div class="mt-5 rounded-xl border border-border/50">
				<div class="flex items-center justify-between gap-3 flex-wrap p-4 border-b border-border/50">
					<div>
						<div class="font-medium text-foreground text-sm">{$t('backup.backups')}</div>
						<div class="text-xs text-muted-foreground mt-0.5">
							{nextRun ? $t('backup.nextRun', { time: formatTime(nextRun) }) : $t('backup.noSchedule')}
						</div>
					</div>
					<div class="flex items-center gap-2">
						<button type="button" on:click={loadBackups} disabled={listLoading} class={smallButton}>{$t('backup.refresh')}</button>
						<button
							type="button"
							on:click={handleBackupNow}
							disabled={running}
							class="px-3 py-1.5 text-xs rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
						>
							{$t('backup.backupNow')}
						</button>
					</div>
				</div>

				{#if job}
					<div class="px-4 pt-4">
						{#if job.status === 'running'}
							<div class="flex items-center gap-2 p-3 rounded-lg bg-primary/5 text-sm text-foreground">
								<svg class="w-4 h-4 animate-spin text-primary shrink-0" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
								</svg>
								{job.kind === 'restore' ? $t('backup.restoring', { stage: stageLabel(job.stage) }) : $t('backup.running', { stage: stageLabel(job.stage) })}
							</div>
						{:else if job.kind === 'restore'}
							<div class="p-3 rounded-lg text-sm break-words {job.status === 'success' ? 'bg-green-500/10 text-green-600' : 'bg-destructive/10 text-destructive'}">
								{#if job.status === 'success' && job.import_stats}
									{$t('backup.lastRestoreOk', {
										time: formatTime(job.finished_at),
										diaries: job.import_stats.diaries.imported,
										media: job.import_stats.media.imported,
										conversations: job.import_stats.conversations.imported,
										skipped: job.import_stats.diaries.skipped
									})}
								{:else}
									{$t('backup.lastRestoreFailed', { time: formatTime(job.finished_at), error: job.error ?? '' })}
								{/if}
							</div>
						{:else if job.status === 'failed'}
							<div class="p-3 rounded-lg text-sm bg-destructive/10 text-destructive break-words">
								{$t('backup.lastBackupFailed', { time: formatTime(job.finished_at), error: job.error ?? '' })}
							</div>
						{:else}
							<div class="text-xs text-muted-foreground">{$t('backup.lastBackupOk', { time: formatTime(job.finished_at) })}</div>
						{/if}
					</div>
				{/if}

				{#if actionError}
					<div class="mx-4 mt-4 p-3 bg-destructive/10 text-destructive rounded-lg text-sm break-words">{actionError}</div>
				{/if}

				<div class="p-4">
					{#if listError}
						<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm break-words">{listError}</div>
					{:else if listLoading && backups.length === 0}
						<div class="text-sm text-muted-foreground">{$t('common.loading')}</div>
					{:else if backups.length === 0}
						<div class="text-sm text-muted-foreground">{$t('backup.empty')}</div>
					{:else}
						<ul class="divide-y divide-border/50">
							{#each backups as entry (entry.id)}
								<li class="py-3 first:pt-0 last:pb-0">
									<div class="flex items-start justify-between gap-3 flex-wrap">
										<div class="min-w-0">
											<div class="flex items-center gap-2 flex-wrap">
												<span class="text-sm font-medium text-foreground tabular-nums">{formatTime(entry.created_at)}</span>
												<span class="px-1.5 py-0.5 rounded text-[11px] font-medium {entry.status === 'success' ? 'bg-green-500/10 text-green-600' : entry.status === 'missing' ? 'bg-amber-500/10 text-amber-600' : 'bg-destructive/10 text-destructive'}">
													{$t(`backup.status.${entry.status}`)}
												</span>
												{#if entry.trigger}
													<span class="px-1.5 py-0.5 rounded text-[11px] bg-muted text-muted-foreground">{$t(`backup.trigger.${entry.trigger}`)}</span>
												{/if}
											</div>
											<div class="text-xs text-muted-foreground mt-1 tabular-nums">
												{#if entry.has_archive}
													{formatBytes(entry.size)} ·
													{$t('backup.summary', { diaries: entry.diaries, media: entry.media, conversations: entry.conversations })}
												{/if}
												{#if entry.duration_ms}
													{entry.has_archive ? '·' : ''} {$t('backup.duration', { duration: formatDuration(entry.duration_ms) })}
												{/if}
											</div>
											{#if entry.error}
												<div class="text-xs text-destructive mt-1 break-words">{entry.error}</div>
											{/if}
										</div>
										<div class="flex items-center gap-1.5 flex-wrap">
											<button type="button" class={smallButton} on:click={() => toggleLog(entry)}>
												{openLogId === entry.id ? $t('backup.hideLog') : $t('backup.viewLog')}
											</button>
											{#if entry.has_archive}
												<button type="button" class={smallButton} disabled={busyId === entry.id} on:click={() => handleDownload(entry.id)}>{$t('backup.download')}</button>
												<button type="button" class={smallButton} disabled={running} on:click={() => (confirming = { id: entry.id, action: 'restore' })}>{$t('backup.restore')}</button>
											{/if}
											<button type="button" class="{smallButton} hover:!bg-destructive/10 hover:text-destructive" disabled={busyId === entry.id} on:click={() => (confirming = { id: entry.id, action: 'delete' })}>{$t('backup.delete')}</button>
										</div>
									</div>

									{#if confirming?.id === entry.id}
										<div class="mt-3 p-3 rounded-lg text-sm {confirming.action === 'delete' ? 'bg-destructive/10 text-destructive' : 'bg-amber-500/10 text-amber-700 dark:text-amber-400'}">
											<p>{confirming.action === 'delete' ? $t('backup.confirmDelete') : $t('backup.confirmRestore')}</p>
											<div class="mt-2 flex gap-2">
												<button type="button" on:click={handleConfirm} class="px-3 py-1.5 text-xs rounded-lg {confirming.action === 'delete' ? 'bg-destructive text-destructive-foreground' : 'bg-primary text-primary-foreground'}">{$t('backup.confirm')}</button>
												<button type="button" on:click={() => (confirming = null)} class={smallButton}>{$t('backup.cancel')}</button>
											</div>
										</div>
									{/if}

									{#if openLogId === entry.id}
										{@const log = logs.get(entry.id)}
										<div class="mt-3 rounded-lg bg-muted/50 p-3 text-xs">
											{#if !entry.has_log}
												<div class="text-muted-foreground">{$t('backup.noLog')}</div>
											{:else if log === 'error'}
												<div class="text-destructive">{$t('backup.logFailed')}</div>
											{:else if !log}
												<div class="text-muted-foreground">{$t('backup.logLoading')}</div>
											{:else}
												<div class="font-mono space-y-0.5 max-h-72 overflow-auto">
													{#each log.entries as line}
														<div class="flex gap-2 {line.level === 'error' ? 'text-destructive' : line.level === 'warn' ? 'text-amber-600' : 'text-foreground'}">
															<span class="text-muted-foreground shrink-0 tabular-nums">{new Date(line.time).toLocaleTimeString(getIntlLocale())}</span>
															<span class="break-all">{line.message}</span>
														</div>
													{/each}
												</div>
												{#if log.sha256 || log.app_version}
													<div class="mt-2 pt-2 border-t border-border/50 text-muted-foreground space-y-0.5 break-all">
														{#if log.sha256}<div>{$t('backup.checksum')}: <span class="font-mono">{log.sha256}</span></div>{/if}
														{#if log.app_version}<div>{$t('backup.version')}: {log.app_version}</div>{/if}
													</div>
												{/if}
											{/if}
										</div>
									{/if}
								</li>
							{/each}
						</ul>
					{/if}
				</div>
			</div>
		{/if}
	{/if}
</div>
