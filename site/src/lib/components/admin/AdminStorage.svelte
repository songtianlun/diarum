<script lang="ts">
	/**
	 * Admin → S3 storage: the one system S3 connection, its global prefix, and
	 * whether users may back up to it. The audit archive, the visit archive
	 * and user backups pick it with their storage selector.
	 */
	import { onMount } from 'svelte';
	import { getSystemS3, saveSystemS3, testSystemS3, type S3Config, type SystemS3Response } from '$lib/api/admin';
	import S3Fields from '$lib/components/storage/S3Fields.svelte';
	import { t } from '$lib/i18n';

	const DEFAULT_PREFIX = 'diarum';

	let response = $state<SystemS3Response | null>(null);
	let s3 = $state<S3Config>({ bucket: '', region: '', endpoint: '', access_key: '', secret: '', force_path_style: false, prefix: DEFAULT_PREFIX });
	let allowBackups = $state(false);
	let saved = $state('');

	let loading = $state(true);
	let error = $state('');
	let saving = $state(false);
	let testing = $state(false);
	let message = $state<{ ok: boolean; text: string } | null>(null);

	onMount(() => {
		void load();
	});

	function snapshot() {
		return JSON.stringify({ s3, allowBackups });
	}

	function apply(next: SystemS3Response) {
		response = next;
		s3 = { ...next.s3, secret: '' };
		allowBackups = next.allow_user_backups;
		saved = snapshot();
	}

	async function load() {
		loading = true;
		error = '';
		try {
			apply(await getSystemS3());
		} catch (e) {
			error = e instanceof Error ? e.message : $t('admin.loadFailed');
		}
		loading = false;
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
			apply(await saveSystemS3({ s3, allow_user_backups: allowBackups }));
			flash(true, $t('s3.admin.saved'));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('s3.admin.saveFailed'));
		}
		saving = false;
	}

	async function test() {
		testing = true;
		try {
			await testSystemS3({ s3, allow_user_backups: allowBackups });
			flash(true, $t('s3.admin.testOk'));
		} catch (e) {
			flash(false, e instanceof Error ? e.message : $t('s3.admin.testFailed'));
		}
		testing = false;
	}

	function clean(prefix: string): string {
		return prefix
			.split(/[\\/]+/)
			.map((part) => part.trim())
			.filter((part) => part && part !== '.' && part !== '..')
			.join('/');
	}

	let dirty = $derived(response !== null && snapshot() !== saved);
	let complete = $derived(!!(s3.bucket.trim() && s3.region.trim() && s3.access_key.trim() && (s3.secret || response?.secret_set)));
	let root = $derived(clean(s3.prefix) || DEFAULT_PREFIX);
	let layout = $derived([
		{ label: $t('s3.admin.layoutAudit'), path: `${root}/audit/YYYY-MM-DD.log.gz` },
		{ label: $t('s3.admin.layoutVisits'), path: `${root}/visits/YYYY/YYYY-MM-DD.jsonl.gz` },
		{ label: $t('s3.admin.layoutBackups'), path: `${root}/user-backups/<user-id>/<backup>.zip` }
	]);
	let usedBy = $derived(
		(response?.used_by ?? []).map((use) => (use === 'audit' ? $t('s3.admin.useAudit') : $t('s3.admin.useVisits'))).concat(response?.configured && response.allow_user_backups ? [$t('s3.admin.layoutBackups')] : [])
	);
	// Moving the bucket or prefix leaves what is already written behind.
	let relocating = $derived(!!response?.configured && usedBy.length > 0 && (s3.bucket.trim() !== response.s3.bucket || (clean(s3.prefix) || DEFAULT_PREFIX) !== response.s3.prefix));
</script>

