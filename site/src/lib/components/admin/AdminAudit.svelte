<script lang="ts">
	/**
	 * Admin → Audit log: statistics over the selected window (traffic, errors,
	 * sign-in failures, busiest users, addresses and endpoints) above a
	 * searchable, filterable list of every recorded API call.
	 */
	import { onDestroy, onMount, untrack } from 'svelte';
	import {
		queryAudit,
		listUsers,
		listAuditFiles,
		shortUA,
		type AuditEntry,
		type AuditStats,
		type AdminUser
	} from '$lib/api/admin';
	import { formatDate, parseDate } from '$lib/utils/date';
	import { getIntlLocale, t, locale } from '$lib/i18n';
	import BarTimeline from './BarTimeline.svelte';
	import BarList from './BarList.svelte';
	import { CATEGORIES, SOURCES, METHODS, STATUSES, TONE_CLASS, toneOf, actionKey, statusTone, isDate } from './auditLabels';

	type Range = '1h' | '24h' | '3d' | '7d' | 'all' | 'custom';

	let {
		initial = {}
	}: {
		initial?: { user?: string; category?: string; ip?: string; day?: string };
	} = $props();

	// Read once: the page remounts this view whenever it hands in new filters.
	const init = untrack(() => initial);
	const PAGE = 50;
	const RANGES: Range[] = ['1h', '24h', '3d', '7d', 'all', 'custom'];
	const STATS_KEY = 'diarum_admin_audit_stats';

	// A day picked elsewhere (the archive list) opens as a custom range.
	let range = $state<Range>(init.day ? 'custom' : '24h');
	let customStart = $state(init.day ?? formatDate(new Date(Date.now() - 2 * 86400000)));
	let customEnd = $state(init.day ?? formatDate(new Date()));
	let q = $state('');
	let user = $state(init.user ?? '');
	let category = $state(init.category ?? '');
	let status = $state('');
	let method = $state('');
	let source = $state('');
	let ip = $state(init.ip ?? '');
	let includePulled = $state(true);
	let showFilters = $state(false);
	let showStats = $state(readStatsPref());

	let entries = $state<AuditEntry[]>([]);
	let stats = $state<AuditStats | null>(null);
	let total = $state(0);
	let scanned = $state({ files: 0, bytes: 0, took: 0 });
	let loading = $state(true);
	let loadingMore = $state(false);
	let error = $state('');
	let expanded = $state(new Set<string>());
	let users = $state<AdminUser[]>([]);
	let pulledDays = $state(0);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let token = 0;

	onMount(() => {
		void load();
		listUsers({ limit: 200, sort: 'username' })
			.then((r) => (users = r.users))
			.catch(() => {});
		listAuditFiles()
			.then((r) => (pulledDays = r.files.filter((f) => f.pulled).length))
			.catch(() => {});
	});
	onDestroy(() => clearTimeout(timer));

	function readStatsPref(): boolean {
		try {
			return localStorage.getItem(STATS_KEY) !== '0';
		} catch {
			return true;
		}
	}

	function toggleStats() {
		showStats = !showStats;
		try {
			localStorage.setItem(STATS_KEY, showStats ? '1' : '0');
		} catch {
			// Preference is a convenience only.
		}
	}

	function timeWindow(): { start?: Date; end?: Date } {
		const now = new Date();
		const hours: Record<string, number> = { '1h': 1, '24h': 24, '3d': 72, '7d': 168 };
		if (range in hours) return { start: new Date(now.getTime() - hours[range] * 3600000), end: now };
		if (range === 'custom') {
			const start = customStart ? parseDate(customStart) : undefined;
			let end: Date | undefined;
			if (customEnd) {
				end = parseDate(customEnd);
				end.setDate(end.getDate() + 1);
				end.setMilliseconds(-1);
			}
			return { start, end };
		}
		return {};
	}

	function baseQuery() {
		return {
			...timeWindow(),
			q,
			user,
			actions: category ? category.split(',') : undefined,
			status,
			method,
			source,
			ip,
			pulled: includePulled
		};
	}

	async function load() {
		const mine = ++token;
		loading = true;
		error = '';
		try {
			const result = await queryAudit({ ...baseQuery(), limit: PAGE, stats: true });
			if (mine !== token) return;
			entries = result.entries;
			stats = result.stats ?? null;
			total = result.total;
			scanned = { files: result.files, bytes: result.bytes, took: result.took_ms };
			expanded = new Set();
		} catch (e) {
			if (mine !== token) return;
			error = e instanceof Error ? e.message : $t('admin.loadFailed');
		} finally {
			if (mine === token) loading = false;
		}
	}

	async function loadMore() {
		const mine = token;
		loadingMore = true;
		try {
			const result = await queryAudit({ ...baseQuery(), limit: PAGE, offset: entries.length });
			if (mine !== token) return;
			entries = [...entries, ...result.entries];
			total = result.total;
		} catch (e) {
			error = e instanceof Error ? e.message : $t('admin.loadFailed');
		}
		loadingMore = false;
	}

	function reload() {
		clearTimeout(timer);
		void load();
	}

	function debounced() {
		clearTimeout(timer);
		timer = setTimeout(() => void load(), 300);
	}

	function setRange(next: Range) {
		range = next;
		reload();
	}

	function apply(patch: Partial<{ user: string; ip: string; category: string; status: string; method: string; source: string }>) {
		if (patch.user !== undefined) user = patch.user;
		if (patch.ip !== undefined) ip = patch.ip;
		if (patch.category !== undefined) category = patch.category;
		if (patch.status !== undefined) status = patch.status;
		if (patch.method !== undefined) method = patch.method;
		if (patch.source !== undefined) source = patch.source;
		reload();
		if (typeof document !== 'undefined') document.getElementById('audit-list')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
	}

	function clearFilters() {
		q = user = category = status = method = source = ip = '';
		reload();
	}

	function toggle(key: string) {
		const next = new Set(expanded);
		if (next.has(key)) next.delete(key);
		else next.add(key);
		expanded = next;
	}

	// ----- Presentation -----

	function n(value: number): string {
		return value.toLocaleString(getIntlLocale());
	}

	function actionLabel(action: string | undefined, _l?: string): string {
		if (!action) return '';
		const key = actionKey(action);
		const label = $t(key);
		return label === key ? action : label;
	}

	function userLabel(id: string): string {
		if (id === '-') return $t('admin.audit.anonymous');
		return users.find((u) => u.id === id)?.username || id;
	}

	function categoryLabel(id: string): string {
		const found = CATEGORIES.find((c) => c.id === id);
		return found ? $t(found.key) : actionLabel(id);
	}

	function entryKey(entry: AuditEntry, index: number) {
		return `${entry.time}-${entry.path ?? ''}-${entry.action ?? ''}-${index}`;
	}

	function timeOf(entry: AuditEntry): string {
		return new Date(entry.time).toLocaleTimeString(getIntlLocale(), { hour: '2-digit', minute: '2-digit', second: '2-digit' });
	}

	function dayLabel(day: string): string {
		const today = formatDate(new Date());
		const yesterday = formatDate(new Date(Date.now() - 86400000));
		const date = parseDate(day);
		const formatted = date.toLocaleDateString(getIntlLocale(), {
			month: 'long',
			day: 'numeric',
			weekday: 'short',
			year: date.getFullYear() === new Date().getFullYear() ? undefined : 'numeric'
		});
		if (day === today) return `${$t('common.today')} · ${formatted}`;
		if (day === yesterday) return `${$t('admin.audit.yesterday')} · ${formatted}`;
		return formatted;
	}

	function groupByDay(list: AuditEntry[], _l: string) {
		const groups: { day: string; label: string; items: { entry: AuditEntry; key: string }[] }[] = [];
		list.forEach((entry, index) => {
			const day = formatDate(new Date(entry.time));
			let group = groups[groups.length - 1];
			if (!group || group.day !== day) {
				group = { day, label: dayLabel(day), items: [] };
				groups.push(group);
			}
			group.items.push({ entry, key: entryKey(entry, index) });
		});
		return groups;
	}

	function highlight(text: string, needle: string): { text: string; hit: boolean }[] {
		const n = needle.trim().toLowerCase();
		if (!n) return [{ text, hit: false }];
		const parts: { text: string; hit: boolean }[] = [];
		const lower = text.toLowerCase();
		let from = 0;
		for (let at = lower.indexOf(n); at !== -1; at = lower.indexOf(n, from)) {
			if (at > from) parts.push({ text: text.slice(from, at), hit: false });
			parts.push({ text: text.slice(at, at + n.length), hit: true });
			from = at + n.length;
		}
		if (from < text.length) parts.push({ text: text.slice(from), hit: false });
		return parts;
	}

	function summary(entry: AuditEntry): string {
		const what = entry.action ? actionLabel(entry.action, $locale) : `${entry.method ?? ''} ${entry.route || entry.path || ''}`.trim();
		return entry.target ? `${what} · ${entry.target}` : what;
	}

	let grouped = $derived(groupByDay(entries, $locale));
	let activeFilters = $derived(
		[
			user && { id: 'user', label: `${$t('admin.audit.filters.user')}: ${userLabel(user)}` },
			category && { id: 'category', label: categoryLabel(category) },
			status && { id: 'status', label: `${$t('admin.audit.filters.status')}: ${status}` },
			method && { id: 'method', label: method },
			source && { id: 'source', label: `${$t('admin.audit.filters.source')}: ${$t(`admin.audit.sources.${source}`)}` },
			ip && { id: 'ip', label: `IP ${ip}` }
		].filter(Boolean) as { id: string; label: string }[]
	);
	let errorRate = $derived(stats && stats.total > 0 ? ((stats.client_errors + stats.server_errors) / stats.total) * 100 : 0);
	let suspicious = $derived((stats?.top_ips ?? []).filter((i) => i.failed_logins >= 5 || (i.errors >= 20 && i.errors / i.count > 0.5)));

	let kpis = $derived(
		stats
			? [
					{ key: 'requests', value: n(stats.total) },
					{ key: 'users', value: n(stats.users), sub: stats.anonymous ? $t('admin.audit.kpi.anonymousCount', { count: n(stats.anonymous) }) : '' },
					{ key: 'ips', value: n(stats.ips) },
					{ key: 'errorRate', value: `${errorRate.toFixed(1)}%`, sub: $t('admin.audit.kpi.errorSplit', { client: n(stats.client_errors), server: n(stats.server_errors) }), warn: errorRate >= 5, patch: { status: 'error' } },
					{ key: 'failedLogins', value: n(stats.login_failed), sub: $t('admin.audit.kpi.loginOk', { count: n(stats.login_ok) }), warn: stats.login_failed > 0, patch: { category: 'auth.login_failed' } },
					{ key: 'denied', value: n(stats.denied), warn: stats.denied > 0, patch: { category: 'auth.denied,auth.forbidden' } },
					{ key: 'writes', value: n(stats.writes) },
					{ key: 'p95', value: `${n(stats.p95_ms)} ms`, sub: $t('admin.audit.kpi.latency', { avg: stats.avg_ms.toFixed(0), max: n(stats.max_ms) }) }
				]
			: []
	);
