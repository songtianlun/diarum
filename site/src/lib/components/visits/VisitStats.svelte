<script lang="ts">
	/**
	 * Visitor statistics dashboard: headline numbers, a daily chart, the most
	 * viewed entries, who reads the most and the full visit log. Shared by
	 * Settings › Statistics (the user's own diaries) and the admin console
	 * (any owner).
	 */
	import { onMount } from 'svelte';
	import {
		getVisitSummary,
		listVisitDiaries,
		listVisitors,
		type DiaryStat,
		type VisitScope,
		type VisitSummary,
		type VisitorStat
	} from '$lib/api/visits';
	import { t } from '$lib/i18n';
	import BarTimeline from '$lib/components/admin/BarTimeline.svelte';
	import BarList from '$lib/components/admin/BarList.svelte';
	import VisitLog from './VisitLog.svelte';
	import VisitDiaryModal from './VisitDiaryModal.svelte';
	import {
		visitorLabel,
		deviceLabel,
		clientLabel,
		reasonLabel,
		sourceLabel,
		dayLabel,
		relativeTime,
		formatNumber,
		dayStartISO,
		periodStart
	} from './visitFormat';

	let {
		scope,
		heading,
		description,
		retentionDays,
		dedupeSeconds
	}: {
		scope: VisitScope;
		heading?: string;
		description?: string;
		retentionDays?: number;
		dedupeSeconds?: number;
	} = $props();

	type Tab = 'overview' | 'diaries' | 'visitors' | 'logs';
	const TABS: Tab[] = ['overview', 'diaries', 'visitors', 'logs'];
	const RANGES = [
		{ id: 'd7', days: 7 },
		{ id: 'd30', days: 30 },
		{ id: 'd90', days: 90 },
		{ id: 'd365', days: 365 },
		{ id: 'all', days: 0 }
	];
	const RANGE_KEY = 'diarum_visit_range';

	let days = $state(30);
	let tab = $state<Tab>('overview');
	let summary = $state<VisitSummary | null>(null);
	let loading = $state(false);
	let error = $state('');
	let openDiary = $state<string | null>(null);
	let logFilter = $state<{ visitor?: string; device?: string; diary?: string }>({});
	let logKey = $state(0);

	// Entries tab
	let diarySort = $state('views');
	let diaryRows = $state<DiaryStat[]>([]);
	let diaryTotal = $state(0);
	let diaryLoading = $state(false);
	// Visitors tab
	let visitorRows = $state<VisitorStat[]>([]);
	let visitorTotal = $state(0);
	let visitorLoading = $state(false);
	const PAGE = 30;

	let start = $derived(periodStart(days));
	let seq = 0;

	onMount(() => {
		try {
			const raw = localStorage.getItem(RANGE_KEY);
			const stored = raw === null ? NaN : Number(raw);
			if (RANGES.some((r) => r.days === stored)) days = stored;
		} catch {
			// Storage unavailable: keep the default.
		}
	});

	$effect(() => {
		void loadSummary(days);
	});

	$effect(() => {
		if (tab === 'diaries') void loadDiaries(false, diarySort, start);
	});

	$effect(() => {
		if (tab === 'visitors') void loadVisitors(false, start);
	});

	function setRange(next: number) {
		days = next;
		try {
			localStorage.setItem(RANGE_KEY, String(next));
		} catch {
			// Not remembered; fine.
		}
	}

	async function loadSummary(d: number) {
		const mine = ++seq;
		loading = true;
		error = '';
		try {
			const data = await getVisitSummary(scope, d || 36500);
			if (mine !== seq) return;
			summary = data;
		} catch (e) {
			if (mine !== seq) return;
			error = e instanceof Error ? e.message : $t('visits.loadFailed');
		}
		if (mine === seq) loading = false;
	}

	async function loadDiaries(more: boolean, sort: string, from?: string) {
		diaryLoading = true;
		try {
			const data = await listVisitDiaries(scope, { sort, start: from, limit: PAGE, offset: more ? diaryRows.length : 0 });
			diaryRows = more ? [...diaryRows, ...data.diaries] : data.diaries;
			diaryTotal = data.total;
		} catch (e) {
			error = e instanceof Error ? e.message : $t('visits.loadFailed');
		}
		diaryLoading = false;
	}

	async function loadVisitors(more: boolean, from?: string) {
		visitorLoading = true;
		try {
			const data = await listVisitors(scope, { start: from, limit: PAGE, offset: more ? visitorRows.length : 0 });
			visitorRows = more ? [...visitorRows, ...data.visitors] : data.visitors;
			visitorTotal = data.total;
		} catch (e) {
			error = e instanceof Error ? e.message : $t('visits.loadFailed');
		}
		visitorLoading = false;
	}

	function refresh() {
		void loadSummary(days);
		if (tab === 'diaries') void loadDiaries(false, diarySort, start);
		if (tab === 'visitors') void loadVisitors(false, start);
		if (tab === 'logs') logKey++;
	}

	function showVisitor(v: VisitorStat) {
		logFilter = v.visitor_id ? { visitor: v.visitor_id } : { visitor: '-', device: v.device };
		logKey++;
		tab = 'logs';
	}

	function pickVisitorKey(key: string) {
		const v = summary?.top_visitors.find((row) => row.key === key);
		if (v) showVisitor(v);
	}

	function retentionText(value: number | undefined): string {
		if (value === undefined) return '';
		if (value === 0) return $t('visits.forever');
		if (value % 365 === 0) return $t('visits.retentionYears', { years: value / 365 });
		return $t('visits.retentionDays', { days: value });
	}

	let totals = $derived(summary?.totals);
	let timeline = $derived(
		(summary?.timeline ?? []).map((p) => ({ start: dayStartISO(p.day), count: p.ok + p.failed, errors: p.failed }))
	);
	let diaryBars = $derived(
		(summary?.top_diaries ?? []).map((d) => ({
			key: d.diary_date,
			label: d.diary_date ? dayLabel(d.diary_date) : $t('visits.unattributed'),
			count: d.views,
			hint: d.others ? $t('visits.othersCount', { count: d.others }) : d.failed ? $t('visits.failedCount', { count: d.failed }) : undefined,
			tone: d.others ? 'text-amber-600 dark:text-amber-400' : 'text-rose-600 dark:text-rose-400'
		}))
	);
	let visitorBars = $derived(
		(summary?.top_visitors ?? []).map((v) => ({
			key: v.key,
			label: visitorLabel(v, scope) + (v.visitor_id ? '' : ` · ${deviceLabel(v.device)}`),
			count: v.views,
			hint: v.failed ? $t('visits.failedCount', { count: v.failed }) : undefined,
			tone: 'text-rose-600 dark:text-rose-400'
		}))
	);
	let cards = $derived(
		totals
			? [
					{ key: 'views', value: totals.views, sub: $t('visits.okFailed', { ok: formatNumber(totals.ok), failed: formatNumber(totals.failed) }), tone: '' },
					{ key: 'visitors', value: totals.visitors, sub: $t('visits.devicesCount', { count: formatNumber(totals.devices) }), tone: '' },
					{ key: 'failed', value: totals.failed, sub: $t('visits.ipsCount', { count: formatNumber(totals.ips) }), tone: totals.failed ? 'text-rose-600 dark:text-rose-400' : '' },
					{ key: 'others', value: totals.others, sub: `${$t('visits.cards.anonymous')} ${formatNumber(totals.anonymous)}`, tone: totals.others ? 'text-amber-600 dark:text-amber-400' : '' }
				]
			: []
	);
	const card = 'rounded-xl border border-border/50 bg-muted/30 px-3.5 sm:px-4 py-3';
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0 flex-1">
			<h2 class="text-lg font-semibold text-foreground">{heading ?? $t('visits.heading')}</h2>
			<p class="text-sm text-muted-foreground mt-1">{description ?? $t('visits.description')}</p>
		</div>
		<button type="button" onclick={refresh} disabled={loading} title={$t('visits.refresh')} aria-label={$t('visits.refresh')} class="flex-shrink-0 p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50">
			<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
		</button>
	</div>

	<div class="flex flex-wrap items-center justify-between gap-2">
		<div class="flex flex-wrap gap-1 rounded-lg bg-muted/50 p-1" role="group" aria-label={$t('visits.rangeLabel')}>
			{#each RANGES as range}
				<button type="button" onclick={() => setRange(range.days)} aria-pressed={days === range.days} class="px-2.5 py-1 rounded-md text-xs whitespace-nowrap transition-colors {days === range.days ? 'bg-card text-foreground shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}">{$t(`visits.ranges.${range.id}`)}</button>
			{/each}
		</div>
		{#if dedupeSeconds !== undefined && retentionDays !== undefined}
			<p class="text-[11px] text-muted-foreground">{$t('visits.hint', { seconds: dedupeSeconds, retention: retentionText(retentionDays) })}</p>
		{/if}
	</div>

	{#if error && !summary}
		<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm flex items-center justify-between gap-3"><span>{error}</span><button type="button" onclick={refresh} class="underline whitespace-nowrap">{$t('visits.retry')}</button></div>
	{:else if !summary}
		<div class="grid grid-cols-2 lg:grid-cols-4 gap-3">{#each Array(4) as _}<div class="h-20 rounded-xl bg-muted/40 animate-pulse"></div>{/each}</div>
		<div class="h-40 rounded-xl bg-muted/40 animate-pulse"></div>
	{:else if totals}
		<div class="grid grid-cols-2 lg:grid-cols-4 gap-3 {loading ? 'opacity-60' : ''} transition-opacity">
			{#each cards as c (c.key)}
				<div class={card}>
					<div class="text-xs text-muted-foreground truncate">{$t(`visits.cards.${c.key === 'others' && scope.kind === 'admin' ? 'othersOwner' : c.key}`)}</div>
					<div class="mt-1 text-2xl font-semibold tabular-nums tracking-tight truncate {c.tone || 'text-foreground'}">{formatNumber(c.value)}</div>
					<div class="mt-0.5 text-[11px] text-muted-foreground truncate">{c.sub}</div>
				</div>
			{/each}
		</div>

		{#if totals.views > 0 && scope.kind === 'self'}
			{#if totals.others > 0}
				<button type="button" onclick={() => { logFilter = {}; logKey++; tab = 'logs'; }} class="w-full text-left flex items-start gap-2.5 p-3 rounded-lg bg-amber-500/10 border border-amber-500/20 text-sm text-amber-800 dark:text-amber-200 hover:bg-amber-500/15 transition-colors">
					<svg class="w-4 h-4 mt-0.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" /></svg>
					<span>{$t('visits.alertOthers', { count: formatNumber(totals.others) })}</span>
				</button>
			{:else}
				<div class="flex items-start gap-2.5 p-3 rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-sm text-emerald-800 dark:text-emerald-200">
					<svg class="w-4 h-4 mt-0.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" /></svg>
					<span>{$t('visits.allClear')}</span>
				</div>
			{/if}
		{/if}

		<nav class="-mx-1 px-1 overflow-x-auto" aria-label={heading ?? $t('visits.heading')}>
			<div class="inline-flex min-w-full sm:min-w-0 p-1 rounded-xl bg-muted/60 gap-1">
				{#each TABS as id}
					<button type="button" onclick={() => { if (id === 'logs' && tab !== 'logs') logFilter = {}; tab = id; }} aria-current={tab === id ? 'page' : undefined} class="flex-1 sm:flex-none px-3 sm:px-4 py-1.5 rounded-lg text-sm whitespace-nowrap transition-all {tab === id ? 'bg-card text-foreground shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}">{$t(`visits.tabs.${id}`)}</button>
				{/each}
			</div>
		</nav>

		{#if tab === 'overview'}
			{#if totals.views === 0}
				<div class="rounded-xl border border-dashed border-border/60 px-4 py-10 text-center text-sm text-muted-foreground">{$t('visits.empty')}</div>
			{:else}
				{#if timeline.length}
					<div class="rounded-xl border border-border/50 p-4">
						<div class="text-sm font-medium text-foreground mb-3">{$t('visits.timeline')}</div>
						<BarTimeline points={timeline} bucket="day" height={170} requestsLabel={$t('visits.timelineViews')} errorsLabel={$t('visits.timelineFailed')} emptyLabel={$t('visits.empty')} />
					</div>
				{/if}
				<div class="grid gap-4 md:grid-cols-2">
					<div class="rounded-xl border border-border/50 p-4 min-w-0">
						<div class="flex items-center justify-between gap-2 mb-2">
							<div class="text-sm font-medium text-foreground">{$t('visits.topDiaries')}</div>
							<button type="button" onclick={() => (tab = 'diaries')} class="text-xs text-muted-foreground hover:text-foreground">{$t('visits.more')} →</button>
						</div>
						<BarList rows={diaryBars} emptyLabel={$t('visits.empty')} onpick={(key) => key && (openDiary = key)} />
					</div>
					<div class="rounded-xl border border-border/50 p-4 min-w-0">
						<div class="flex items-center justify-between gap-2 mb-2">
							<div class="text-sm font-medium text-foreground">{$t('visits.topVisitors')}</div>
							<button type="button" onclick={() => (tab = 'visitors')} class="text-xs text-muted-foreground hover:text-foreground">{$t('visits.more')} →</button>
						</div>
						<BarList rows={visitorBars} emptyLabel={$t('visits.empty')} barClass="bg-sky-500/15" onpick={pickVisitorKey} />
					</div>
				</div>
				<div class="grid gap-4 md:grid-cols-3">
					<div class="rounded-xl border border-border/50 p-4 min-w-0">
						<div class="text-sm font-medium text-foreground mb-2">{$t('visits.sources')}</div>
						<BarList rows={summary.sources.map((s) => ({ key: s.key, label: sourceLabel(s.key), count: s.count }))} emptyLabel="—" barClass="bg-violet-500/15" />
						{#if summary.reasons.length}
							<div class="text-sm font-medium text-foreground mt-4 mb-2">{$t('visits.reasons')}</div>
							<BarList rows={summary.reasons.map((s) => ({ key: s.key, label: reasonLabel(s.key), count: s.count }))} emptyLabel="—" barClass="bg-rose-500/15" />
						{/if}
					</div>
					<div class="rounded-xl border border-border/50 p-4 min-w-0 md:col-span-2">
						<div class="flex items-center justify-between gap-2 mb-2">
							<div class="text-sm font-medium text-foreground">{$t('visits.recentFailures')}</div>
							{#if summary.recent_failures.length}<button type="button" onclick={() => { logFilter = {}; logKey++; tab = 'logs'; }} class="text-xs text-muted-foreground hover:text-foreground">{$t('visits.more')} →</button>{/if}
						</div>
						{#if summary.recent_failures.length === 0}
							<div class="py-6 text-center text-xs text-muted-foreground">{$t('visits.noFailures')}</div>
						{:else}
							<ul class="divide-y divide-border/50">
								{#each summary.recent_failures as v (v.id)}
									<li class="py-2 flex items-start gap-2 text-sm">
										<span class="mt-1.5 h-2 w-2 flex-shrink-0 rounded-full bg-rose-500" aria-hidden="true"></span>
										<div class="min-w-0 flex-1">
											<div class="flex flex-wrap items-center gap-x-2">
												<span class="font-medium text-foreground truncate">{visitorLabel(v, scope)}</span>
												<span class="text-xs text-rose-600 dark:text-rose-400">{reasonLabel(v.reason)}</span>
												<span class="ml-auto text-xs text-muted-foreground whitespace-nowrap">{relativeTime(v.time)}</span>
											</div>
											<div class="text-xs text-muted-foreground truncate">{v.diary_date ? dayLabel(v.diary_date) : $t('visits.unattributed')} · {clientLabel(v.ua)}{#if v.ip} · <span class="font-mono">{v.ip}</span>{/if}</div>
										</div>
									</li>
								{/each}
							</ul>
						{/if}
					</div>
				</div>
			{/if}
		{:else if tab === 'diaries'}
			<div class="flex items-center justify-between gap-2">
				<span class="text-xs text-muted-foreground tabular-nums">{$t('visits.showing', { shown: formatNumber(diaryRows.length), total: formatNumber(diaryTotal) })}</span>
				<select bind:value={diarySort} class="px-2.5 py-1.5 bg-background rounded-lg text-xs border border-border/60 focus:outline-none focus:ring-2 focus:ring-primary/40">
					{#each ['views', 'last', 'others', 'failed', 'visitors', 'date'] as s}<option value={s}>{$t(`visits.sort.${s}`)}</option>{/each}
				</select>
			</div>
			{#if !diaryRows.length && diaryLoading}
				<div class="space-y-2">{#each Array(4) as _}<div class="h-14 rounded-lg bg-muted/40 animate-pulse"></div>{/each}</div>
			{:else if !diaryRows.length}
				<div class="rounded-xl border border-dashed border-border/60 px-4 py-10 text-center text-sm text-muted-foreground">{$t('visits.empty')}</div>
			{:else}
				<ul class="divide-y divide-border/50 rounded-xl border border-border/50 overflow-hidden">
					{#each diaryRows as d, i (d.owner + d.diary_date)}
						<li>
							<button type="button" onclick={() => d.diary_date && (openDiary = d.diary_date)} disabled={!d.diary_date} class="w-full text-left px-3 py-2.5 flex items-center gap-3 bg-card hover:bg-muted/40 transition-colors disabled:cursor-default">
								<span class="w-6 text-xs text-muted-foreground tabular-nums text-right">{i + 1}</span>
								<span class="min-w-0 flex-1">
									<span class="block text-sm font-medium text-foreground truncate">{d.diary_date ? dayLabel(d.diary_date, true) : $t('visits.unattributed')}</span>
									<span class="block text-xs text-muted-foreground truncate">
										{$t('visits.visitorsCount', { count: d.visitors })}
										{#if d.others}· <span class="text-amber-600 dark:text-amber-400">{$t('visits.othersCount', { count: d.others })}</span>{/if}
										{#if d.failed}· <span class="text-rose-600 dark:text-rose-400">{$t('visits.failedCount', { count: d.failed })}</span>{/if}
										· {$t('visits.lastSeen', { when: relativeTime(d.last) })}
									</span>
								</span>
								<span class="text-right flex-shrink-0">
									<span class="block text-base font-semibold tabular-nums">{formatNumber(d.views)}</span>
									<span class="block text-[11px] text-muted-foreground">{$t('visits.cards.views')}</span>
								</span>
							</button>
						</li>
					{/each}
				</ul>
				{#if diaryRows.length < diaryTotal}
					<button type="button" onclick={() => loadDiaries(true, diarySort, start)} disabled={diaryLoading} class="w-full py-2 text-sm rounded-lg border border-border/60 hover:bg-muted/50 disabled:opacity-50">{diaryLoading ? $t('visits.loading') : $t('visits.more')}</button>
				{/if}
			{/if}
		{:else if tab === 'visitors'}
			<span class="block text-xs text-muted-foreground tabular-nums">{$t('visits.showing', { shown: formatNumber(visitorRows.length), total: formatNumber(visitorTotal) })}</span>
			{#if !visitorRows.length && visitorLoading}
				<div class="space-y-2">{#each Array(4) as _}<div class="h-14 rounded-lg bg-muted/40 animate-pulse"></div>{/each}</div>
			{:else if !visitorRows.length}
				<div class="rounded-xl border border-dashed border-border/60 px-4 py-10 text-center text-sm text-muted-foreground">{$t('visits.empty')}</div>
			{:else}
				<ul class="divide-y divide-border/50 rounded-xl border border-border/50 overflow-hidden">
					{#each visitorRows as v, i (v.key)}
						<li>
							<button type="button" onclick={() => showVisitor(v)} class="w-full text-left px-3 py-2.5 flex items-center gap-3 bg-card hover:bg-muted/40 transition-colors">
								<span class="w-6 text-xs text-muted-foreground tabular-nums text-right">{i + 1}</span>
								<span class="flex-shrink-0 w-8 h-8 rounded-full flex items-center justify-center text-xs font-semibold {v.self ? 'bg-primary/10 text-primary' : v.visitor_id ? 'bg-amber-500/15 text-amber-700 dark:text-amber-300' : 'bg-rose-500/10 text-rose-700 dark:text-rose-300'}" aria-hidden="true">
									{v.visitor_id ? (v.visitor || '?').charAt(0).toUpperCase() : '?'}
								</span>
								<span class="min-w-0 flex-1">
									<span class="block text-sm font-medium text-foreground truncate">{visitorLabel(v, scope)}{#if !v.visitor_id} · <span class="font-normal text-muted-foreground">{deviceLabel(v.device)}</span>{/if}</span>
									<span class="block text-xs text-muted-foreground truncate">
										{$t('visits.diariesCount', { count: v.diaries })} · {v.visitor_id ? $t('visits.devicesCount', { count: v.devices }) + ' · ' : ''}{$t('visits.ipsCount', { count: v.ips })} · {clientLabel(v.last_ua)}{#if v.last_ip} · <span class="font-mono">{v.last_ip}</span>{/if}
									</span>
									<span class="block text-[11px] text-muted-foreground/80 truncate">{$t('visits.firstSeen', { when: relativeTime(v.first) })} · {$t('visits.lastSeen', { when: relativeTime(v.last) })}</span>
								</span>
								<span class="text-right flex-shrink-0">
									<span class="block text-base font-semibold tabular-nums">{formatNumber(v.views)}</span>
									{#if v.failed}<span class="block text-[11px] text-rose-600 dark:text-rose-400">{$t('visits.failedCount', { count: v.failed })}</span>{:else}<span class="block text-[11px] text-muted-foreground">{$t('visits.cards.views')}</span>{/if}
								</span>
							</button>
						</li>
					{/each}
				</ul>
				{#if visitorRows.length < visitorTotal}
					<button type="button" onclick={() => loadVisitors(true, start)} disabled={visitorLoading} class="w-full py-2 text-sm rounded-lg border border-border/60 hover:bg-muted/50 disabled:opacity-50">{visitorLoading ? $t('visits.loading') : $t('visits.more')}</button>
				{/if}
			{/if}
		{:else}
			{#key logKey}
				<VisitLog {scope} fixed={logFilter} {start} onopendiary={(date) => (openDiary = date)} onclearfixed={() => { logFilter = {}; logKey++; }} />
			{/key}
		{/if}
	{/if}
</div>

{#if openDiary}
	<VisitDiaryModal {scope} date={openDiary} {start} onclose={() => (openDiary = null)} />
{/if}
