<script lang="ts">
	/**
	 * Everything about the visits of one entry: totals, who read it (ranked)
	 * and its full log. A bottom sheet on phones, a dialog on wider screens.
	 */
	import { onMount } from 'svelte';
	import { getVisitDiary, type DiaryStat, type VisitScope, type VisitorStat } from '$lib/api/visits';
	import { t } from '$lib/i18n';
	import VisitLog from './VisitLog.svelte';
	import { visitorLabel, deviceLabel, clientLabel, dayLabel, relativeTime, timeLabel, formatNumber } from './visitFormat';

	let { scope, date, start, onclose }: { scope: VisitScope; date: string; start?: string; onclose: () => void } = $props();

	let stats = $state<DiaryStat | null>(null);
	let visitors = $state<VisitorStat[]>([]);
	let loading = $state(true);
	let error = $state('');
	let picked = $state<VisitorStat | null>(null);
	let panel: HTMLDivElement | undefined = $state();

	async function load() {
		loading = true;
		error = '';
		try {
			const data = await getVisitDiary(scope, date, { start });
			stats = data.stats;
			visitors = data.visitors;
		} catch (e) {
			error = e instanceof Error ? e.message : $t('visits.loadFailed');
		}
		loading = false;
	}

	onMount(() => {
		void load();
		const previous = document.body.style.overflow;
		document.body.style.overflow = 'hidden';
		panel?.focus();
		const onKey = (event: KeyboardEvent) => {
			if (event.key === 'Escape') onclose();
		};
		window.addEventListener('keydown', onKey);
		return () => {
			document.body.style.overflow = previous;
			window.removeEventListener('keydown', onKey);
		};
	});

	let max = $derived(Math.max(1, ...visitors.map((v) => v.views)));
	let fixed = $derived(
		picked
			? { diary: date, visitor: picked.visitor_id || '-', device: picked.visitor_id ? undefined : picked.device }
			: { diary: date }
	);
</script>