</script>

{#snippet kpi(k: { key: string; value: string; sub?: string; warn?: boolean })}
	<div class="text-[11px] text-muted-foreground truncate">{$t(`admin.audit.kpi.${k.key}`)}</div>
	<div class="text-xl font-semibold tabular-nums truncate {k.warn ? 'text-amber-600 dark:text-amber-400' : 'text-foreground'}">{k.value}</div>
	{#if k.sub}<div class="text-[11px] text-muted-foreground truncate">{k.sub}</div>{/if}
{/snippet}

<div class="space-y-4">
	<!-- Controls -->
	<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-5 animate-fade-in">
		<div class="flex items-start justify-between gap-3 mb-3">
			<div class="min-w-0">
				<h2 class="text-lg font-semibold text-foreground">{$t('admin.audit.heading')}</h2>
				<p class="text-sm text-muted-foreground mt-1">{$t('admin.audit.description')}</p>
			</div>
			<button onclick={reload} disabled={loading} title={$t('admin.refresh')} aria-label={$t('admin.refresh')} class="flex-shrink-0 p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50">
				<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
			</button>
		</div>

		<!-- Time range -->
		<div class="flex gap-1.5 overflow-x-auto pb-1 -mx-1 px-1">
			{#each RANGES as id}
				<button onclick={() => setRange(id)} class="flex-shrink-0 px-3 py-1.5 rounded-lg text-sm border transition-colors {range === id ? 'bg-primary text-primary-foreground border-primary' : 'border-border/60 text-foreground hover:bg-muted/50'}">{$t(`admin.audit.ranges.${id}`)}</button>
			{/each}
		</div>
		{#if range === 'custom'}
			<div class="mt-2 flex flex-wrap items-center gap-2 text-sm">
				<input type="date" bind:value={customStart} max={customEnd || undefined} onchange={reload} aria-label={$t('admin.audit.from')} class="px-2 py-1.5 bg-background border border-border rounded-md focus:outline-none focus:ring-2 focus:ring-primary/40" />
				<span class="text-muted-foreground">→</span>
				<input type="date" bind:value={customEnd} min={customStart || undefined} onchange={reload} aria-label={$t('admin.audit.to')} class="px-2 py-1.5 bg-background border border-border rounded-md focus:outline-none focus:ring-2 focus:ring-primary/40" />
			</div>
		{/if}

		<!-- Search + filters -->
		<div class="mt-3 flex gap-2">
			<div class="relative flex-1 min-w-0">
				<svg class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
				<input type="search" bind:value={q} oninput={debounced} placeholder={$t('admin.audit.searchPlaceholder')} class="w-full pl-9 pr-3 py-2 bg-muted/50 border border-transparent rounded-lg text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:bg-background focus:border-border focus:ring-2 focus:ring-primary/30" />
			</div>
			<button onclick={() => (showFilters = !showFilters)} aria-expanded={showFilters} class="flex-shrink-0 inline-flex items-center gap-1.5 px-3 py-2 rounded-lg border text-sm transition-colors {showFilters || activeFilters.length ? 'border-primary/50 text-primary bg-primary/5' : 'border-border/60 text-foreground hover:bg-muted/50'}">
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.293A1 1 0 013 6.586V4z" /></svg>
				<span class="hidden sm:inline">{$t('admin.audit.filtersButton')}</span>
				{#if activeFilters.length}<span class="tabular-nums">{activeFilters.length}</span>{/if}
			</button>
		</div>

		{#if showFilters}
			<div class="mt-3 grid grid-cols-2 lg:grid-cols-3 gap-2 animate-fade-in">
				<label class="text-xs text-muted-foreground col-span-2 lg:col-span-1">{$t('admin.audit.filters.user')}
					<select bind:value={user} onchange={reload} class="mt-1 w-full px-2 py-1.5 bg-background border border-border/60 rounded-md text-sm text-foreground">
						<option value="">{$t('admin.audit.filters.anyUser')}</option>
						<option value="-">{$t('admin.audit.anonymous')}</option>
						{#each users as u (u.id)}<option value={u.id}>{u.username}</option>{/each}
						{#if user && user !== '-' && !users.some((u) => u.id === user)}<option value={user}>{user}</option>{/if}
					</select>
				</label>
				<label class="text-xs text-muted-foreground">{$t('admin.audit.filters.category')}
					<select bind:value={category} onchange={reload} class="mt-1 w-full px-2 py-1.5 bg-background border border-border/60 rounded-md text-sm text-foreground">
						{#each CATEGORIES as c (c.id)}<option value={c.id}>{$t(c.key)}</option>{/each}
						{#if category && !CATEGORIES.some((c) => c.id === category)}<option value={category}>{actionLabel(category)}</option>{/if}
					</select>
				</label>
				<label class="text-xs text-muted-foreground">{$t('admin.audit.filters.status')}
					<select bind:value={status} onchange={reload} class="mt-1 w-full px-2 py-1.5 bg-background border border-border/60 rounded-md text-sm text-foreground">
						<option value="">{$t('admin.audit.filters.any')}</option>
						{#each STATUSES as s}<option value={s}>{$t(`admin.audit.statuses.${s}`)}</option>{/each}
					</select>
				</label>
				<label class="text-xs text-muted-foreground">{$t('admin.audit.filters.method')}
					<select bind:value={method} onchange={reload} class="mt-1 w-full px-2 py-1.5 bg-background border border-border/60 rounded-md text-sm text-foreground">
						<option value="">{$t('admin.audit.filters.any')}</option>
						{#each METHODS as m}<option value={m}>{m}</option>{/each}
					</select>
				</label>
				<label class="text-xs text-muted-foreground">{$t('admin.audit.filters.source')}
					<select bind:value={source} onchange={reload} class="mt-1 w-full px-2 py-1.5 bg-background border border-border/60 rounded-md text-sm text-foreground">
						<option value="">{$t('admin.audit.filters.any')}</option>
						{#each SOURCES as s}<option value={s}>{$t(`admin.audit.sources.${s}`)}</option>{/each}
					</select>
				</label>
				<label class="text-xs text-muted-foreground col-span-2 lg:col-span-1">IP
					<input type="text" bind:value={ip} oninput={debounced} placeholder="203.0.113.7" class="mt-1 w-full px-2 py-1.5 bg-background border border-border/60 rounded-md text-sm text-foreground font-mono" />
				</label>
			</div>
		{/if}

		{#if activeFilters.length || pulledDays > 0}
			<div class="mt-3 flex flex-wrap items-center gap-1.5">
				{#each activeFilters as f (f.id)}
					<button onclick={() => apply({ [f.id]: '' })} class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs bg-primary/10 text-primary hover:bg-primary/15">
						{f.label}
						<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
					</button>
				{/each}
				{#if activeFilters.length > 1}<button onclick={clearFilters} class="text-xs text-muted-foreground hover:text-foreground underline">{$t('admin.audit.clearFilters')}</button>{/if}
				{#if pulledDays > 0}
					<label class="ml-auto inline-flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
						<input type="checkbox" bind:checked={includePulled} onchange={reload} class="rounded border-border" />
						{$t('admin.audit.includePulled', { count: pulledDays })}
					</label>
				{/if}
			</div>
		{/if}
	</section>

	{#if error}
		<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm flex items-center justify-between gap-3">
			<span>{error}</span>
			<button onclick={reload} class="underline whitespace-nowrap">{$t('admin.retry')}</button>
		</div>
	{/if}

	<!-- Statistics -->
	<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-5">
		<button onclick={toggleStats} aria-expanded={showStats} class="w-full flex items-center justify-between gap-2 text-left">
			<h3 class="font-semibold text-foreground">{$t('admin.audit.analysis')}</h3>
			<span class="flex items-center gap-2 text-xs text-muted-foreground">
				{#if stats}<span class="hidden sm:inline">{$t('admin.audit.scanned', { files: scanned.files, ms: scanned.took })}</span>{/if}
				<svg class="w-4 h-4 transition-transform {showStats ? 'rotate-180' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" /></svg>
			</span>
		</button>

		{#if showStats}
			{#if loading && !stats}
				<div class="mt-4 grid grid-cols-2 lg:grid-cols-4 gap-2">{#each Array(8) as _}<div class="h-16 rounded-lg bg-muted/40 animate-pulse"></div>{/each}</div>
			{:else if stats}
				<div class="mt-4 space-y-5 transition-opacity {loading ? 'opacity-60' : ''}">
					{#if suspicious.length}
						<div class="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm">
							<div class="font-medium text-amber-800 dark:text-amber-300">{$t('admin.audit.suspiciousTitle')}</div>
							<ul class="mt-1 space-y-0.5">
								{#each suspicious as s (s.ip)}
									<li class="text-amber-900/80 dark:text-amber-200/80">
										<button onclick={() => apply({ ip: s.ip })} class="font-mono underline decoration-dotted">{s.ip}</button>
										— {$t('admin.audit.suspiciousLine', { failed: n(s.failed_logins), errors: n(s.errors), count: n(s.count) })}
									</li>
								{/each}
							</ul>
						</div>
					{/if}

					<div class="grid grid-cols-2 lg:grid-cols-4 gap-2">
						{#each kpis as k (k.key)}
							{#if k.patch}
								<button type="button" onclick={() => apply(k.patch!)} class="text-left rounded-lg bg-muted/40 px-3 py-2.5 min-w-0 hover:bg-muted/70 transition-colors">{@render kpi(k)}</button>
							{:else}
								<div class="rounded-lg bg-muted/40 px-3 py-2.5 min-w-0">{@render kpi(k)}</div>
							{/if}
						{/each}
					</div>

					<div>
						<div class="flex items-center justify-between mb-2">
							<h4 class="text-sm font-medium text-foreground">{$t('admin.audit.timeline')}</h4>
							<span class="flex items-center gap-3 text-[11px] text-muted-foreground">
								<span class="inline-flex items-center gap-1"><span class="w-2 h-2 rounded-sm bg-primary/70"></span>{$t('admin.audit.kpi.requests')}</span>
								<span class="inline-flex items-center gap-1"><span class="w-2 h-2 rounded-sm bg-rose-500/85"></span>{$t('admin.audit.kpi.errors')}</span>
							</span>
						</div>
						<BarTimeline points={stats.timeline} bucket={stats.bucket} requestsLabel={$t('admin.audit.kpi.requests')} errorsLabel={$t('admin.audit.kpi.errors')} emptyLabel={$t('admin.audit.noTraffic')} />
					</div>

					<div class="grid gap-4 lg:grid-cols-2">
						<div class="rounded-lg border border-border/50 p-3">
							<h4 class="text-sm font-medium text-foreground mb-2">{$t('admin.audit.panels.actions')}</h4>
							<BarList
								rows={stats.actions.slice(0, 10).map((a) => ({ key: a.key, label: a.key === '-' ? $t('admin.audit.otherRequests') : actionLabel(a.key, $locale), count: a.count }))}
								onpick={(key) => apply({ category: key })}
								emptyLabel={$t('admin.audit.none')}
							/>
						</div>
						<div class="rounded-lg border border-border/50 p-3">
							<h4 class="text-sm font-medium text-foreground mb-2">{$t('admin.audit.panels.users')}</h4>
							<BarList
								rows={stats.top_users.map((u) => ({ key: u.key, label: u.label || u.key, count: u.count }))}
								onpick={(key) => apply({ user: key })}
								barClass="bg-sky-500/15"
								emptyLabel={$t('admin.audit.none')}
							/>
						</div>
						<div class="rounded-lg border border-border/50 p-3">
							<h4 class="text-sm font-medium text-foreground mb-2">{$t('admin.audit.panels.ips')}</h4>
							<BarList
								rows={stats.top_ips.map((i) => ({
									key: i.ip,
									label: i.ip,
									mono: true,
									count: i.count,
									hint: i.failed_logins ? $t('admin.audit.failedShort', { count: i.failed_logins }) : i.errors ? $t('admin.audit.errorsShort', { count: i.errors }) : i.users ? $t('admin.audit.usersShort', { count: i.users }) : '',
									tone: i.failed_logins || i.errors ? 'text-amber-600 dark:text-amber-400' : undefined
								}))}
								onpick={(key) => apply({ ip: key })}
								barClass="bg-violet-500/15"
								emptyLabel={$t('admin.audit.none')}
							/>
						</div>
						<div class="rounded-lg border border-border/50 p-3">
							<h4 class="text-sm font-medium text-foreground mb-2">{$t('admin.audit.panels.failedLogins')}</h4>
							<BarList rows={stats.failed_logins.map((f) => ({ key: f.key, label: f.key, count: f.count }))} barClass="bg-amber-500/20" emptyLabel={$t('admin.audit.noFailedLogins')} />
						</div>
						<div class="rounded-lg border border-border/50 p-3">
							<h4 class="text-sm font-medium text-foreground mb-2">{$t('admin.audit.panels.status')}</h4>
							<BarList rows={stats.status.map((s) => ({ key: s.key, label: s.key === '-' ? '—' : s.key, count: s.count }))} onpick={(key) => key !== '-' && apply({ status: key })} barClass="bg-emerald-500/15" />
						</div>
						<div class="rounded-lg border border-border/50 p-3">
							<h4 class="text-sm font-medium text-foreground mb-2">{$t('admin.audit.panels.sources')}</h4>
							<BarList rows={stats.sources.map((s) => ({ key: s.key, label: s.key === '-' ? '—' : $t(`admin.audit.sources.${s.key}`), count: s.count }))} onpick={(key) => key !== '-' && apply({ source: key })} barClass="bg-slate-500/15" />
						</div>
					</div>

					<div class="rounded-lg border border-border/50">
						<h4 class="text-sm font-medium text-foreground px-3 pt-3 pb-2">{$t('admin.audit.panels.routes')}</h4>
						{#if stats.routes.length === 0}
							<div class="py-6 text-center text-xs text-muted-foreground">{$t('admin.audit.none')}</div>
						{:else}
							<div class="overflow-x-auto">
								<table class="w-full text-xs">
									<thead class="text-muted-foreground">
										<tr class="border-b border-border/50">
											<th class="text-left font-normal px-3 py-1.5">{$t('admin.audit.cols.endpoint')}</th>
											<th class="text-right font-normal px-2 py-1.5 whitespace-nowrap">{$t('admin.audit.cols.calls')}</th>
											<th class="text-right font-normal px-2 py-1.5 whitespace-nowrap">{$t('admin.audit.cols.errors')}</th>
											<th class="text-right font-normal px-3 py-1.5 whitespace-nowrap">{$t('admin.audit.cols.avg')}</th>
										</tr>
									</thead>
									<tbody>
										{#each stats.routes as r (r.method + r.route)}
											<tr class="border-b border-border/30 last:border-0 hover:bg-muted/30">
												<td class="px-3 py-1.5 font-mono whitespace-nowrap"><span class="text-muted-foreground mr-1.5">{r.method}</span>{r.route || '—'}</td>
												<td class="text-right px-2 py-1.5 tabular-nums">{n(r.count)}</td>
												<td class="text-right px-2 py-1.5 tabular-nums {r.errors ? 'text-amber-600 dark:text-amber-400' : 'text-muted-foreground'}">{n(r.errors)}</td>
												<td class="text-right px-3 py-1.5 tabular-nums text-muted-foreground whitespace-nowrap">{r.avg_ms.toFixed(0)} ms</td>
											</tr>
										{/each}
									</tbody>
								</table>
							</div>
						{/if}
					</div>
				</div>
			{/if}
		{/if}
	</section>

	<!-- Entries -->
	<section id="audit-list" class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-5 scroll-mt-16">
		<div class="flex items-center justify-between text-xs text-muted-foreground mb-2 min-h-[1.25rem]">
			<span>{#if !loading}{$t('admin.audit.summary', { shown: n(entries.length), total: n(total) })}{/if}</span>
			<span class="hidden sm:inline">{$t('admin.audit.localTime')}</span>
		</div>

		{#if loading && entries.length === 0}
			<div class="space-y-2">{#each Array(6) as _}<div class="h-14 rounded-lg bg-muted/40 animate-pulse"></div>{/each}</div>
		{:else if entries.length === 0 && !error}
			<div class="flex flex-col items-center text-center py-12 px-4">
				<div class="text-sm font-medium text-foreground">{$t('admin.audit.empty')}</div>
				<div class="text-xs text-muted-foreground mt-1 max-w-sm">{$t('admin.audit.emptyHint')}</div>
			</div>
		{:else}
			<div class="transition-opacity {loading ? 'opacity-60' : ''}">
				{#each grouped as group (group.day)}
					<div class="sticky top-11 z-[1] -mx-1 px-1 py-1.5 bg-card/95 backdrop-blur text-xs font-medium text-muted-foreground">
						{group.label}<span class="ml-1 opacity-70 tabular-nums">· {group.items.length}</span>
					</div>
					<ol class="mb-2 divide-y divide-border/40">
						{#each group.items as { entry, key } (key)}
							{@const tone = toneOf(entry)}
							{@const open = expanded.has(key)}
							<li>
								<button onclick={() => toggle(key)} aria-expanded={open} class="w-full text-left rounded-lg px-2 py-2 hover:bg-muted/40 transition-colors {open ? 'bg-muted/40' : ''}">
									<div class="flex items-start gap-2.5">
										<span class="mt-0.5 flex-shrink-0 w-11 text-right font-mono text-xs tabular-nums {statusTone(entry.status)}">{entry.status ?? '—'}</span>
										<div class="min-w-0 flex-1">
											<div class="text-sm text-foreground leading-snug break-words">
												<span class="font-medium">{entry.user || (entry.user_id ? entry.user_id : $t('admin.audit.anonymous'))}</span>
												<span class="text-muted-foreground"> · </span>
												{#each highlight(summary(entry), q) as part}{#if part.hit}<mark class="bg-amber-200/70 dark:bg-amber-500/30 text-inherit rounded px-0.5">{part.text}</mark>{:else}{part.text}{/if}{/each}
											</div>
											<div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
												<span class="tabular-nums">{timeOf(entry)}</span>
												{#if entry.action}<span class="px-1.5 py-px rounded ring-1 ring-inset {TONE_CLASS[tone]}">{actionLabel(entry.action, $locale)}</span>{/if}
												{#if entry.action && entry.method}<span class="font-mono">{entry.method}</span>{/if}
												{#if entry.ms !== undefined}<span class="tabular-nums">{entry.ms} ms</span>{/if}
												{#if entry.source && entry.source !== 'web'}<span>{$t(`admin.audit.sources.${entry.source}`)}</span>{/if}
												{#if entry.ip}<span class="font-mono">{entry.ip}</span>{/if}
												{#if entry.ua}<span class="hidden sm:inline">{shortUA(entry.ua)}</span>{/if}
											</div>
										</div>
										<svg class="w-4 h-4 mt-0.5 flex-shrink-0 text-muted-foreground transition-transform {open ? 'rotate-180' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" /></svg>
									</div>
								</button>
								{#if open}
									<div class="mx-2 mb-2 mt-1 rounded-lg border border-border/60 bg-background/60 p-3 text-xs animate-fade-in">
										<div class="grid grid-cols-[5.5rem_1fr] gap-x-3 gap-y-1">
											<span class="text-muted-foreground">{$t('admin.audit.fields.time')}</span>
											<span class="font-mono break-all">{new Date(entry.time).toLocaleString(getIntlLocale())} <span class="text-muted-foreground">({entry.time})</span></span>
											<span class="text-muted-foreground">{$t('admin.audit.fields.user')}</span>
											<span class="break-all">{entry.user || '—'} {#if entry.user_id}<span class="font-mono text-muted-foreground">({entry.user_id})</span>{/if}</span>
											{#if entry.action}<span class="text-muted-foreground">{$t('admin.audit.fields.action')}</span><span class="font-mono">{entry.action}</span>{/if}
											{#if entry.target}
												<span class="text-muted-foreground">{$t('admin.audit.fields.target')}</span>
												<span class="font-mono break-all">{entry.target}</span>
											{/if}
											{#if entry.method}<span class="text-muted-foreground">{$t('admin.audit.fields.request')}</span><span class="font-mono break-all">{entry.method} {entry.path}{#if entry.route && entry.route !== entry.path} <span class="text-muted-foreground">({entry.route})</span>{/if}</span>{/if}
											{#if entry.status}<span class="text-muted-foreground">{$t('admin.audit.fields.result')}</span><span class="font-mono"><span class={statusTone(entry.status)}>{entry.status}</span> · {entry.ms ?? 0} ms</span>{/if}
											{#if entry.error}<span class="text-muted-foreground">{$t('admin.audit.fields.error')}</span><span class="font-mono break-all text-rose-600 dark:text-rose-400">{entry.error}</span>{/if}
											{#if entry.source}<span class="text-muted-foreground">{$t('admin.audit.fields.source')}</span><span>{$t(`admin.audit.sources.${entry.source}`)}</span>{/if}
											{#if entry.ip}<span class="text-muted-foreground">IP</span><span class="font-mono">{entry.ip}</span>{/if}
											{#if entry.ua}<span class="text-muted-foreground">{$t('admin.audit.fields.device')}</span><span class="font-mono break-all">{entry.ua}</span>{/if}
										</div>
										{#if entry.detail && Object.keys(entry.detail).length > 0}
											<pre class="mt-2 p-2 rounded bg-muted/50 font-mono text-[11px] leading-relaxed overflow-x-auto whitespace-pre-wrap break-all">{JSON.stringify(entry.detail, null, 2)}</pre>
										{/if}
										<div class="mt-2 flex flex-wrap gap-1.5">
											{#if entry.user_id}<button onclick={() => apply({ user: entry.user_id })} class="px-2 py-1 rounded-md border border-border/60 hover:bg-muted/50">{$t('admin.audit.onlyUser')}</button>{/if}
											{#if entry.ip}<button onclick={() => apply({ ip: entry.ip })} class="px-2 py-1 rounded-md border border-border/60 hover:bg-muted/50">{$t('admin.audit.onlyIP')}</button>{/if}
											{#if entry.action}<button onclick={() => apply({ category: entry.action })} class="px-2 py-1 rounded-md border border-border/60 hover:bg-muted/50">{$t('admin.audit.onlyAction')}</button>{/if}
											{#if isDate(entry.target) && entry.user_id}<span class="px-2 py-1 text-muted-foreground">{$t('admin.audit.diaryDate', { date: entry.target })}</span>{/if}
										</div>
									</div>
								{/if}
							</li>
						{/each}
					</ol>
				{/each}
				{#if entries.length < total}
					<div class="pt-2 flex justify-center">
						<button onclick={loadMore} disabled={loadingMore} class="px-4 py-2 rounded-lg border border-border/60 text-sm hover:bg-muted/50 disabled:opacity-50">
							{loadingMore ? $t('common.loading') : $t('admin.audit.loadMore', { count: n(Math.min(PAGE, total - entries.length)) })}
						</button>
					</div>
				{/if}
			</div>
		{/if}
	</section>
</div>
