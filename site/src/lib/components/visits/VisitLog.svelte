<script lang="ts">
	/**
	 * Paged, filterable list of individual visits. Used for the full log, for
	 * one entry's log and for one visitor's log.
	 */
	import { listVisitLogs, type Visit, type VisitFilter, type VisitScope } from '$lib/api/visits';
	import { t } from '$lib/i18n';
	import {
		visitorLabel,
		deviceLabel,
		clientLabel,
		reasonLabel,
		sourceLabel,
		kindLabel,
		dayLabel,
		timeLabel,
		relativeTime,
		formatNumber,
		OK_BADGE,
		FAIL_BADGE,
		NEUTRAL_BADGE,
		PULLED_BADGE
	} from './visitFormat';

	let {
		scope,
		fixed = {},
		start,
		pageSize = 25,
		showDiary = true,
		showFilters = true,
		onopendiary,
		onclearfixed
	}: {
		scope: VisitScope;
		/** Filters set from outside (an entry, a visitor), shown as chips. */
		fixed?: VisitFilter;
		/** Period start (RFC 3339); the date pickers override it. */
		start?: string;
		pageSize?: number;
		showDiary?: boolean;
		showFilters?: boolean;
		onopendiary?: (date: string) => void;
		onclearfixed?: () => void;
	} = $props();

	let result = $state<'' | 'ok' | 'failed'>('');
	let source = $state('');
	let others = $state(false);
	let q = $state('');
	let from = $state('');
	let to = $state('');
	let page = $state(0);

	let rows = $state<Visit[]>([]);
	let total = $state(0);
	let loading = $state(false);
	let error = $state('');
	let seq = 0;
	let searchTimer: ReturnType<typeof setTimeout> | undefined;
	let debouncedQ = $state('');

	$effect(() => {
		const value = q;
		clearTimeout(searchTimer);
		searchTimer = setTimeout(() => (debouncedQ = value), 300);
		return () => clearTimeout(searchTimer);
	});

	let params = $derived({
		...fixed,
		result,
		source,
		others,
		q: debouncedQ,
		start: from || start,
		end: to || undefined
	});
	let key = $derived(JSON.stringify(params));
	let lastKey = '';

	$effect(() => {
		// Filters changed: back to the first page.
		if (key !== lastKey) {
			lastKey = key;
			if (page !== 0) {
				page = 0;
				return;
			}
		}
		void load(page);
	});

	export function refresh() {
		void load(page);
	}

	async function load(p: number) {
		const mine = ++seq;
		loading = true;
		error = '';
		try {
			const data = await listVisitLogs(scope, { ...JSON.parse(key), limit: pageSize, offset: p * pageSize });
			if (mine !== seq) return;
			rows = data.visits;
			total = data.total;
		} catch (e) {
			if (mine !== seq) return;
			error = e instanceof Error ? e.message : $t('visits.loadFailed');
		}
		if (mine === seq) loading = false;
	}

	function clear() {
		result = '';
		source = '';
		others = false;
		q = '';
		from = '';
		to = '';
	}

	let pages = $derived(Math.max(1, Math.ceil(total / pageSize)));
	let filtered = $derived(!!(result || source || others || q || from || to));
	const input = 'px-2.5 py-1.5 bg-background rounded-lg text-xs text-foreground border border-border/60 focus:outline-none focus:ring-2 focus:ring-primary/40';
</script>