<div class="space-y-4">
	{#if message}
		<div class="sticky top-12 z-10 p-3 rounded-lg text-sm shadow-sm animate-fade-in break-words {message.ok ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border border-emerald-500/20' : 'bg-destructive/10 text-destructive border border-destructive/20'}">{message.text}</div>
	{/if}

	{#if error}
		<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm flex items-center justify-between gap-3"><span>{error}</span><button onclick={load} class="underline">{$t('admin.retry')}</button></div>
	{:else if loading && !response}
		<div class="space-y-3">{#each Array(2) as _}<div class="h-32 rounded-xl bg-muted/40 animate-pulse"></div>{/each}</div>
	{:else if response}
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6 animate-fade-in">
			<div class="flex items-start justify-between gap-3">
				<div class="min-w-0">
					<h2 class="text-lg font-semibold text-foreground flex flex-wrap items-center gap-2">
						{$t('s3.admin.heading')}
						<span class="px-2 py-0.5 rounded-full text-[11px] font-medium ring-1 ring-inset {response.configured ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 ring-emerald-500/20' : 'bg-muted text-muted-foreground ring-border'}">{response.configured ? $t('s3.admin.statusReady') : $t('s3.admin.statusMissing')}</span>
					</h2>
					<p class="text-sm text-muted-foreground mt-1">{$t('s3.admin.description')}</p>
				</div>
				<button onclick={load} disabled={loading} title={$t('admin.refresh')} aria-label={$t('admin.refresh')} class="flex-shrink-0 p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50">
					<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
				</button>
			</div>
			{#if response.configured}
				<div class="mt-3 text-xs text-muted-foreground">
					{$t('s3.admin.usedBy')}: <span class="text-foreground">{usedBy.length ? usedBy.join(' · ') : $t('s3.admin.usedByNone')}</span>
				</div>
			{/if}
		</section>

		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6">
			<h3 class="font-semibold text-foreground mb-4">{$t('s3.admin.connection')}</h3>
			<S3Fields bind:s3 secretSet={response.secret_set} prefixLabel={$t('s3.admin.globalPrefix')} prefixPlaceholder={DEFAULT_PREFIX} prefixHint={$t('s3.admin.globalPrefixHint')} />

			<div class="mt-5 rounded-lg bg-muted/40 p-3">
				<div class="text-xs font-medium text-foreground mb-2">{$t('s3.admin.layout')}</div>
				<dl class="grid gap-1.5 text-xs">
					{#each layout as row}
						<div class="flex flex-col sm:flex-row sm:gap-3">
							<dt class="text-muted-foreground sm:w-40 shrink-0">{row.label}</dt>
							<dd class="font-mono text-foreground break-all">{row.path}</dd>
						</div>
					{/each}
				</dl>
			</div>

			<label class="mt-5 pt-4 border-t border-border/50 flex items-start justify-between gap-4 cursor-pointer">
				<span>
					<span class="block text-sm font-medium text-foreground">{$t('s3.admin.allowBackups')}</span>
					<span class="block text-xs text-muted-foreground mt-0.5">{$t('s3.admin.allowBackupsDesc')}</span>
				</span>
				<input type="checkbox" bind:checked={allowBackups} class="mt-1 h-5 w-5 rounded border-border accent-[hsl(var(--primary))]" />
			</label>

			{#if relocating}
				<p class="mt-4 p-2.5 rounded-lg bg-amber-500/10 text-amber-700 dark:text-amber-300 text-xs">{$t('s3.admin.changeWarning')}</p>
			{/if}

			<div class="mt-5 flex flex-wrap items-center gap-2">
				<button onclick={save} disabled={saving || !dirty} class="px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed">{saving ? $t('common.saving') : $t('common.save')}</button>
				<button onclick={test} disabled={testing || !complete} class="px-4 py-2 rounded-lg border border-border/60 text-sm hover:bg-muted/50 disabled:opacity-50 disabled:cursor-not-allowed">{testing ? $t('s3.admin.testing') : $t('s3.admin.test')}</button>
				{#if dirty}<span class="text-xs text-amber-600 dark:text-amber-400">{$t('s3.admin.unsaved')}</span>{/if}
			</div>
			{#if response.configured}
				<p class="mt-3 text-xs text-muted-foreground">{$t('s3.admin.removeHint')}</p>
			{/if}
		</section>
	{/if}
</div>