<div class="fixed inset-0 z-50 flex items-end sm:items-center justify-center" role="presentation">
	<button type="button" class="absolute inset-0 bg-black/40 backdrop-blur-[2px] animate-fade-in cursor-default" aria-label={$t('visits.close')} onclick={onclose}></button>
	<div
		bind:this={panel}
		tabindex="-1"
		role="dialog"
		aria-modal="true"
		aria-label={$t('visits.diaryHeading', { date: dayLabel(date) })}
		class="relative w-full sm:max-w-3xl max-h-[92vh] sm:max-h-[85vh] flex flex-col bg-card rounded-t-2xl sm:rounded-2xl shadow-xl border border-border/50 outline-none animate-fade-in"
	>
		<div class="sm:hidden mx-auto mt-2 h-1 w-10 rounded-full bg-muted-foreground/30" aria-hidden="true"></div>
		<header class="flex items-start justify-between gap-3 px-4 sm:px-6 pt-3 sm:pt-5 pb-3 border-b border-border/50">
			<div class="min-w-0">
				<h3 class="text-base sm:text-lg font-semibold text-foreground break-words">{$t('visits.diaryHeading', { date: dayLabel(date, true) })}</h3>
				{#if stats}
					<p class="text-xs text-muted-foreground mt-0.5 tabular-nums">{$t('visits.firstSeen', { when: timeLabel(stats.first) })} · {$t('visits.lastSeen', { when: timeLabel(stats.last) })}</p>
				{/if}
			</div>
			<div class="flex items-center gap-1 flex-shrink-0">
				{#if scope.kind === 'self' && /^\d{4}-\d{2}-\d{2}$/.test(date)}
					<a href="/diary/{date}" class="px-2.5 py-1.5 text-xs rounded-lg border border-border/60 hover:bg-muted/50">{$t('visits.open')}</a>
				{/if}
				<button type="button" onclick={onclose} class="p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60" aria-label={$t('visits.close')}>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
				</button>
			</div>
		</header>

		<div class="overflow-y-auto overscroll-contain px-4 sm:px-6 py-4 space-y-5 pb-[max(1rem,env(safe-area-inset-bottom))]">
			{#if error}
				<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm flex items-center justify-between gap-3"><span>{error}</span><button type="button" onclick={load} class="underline">{$t('visits.retry')}</button></div>
			{:else if loading && !stats}
				<div class="grid grid-cols-2 sm:grid-cols-4 gap-2">{#each Array(4) as _}<div class="h-16 rounded-lg bg-muted/40 animate-pulse"></div>{/each}</div>
			{:else if stats}
				<div class="grid grid-cols-2 sm:grid-cols-4 gap-2">
					<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('visits.cards.views')}</div><div class="text-lg font-semibold tabular-nums">{formatNumber(stats.views)}</div></div>
					<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('visits.cards.visitors')}</div><div class="text-lg font-semibold tabular-nums">{formatNumber(stats.visitors)}</div></div>
					<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('visits.cards.failed')}</div><div class="text-lg font-semibold tabular-nums {stats.failed ? 'text-rose-600 dark:text-rose-400' : ''}">{formatNumber(stats.failed)}</div></div>
					<div class="rounded-lg bg-muted/40 px-3 py-2.5"><div class="text-[11px] text-muted-foreground">{$t('visits.cards.others')}</div><div class="text-lg font-semibold tabular-nums {stats.others ? 'text-amber-600 dark:text-amber-400' : ''}">{formatNumber(stats.others)}</div></div>
				</div>
			{:else}
				<div class="py-6 text-center text-sm text-muted-foreground">{$t('visits.empty')}</div>
			{/if}

			{#if visitors.length}
				<section>
					<h4 class="text-sm font-medium text-foreground mb-2">{$t('visits.diaryVisitors')}</h4>
					<ul class="space-y-1">
						{#each visitors as v, i (v.key)}
							<li>
								<button
									type="button"
									onclick={() => (picked = picked?.key === v.key ? null : v)}
									aria-pressed={picked?.key === v.key}
									class="relative w-full overflow-hidden rounded-lg px-3 py-2 text-left transition-colors {picked?.key === v.key ? 'ring-2 ring-primary/50 bg-primary/5' : 'hover:bg-muted/50'}"
								>
									<span class="absolute inset-y-0 left-0 {v.self ? 'bg-primary/10' : v.visitor_id ? 'bg-amber-500/15' : 'bg-rose-500/10'}" style="width: {(v.views / max) * 100}%"></span>
									<span class="relative flex items-center gap-2">
										<span class="w-5 text-xs text-muted-foreground tabular-nums">{i + 1}</span>
										<span class="min-w-0 flex-1">
											<span class="block text-sm text-foreground truncate">{visitorLabel(v, scope)}</span>
											<span class="block text-[11px] text-muted-foreground truncate">
												{v.visitor_id ? $t('visits.devicesCount', { count: v.devices }) : deviceLabel(v.device)} · {$t('visits.ipsCount', { count: v.ips })} · {clientLabel(v.last_ua)}{#if v.last_ip} · <span class="font-mono">{v.last_ip}</span>{/if}
											</span>
										</span>
										<span class="flex-shrink-0 text-right">
											<span class="block text-sm font-medium tabular-nums">{formatNumber(v.views)}</span>
											<span class="block text-[11px] text-muted-foreground whitespace-nowrap">{#if v.failed}<span class="text-rose-600 dark:text-rose-400">{$t('visits.failedCount', { count: v.failed })}</span> · {/if}{relativeTime(v.last)}</span>
										</span>
									</span>
								</button>
							</li>
						{/each}
					</ul>
				</section>
			{/if}

			<section>
				<h4 class="text-sm font-medium text-foreground mb-2">{$t('visits.diaryLog')}</h4>
				<VisitLog {scope} {fixed} {start} pageSize={15} showDiary={false} onclearfixed={picked ? () => (picked = null) : undefined} />
			</section>
		</div>
	</div>
</div>