<div class="space-y-3">
	{#if showFilters}
		<div class="flex flex-wrap items-center gap-2">
			<div class="inline-flex rounded-lg bg-muted/50 p-0.5" role="group" aria-label={$t('visits.filters.result')}>
				{#each [['', 'all'], ['ok', 'ok'], ['failed', 'failed']] as [value, label]}
					<button
						type="button"
						onclick={() => (result = value as typeof result)}
						aria-pressed={result === value}
						class="px-2.5 py-1 rounded-md text-xs whitespace-nowrap transition-colors {result === value ? 'bg-card text-foreground shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
					>{$t(`visits.filters.${label}`)}</button>
				{/each}
			</div>
			<select bind:value={source} class={input} aria-label={$t('visits.filters.source')}>
				<option value="">{$t('visits.filters.source')}: {$t('visits.filters.anySource')}</option>
				<option value="web">{$t('visits.source.web')}</option>
				<option value="api">{$t('visits.source.api')}</option>
				<option value="mcp">{$t('visits.source.mcp')}</option>
			</select>
			<label class="inline-flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
				<input type="checkbox" bind:checked={others} class="rounded border-border" />
				{$t('visits.filters.others')}
			</label>
			<input type="search" bind:value={q} placeholder={$t('visits.filters.search')} class="{input} w-full sm:w-56 sm:ml-auto" />
			<div class="flex w-full sm:w-auto items-center gap-1.5">
				<input type="date" bind:value={from} max={to || undefined} aria-label={$t('visits.filters.from')} class="{input} flex-1 min-w-0" />
				<span class="text-xs text-muted-foreground">–</span>
				<input type="date" bind:value={to} min={from || undefined} aria-label={$t('visits.filters.to')} class="{input} flex-1 min-w-0" />
			</div>
		</div>
	{/if}

	{#if fixed.diary || fixed.visitor || filtered}
		<div class="flex flex-wrap items-center gap-1.5 text-xs">
			{#if fixed.diary}<span class="px-2 py-0.5 rounded-full bg-primary/10 text-primary">{$t('visits.filters.diary', { date: dayLabel(fixed.diary) })}</span>{/if}
			{#if fixed.visitor}<span class="px-2 py-0.5 rounded-full bg-primary/10 text-primary">{$t('visits.filters.visitor', { name: fixed.visitor === '-' ? $t('visits.anonymous') : (rows.find((r) => r.visitor_id === fixed.visitor)?.visitor ?? fixed.visitor.slice(0, 8)) })}{#if fixed.device} · {deviceLabel(fixed.device)}{/if}</span>{/if}
			{#if (fixed.visitor || fixed.diary) && onclearfixed}
				<button type="button" onclick={onclearfixed} class="px-2 py-0.5 rounded-full text-muted-foreground hover:text-foreground hover:bg-muted/60" aria-label={$t('visits.filters.clear')}>✕</button>
			{/if}
			{#if filtered}<button type="button" onclick={clear} class="ml-auto underline text-muted-foreground hover:text-foreground">{$t('visits.filters.clear')}</button>{/if}
		</div>
	{/if}

	{#if error}
		<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm flex items-center justify-between gap-3"><span>{error}</span><button type="button" onclick={refresh} class="underline">{$t('visits.retry')}</button></div>
	{:else if !rows.length && loading}
		<div class="space-y-2">{#each Array(4) as _}<div class="h-14 rounded-lg bg-muted/40 animate-pulse"></div>{/each}</div>
	{:else if !rows.length}
		<div class="py-10 text-center text-sm text-muted-foreground rounded-xl border border-dashed border-border/60">{$t('visits.noLogs')}</div>
	{:else}
		<ul class="divide-y divide-border/50 rounded-xl border border-border/50 overflow-hidden {loading ? 'opacity-60' : ''} transition-opacity">
			{#each rows as v (v.id)}
				<li class="px-3 py-2.5 bg-card hover:bg-muted/30 transition-colors">
					<div class="flex items-start gap-2.5">
						<span class="mt-1.5 h-2 w-2 flex-shrink-0 rounded-full {v.success ? 'bg-emerald-500' : 'bg-rose-500'}" aria-hidden="true"></span>
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-center gap-x-2 gap-y-0.5">
								<span class="text-sm font-medium truncate {v.self ? 'text-foreground' : v.success ? 'text-amber-700 dark:text-amber-300' : 'text-foreground'}">{visitorLabel(v, scope)}</span>
								<span class="px-1.5 py-px rounded text-[11px] ring-1 ring-inset {v.success ? OK_BADGE : FAIL_BADGE}">{v.success ? $t('visits.status.ok') : reasonLabel(v.reason)}</span>
								{#if v.pulled}<span class="px-1.5 py-px rounded text-[11px] ring-1 ring-inset {PULLED_BADGE}">{$t('visits.pulled')}</span>{/if}
								<span class="ml-auto text-xs text-muted-foreground tabular-nums whitespace-nowrap" title={timeLabel(v.time)}>{relativeTime(v.time)}</span>
							</div>
							<div class="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
								{#if showDiary}
									{#if v.diary_date && onopendiary && /^\d{4}-\d{2}-\d{2}$/.test(v.diary_date)}
										<button type="button" onclick={() => onopendiary?.(v.diary_date!)} class="text-foreground/80 hover:text-primary hover:underline">{dayLabel(v.diary_date)}</button>
									{:else}
										<span class="text-foreground/80">{v.diary_date || (v.diary_id ? `#${v.diary_id.slice(0, 10)}` : $t('visits.unattributed'))}</span>
									{/if}
									<span class="opacity-40">·</span>
								{/if}
								<span>{kindLabel(v.kind)}</span>
								<span class="px-1 rounded {NEUTRAL_BADGE} ring-1 ring-inset text-[10px] uppercase tracking-wide">{sourceLabel(v.source)}</span>
								<span class="opacity-40">·</span>
								<span title={v.ua}>{clientLabel(v.ua)}</span>
								{#if v.device}<span class="opacity-40">·</span><span>{deviceLabel(v.device)}</span>{/if}
								{#if v.ip}<span class="opacity-40">·</span><span class="font-mono">{v.ip}</span>{/if}
								<span class="opacity-40">·</span><span class="tabular-nums">HTTP {v.status}</span>
							</div>
							<div class="sm:hidden mt-0.5 text-[11px] text-muted-foreground/70 tabular-nums">{timeLabel(v.time)}</div>
						</div>
					</div>
				</li>
			{/each}
		</ul>
		<div class="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
			<span class="tabular-nums">{$t('visits.showing', { shown: formatNumber(page * pageSize + rows.length), total: formatNumber(total) })}</span>
			{#if pages > 1}
				<div class="flex items-center gap-1">
					<button type="button" onclick={() => (page = Math.max(0, page - 1))} disabled={page === 0 || loading} class="px-2.5 py-1 rounded-md border border-border/60 hover:bg-muted/50 disabled:opacity-40">{$t('visits.prev')}</button>
					<span class="px-2 tabular-nums">{$t('visits.page', { page: page + 1, pages })}</span>
					<button type="button" onclick={() => (page = Math.min(pages - 1, page + 1))} disabled={page >= pages - 1 || loading} class="px-2.5 py-1 rounded-md border border-border/60 hover:bg-muted/50 disabled:opacity-40">{$t('visits.next')}</button>
				</div>
			{/if}
		</div>
	{/if}
</div>
