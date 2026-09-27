<script lang="ts">
	/**
	 * Admin → Overview: installation-wide counts, recent activity charts,
	 * the last 24 hours of API traffic and the health of the audit writer.
	 */
	import { onMount } from 'svelte';
	import { getOverview, queryAudit, formatBytes, type Overview, type AuditStats } from '$lib/api/admin';
	import LineChart from '$lib/components/stats/LineChart.svelte';
	import BarTimeline from './BarTimeline.svelte';
	import { getIntlLocale, t } from '$lib/i18n';

	let { onnavigate }: { onnavigate?: (tab: 'users' | 'audit', filter?: Record<string, string>) => void } = $props();

	let overview = $state<Overview | null>(null);
	let recent = $state<AuditStats | null>(null);
	let loading = $state(true);
	let error = $state('');

	onMount(() => {
		void load();
	});

	async function load() {
		loading = true;
		error = '';
		try {
			const end = new Date();
			const start = new Date(end.getTime() - 24 * 3600 * 1000);
			const [ov, audit] = await Promise.all([
				getOverview(),
				queryAudit({ start, end, limit: 0, stats: true }).catch(() => null)
			]);
			overview = ov;
			recent = audit?.stats ?? null;
		} catch (e) {
			error = e instanceof Error ? e.message : $t('admin.loadFailed');
		}
		loading = false;
	}

	function n(value: number): string {
		return value.toLocaleString(getIntlLocale());
	}

	function uptime(seconds: number): string {
		const d = Math.floor(seconds / 86400);
		const h = Math.floor((seconds % 86400) / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		if (d > 0) return $t('admin.overview.uptimeDays', { d, h });
		if (h > 0) return $t('admin.overview.uptimeHours', { h, m });
		return $t('admin.overview.uptimeMinutes', { m });
	}

	function series(points: { date: string; count: number }[]) {
		return points.map((p) => ({ label: p.date.slice(5).replace('-', '/'), title: p.date, value: p.count }));
	}

	let cards = $derived(
		overview
			? [
					{ key: 'users', value: overview.stats.users, sub: $t('admin.overview.adminsCount', { count: overview.stats.admins }), tab: 'users' as const },
					{ key: 'activeUsers', value: overview.stats.active_users_7d, sub: $t('admin.overview.newUsers', { count: overview.stats.new_users_30d }) },
					{ key: 'diaries', value: overview.stats.diaries, sub: $t('admin.overview.diariesRecent', { today: overview.stats.diaries_today, week: overview.stats.diaries_7d }) },
					{ key: 'revisions', value: overview.stats.revisions },
					{ key: 'media', value: overview.stats.media },
					{ key: 'conversations', value: overview.stats.conversations, sub: $t('admin.overview.messagesCount', { count: overview.stats.messages }) },
					{ key: 'database', value: -1, display: formatBytes(overview.stats.database_bytes) },
					{ key: 'auditSize', value: -1, display: formatBytes(overview.audit.bytes), sub: $t('admin.overview.auditFiles', { count: overview.audit.files }) }
				]
			: []
	);

	let errorRate = $derived(recent && recent.total > 0 ? ((recent.client_errors + recent.server_errors) / recent.total) * 100 : 0);
</script>

{#snippet cardBody(card: { key: string; value: number; display?: string; sub?: string })}
	<div class="text-xs text-muted-foreground truncate">{$t(`admin.overview.cards.${card.key}`)}</div>
	<div class="mt-1 text-2xl font-semibold tabular-nums text-foreground tracking-tight truncate">{card.display ?? n(card.value)}</div>
	{#if card.sub}<div class="mt-0.5 text-[11px] text-muted-foreground truncate">{card.sub}</div>{/if}
{/snippet}

<div class="space-y-6">
	{#if error}
		<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm flex items-center justify-between gap-3">
			<span>{error}</span>
			<button onclick={load} class="underline whitespace-nowrap">{$t('admin.retry')}</button>
		</div>
	{:else if loading && !overview}
		<div class="grid grid-cols-2 lg:grid-cols-4 gap-3">
			{#each Array(8) as _}<div class="h-24 rounded-xl bg-muted/40 animate-pulse"></div>{/each}
		</div>
	{:else if overview}
		<!-- Headline numbers -->
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6 animate-fade-in">
			<div class="flex items-start justify-between gap-3 mb-4">
				<div class="min-w-0">
					<h2 class="text-lg font-semibold text-foreground">{$t('admin.overview.heading')}</h2>
					<p class="text-sm text-muted-foreground mt-1">{$t('admin.overview.description')}</p>
				</div>
				<button onclick={load} disabled={loading} title={$t('admin.refresh')} aria-label={$t('admin.refresh')} class="flex-shrink-0 p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50">
					<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
				</button>
			</div>
			<div class="grid grid-cols-2 lg:grid-cols-4 gap-3">
				{#each cards as card (card.key)}
					{#if card.tab}
						<button type="button" onclick={() => onnavigate?.(card.tab!)} class="text-left rounded-xl border border-border/50 bg-muted/30 px-4 py-3.5 min-w-0 hover:border-primary/40 hover:bg-muted/50 transition-colors">{@render cardBody(card)}</button>
					{:else}
						<div class="rounded-xl border border-border/50 bg-muted/30 px-4 py-3.5 min-w-0">{@render cardBody(card)}</div>
					{/if}
				{/each}
			</div>
		</section>

		<!-- Last 24 hours of traffic -->
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6 animate-fade-in">
			<div class="flex flex-wrap items-start justify-between gap-3 mb-4">
				<div class="min-w-0">
					<h3 class="font-semibold text-foreground">{$t('admin.overview.last24h')}</h3>
					<p class="text-xs text-muted-foreground mt-0.5">{$t('admin.overview.last24hDesc')}</p>
				</div>
				<button onclick={() => onnavigate?.('audit')} class="text-sm text-primary hover:underline">{$t('admin.overview.viewLogs')} →</button>
			</div>
			{#if recent}
				<div class="grid grid-cols-2 sm:grid-cols-5 gap-2 mb-4">
					<div class="rounded-lg bg-muted/40 px-3 py-2"><div class="text-[11px] text-muted-foreground">{$t('admin.audit.kpi.requests')}</div><div class="text-lg font-semibold tabular-nums">{n(recent.total)}</div></div>
					<div class="rounded-lg bg-muted/40 px-3 py-2"><div class="text-[11px] text-muted-foreground">{$t('admin.audit.kpi.users')}</div><div class="text-lg font-semibold tabular-nums">{n(recent.users)}</div></div>
					<div class="rounded-lg bg-muted/40 px-3 py-2"><div class="text-[11px] text-muted-foreground">{$t('admin.audit.kpi.errorRate')}</div><div class="text-lg font-semibold tabular-nums {errorRate >= 5 ? 'text-rose-600 dark:text-rose-400' : ''}">{errorRate.toFixed(1)}%</div></div>
					<button onclick={() => onnavigate?.('audit', { category: 'auth.login_failed,auth.denied,auth.forbidden' })} class="text-left rounded-lg bg-muted/40 px-3 py-2 hover:bg-muted/70 transition-colors"><div class="text-[11px] text-muted-foreground">{$t('admin.audit.kpi.failedLogins')}</div><div class="text-lg font-semibold tabular-nums {recent.login_failed > 0 ? 'text-amber-600 dark:text-amber-400' : ''}">{n(recent.login_failed)}</div></button>
					<div class="col-span-2 sm:col-span-1 rounded-lg bg-muted/40 px-3 py-2"><div class="text-[11px] text-muted-foreground">{$t('admin.audit.kpi.p95')}</div><div class="text-lg font-semibold tabular-nums">{n(recent.p95_ms)} ms</div></div>
				</div>
				<BarTimeline points={recent.timeline} bucket={recent.bucket} requestsLabel={$t('admin.audit.kpi.requests')} errorsLabel={$t('admin.audit.kpi.errors')} emptyLabel={$t('admin.audit.noTraffic')} />
			{:else}
				<div class="py-8 text-center text-sm text-muted-foreground">{$t('admin.audit.unavailable')}</div>
			{/if}
		</section>

		<!-- Activity charts -->
		<div class="grid gap-6 lg:grid-cols-2">
			<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-5">
				<h3 class="font-semibold text-foreground mb-3 text-sm">{$t('admin.overview.dailyDiaries')}</h3>
				<LineChart points={series(overview.stats.daily_diaries)} height={180} showDots emptyLabel={$t('admin.overview.noData')} />
			</section>
			<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-5">
				<h3 class="font-semibold text-foreground mb-3 text-sm">{$t('admin.overview.dailyUsers')}</h3>
				<LineChart points={series(overview.stats.daily_users)} height={180} showDots emptyLabel={$t('admin.overview.noData')} />
			</section>
		</div>

		<!-- System -->
		<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6">
			<h3 class="font-semibold text-foreground mb-3">{$t('admin.overview.system')}</h3>
			<dl class="grid grid-cols-[auto_1fr] sm:grid-cols-[auto_1fr_auto_1fr] gap-x-4 gap-y-2 text-sm">
				<dt class="text-muted-foreground">{$t('admin.overview.version')}</dt><dd class="font-mono break-all">{overview.system.version}</dd>
				<dt class="text-muted-foreground">{$t('admin.overview.uptime')}</dt><dd>{uptime(overview.system.uptime_seconds)}</dd>
				<dt class="text-muted-foreground">{$t('admin.overview.timezone')}</dt><dd>{overview.system.server_timezone}</dd>
				<dt class="text-muted-foreground">{$t('admin.overview.runtime')}</dt><dd class="font-mono">{overview.system.go_version} · {overview.system.goroutines}</dd>
				<dt class="text-muted-foreground">{$t('admin.overview.auditWriter')}</dt>
				<dd class="sm:col-span-3">
					{#if !overview.audit.enabled}
						<span class="text-rose-600 dark:text-rose-400">{$t('admin.overview.auditDisabled')}</span>
					{:else}
						{$t('admin.overview.auditWriterStats', { written: n(overview.audit.writer.written), queued: n(overview.audit.writer.queued) })}
						{#if overview.audit.writer.dropped > 0}
							<span class="ml-1 text-rose-600 dark:text-rose-400 font-medium">{$t('admin.overview.auditDropped', { count: n(overview.audit.writer.dropped) })}</span>
						{/if}
					{/if}
				</dd>
			</dl>
		</section>
	{/if}
</div>
