<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import MilestoneFormModal from '$lib/components/days/MilestoneFormModal.svelte';
	import MilestoneGantt, { type GanttItem } from '$lib/components/days/MilestoneGantt.svelte';
	import { getDay, deleteMilestone, type Day, type Milestone } from '$lib/api/days';
	import { isAuthenticated } from '$lib/api/client';
	import { countDay, diffDays, weekday } from '$lib/utils/days';
	import { getToday } from '$lib/utils/date';
	import { t, locale } from '$lib/i18n';

	type View = 'list' | 'gantt';
	const VIEW_KEY = 'diarum_milestone_view';

	let id = $derived($page.params.id ?? '');
	let day = $state<Day | null>(null);
	let milestones: Milestone[] = $state([]);
	let loading = $state(true);
	let missing = $state(false);
	let view = $state<View>('list');
	// null: closed; {}: new; a milestone: editing it.
	let editing = $state<{ milestone?: Milestone } | null>(null);
	const today = getToday();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');

	interface Row {
		id: string;
		name: string;
		date: string;
		note: string;
		origin?: boolean;
		end?: boolean;
		milestone?: Milestone;
	}

	// The day itself always leads; its milestones follow by date.
	let rows: Row[] = $derived(
		day
			? [
					{ id: '__origin', name: day.name, date: day.date, note: day.note, origin: true },
					...[...milestones]
						.sort((a, b) => a.date.localeCompare(b.date) || a.created.localeCompare(b.created))
						.map((m) => ({ id: m.id, name: m.name, date: m.date, note: m.note, end: m.end, milestone: m }))
				]
			: []
	);

	// Where the "today" marker goes in the list: before the first row still to come.
	let todayIndex = $derived.by(() => {
		const i = rows.findIndex((row, index) => index > 0 && diffDays(today, row.date) > 0);
		return i === -1 ? rows.length : i;
	});

	async function load() {
		loading = true;
		missing = false;
		try {
			const data = await getDay(id);
			day = data.day;
			milestones = data.milestones;
		} catch {
			missing = true;
		} finally {
			loading = false;
		}
	}

	function setView(next: View) {
		view = next;
		try {
			localStorage.setItem(VIEW_KEY, next);
		} catch {
			// per-device preference only
		}
	}

	// A milestone linked from elsewhere (#m-<id>) is shown in the list and
	// briefly highlighted.
	let highlight = $state('');

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		try {
			const stored = localStorage.getItem(VIEW_KEY);
			if (stored === 'list' || stored === 'gantt') view = stored;
		} catch {
			// ignore
		}
		const target = location.hash.startsWith('#m-') ? location.hash.slice(3) : '';
		if (target) view = 'list';
		load().then(async () => {
			if (!target) return;
			await tick();
			document.getElementById(`m-${target}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
			highlight = target;
			setTimeout(() => (highlight = ''), 2400);
		});
	});

	function onsaved(saved: Milestone) {
		const exists = milestones.some((m) => m.id === saved.id);
		milestones = exists ? milestones.map((m) => (m.id === saved.id ? saved : m)) : [...milestones, saved];
		syncEnd();
		editing = null;
	}

	async function remove(m: Milestone) {
		if (!day || !confirm($t('days.milestones.deleteConfirm', { name: m.name }))) return;
		await deleteMilestone(day.id, m.id);
		milestones = milestones.filter((x) => x.id !== m.id);
		syncEnd();
	}

	// The day's end date follows its end milestone.
	function syncEnd() {
		if (day) day.end = milestones.find((m) => m.end)?.date ?? '';
	}

	let endTaken = $derived(milestones.find((m) => m.end && m.id !== editing?.milestone?.id)?.name ?? null);

	function offsetLabel(date: string): string {
		if (!day) return '';
		const n = diffDays(day.date, date);
		if (n === 0) return $t('days.milestones.offsetSame');
		return n > 0 ? $t('days.milestones.offsetAfter', { n }) : $t('days.milestones.offsetBefore', { n: -n });
	}

	function relativeLabel(date: string, origin = false): string {
		// The day itself counts the way its board does, start day included or not.
		if (origin && day) {
			const count = countDay({ date, repeat: '', include_start: day.include_start, end: day.end }, today);
			const label =
				count.direction === 'today'
					? $t('days.todayShort')
					: count.direction === 'future'
						? $t('days.inDays', { n: count.days })
						: $t('days.agoDays', { n: count.days });
			return count.ended ? `${label} · ${$t('days.endedShort')}` : label;
		}
		const n = diffDays(today, date);
		if (n === 0) return $t('days.todayShort');
		return n > 0 ? $t('days.inDays', { n }) : $t('days.agoDays', { n: -n });
	}

	function selectGantt(item: GanttItem) {
		const row = rows.find((r) => r.id === item.id);
		if (row?.milestone) editing = { milestone: row.milestone };
	}
</script>

<svelte:head>
	<title>{day ? `${day.name} · ${$t('days.milestones.title')}` : $t('days.milestones.title')} - Diarum</title>
</svelte:head>

<div class="min-h-screen bg-background flex flex-col">
	<PageHeader title={$t('days.milestones.title')} />

	<main class="flex-1 w-full max-w-4xl mx-auto px-4 py-5 sm:py-6">
		<a href="/days/{id}" class="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors mb-4 max-w-full">
			<svg class="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" /></svg>
			<span class="truncate">{day ? `${day.emoji || ''} ${day.name}`.trim() : $t('days.back')}</span>
		</a>

		{#if loading}
			<div class="h-64 rounded-2xl bg-muted/40 animate-pulse"></div>
		{:else if missing || !day}
			<div class="py-16 text-center text-sm text-muted-foreground">{$t('days.notFound')}</div>
		{:else}
			<div class="flex items-center justify-between gap-3 mb-4">
				<div class="inline-grid grid-cols-2 p-1 rounded-lg bg-muted/60 text-sm">
					{#each [['list', 'days.milestones.list'], ['gantt', 'days.milestones.gantt']] as [key, label]}
						<button
							type="button"
							onclick={() => setView(key as View)}
							aria-pressed={view === key}
							class="px-4 py-1.5 rounded-md transition-colors {view === key ? 'bg-card text-foreground shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
						>{$t(label)}</button>
					{/each}
				</div>
				<button
					type="button"
					onclick={() => (editing = {})}
					class="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-lg bg-primary text-primary-foreground text-sm font-medium shadow-sm hover:opacity-90 transition-opacity"
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-width="2" d="M12 5v14M5 12h14" /></svg>
					<span class="hidden sm:inline">{$t('days.milestones.add')}</span>
				</button>
			</div>

			<div class="bg-card rounded-2xl border border-border/50 shadow-sm p-4 sm:p-6 animate-fade-in-only">
				{#if view === 'gantt'}
					<MilestoneGantt items={rows} {today} includeStart={day.include_start} onselect={selectGantt} />
				{:else}
					<ol class="relative">
						{#each rows as row, index (row.id)}
							{#if index === todayIndex && index > 0}
								<li class="relative flex items-center gap-3 pl-0 py-1.5" aria-label={$t('days.milestones.today')}>
									<span class="w-7 flex justify-center flex-shrink-0"><span class="w-2.5 h-2.5 rounded-full bg-amber-600 ring-4 ring-amber-500/15"></span></span>
									<span class="flex-1 border-t border-dashed border-amber-500/50"></span>
									<span class="text-[11px] font-medium text-amber-700 dark:text-amber-400 tabular-nums">{$t('days.milestones.today')} · {today}</span>
								</li>
							{/if}
							{@const passed = diffDays(row.date, today) >= 0}
							<li id="m-{row.id}" class="group relative flex gap-3 -mx-2 px-2 rounded-xl transition-colors duration-700 {highlight === row.id ? 'bg-amber-500/10' : ''}">
								<!-- rail -->
								<span class="relative w-7 flex-shrink-0 flex justify-center">
									{#if index < rows.length - 1 || todayIndex === rows.length}
										<span class="absolute top-6 -bottom-1 w-px bg-border"></span>
									{/if}
									{#if row.milestone?.end}
										<span class="relative mt-3 w-3.5 h-3.5 rounded-[3px] {passed ? 'bg-foreground/70' : 'bg-card border-2 border-foreground/50'} ring-4 ring-foreground/5"></span>
									{:else if row.origin}
										<span class="relative mt-3 w-4 h-4 rotate-45 rounded-[3px] bg-amber-500 shadow-sm ring-4 ring-amber-500/15"></span>
									{:else}
										<span class="relative mt-3.5 w-3 h-3 rounded-full {passed ? 'bg-primary' : 'bg-card border-2 border-primary/60'}"></span>
									{/if}
								</span>
								<div class="flex-1 min-w-0 pb-5 pt-2">
									<div class="flex items-start justify-between gap-3">
										<div class="min-w-0">
											<div class="flex items-center gap-2 flex-wrap">
												<span class="text-sm font-medium text-foreground break-words {row.origin ? 'font-semibold' : ''}">{row.name}</span>
												{#if row.origin}
													<span class="px-1.5 py-px rounded text-[10px] bg-amber-500/15 text-amber-700 dark:text-amber-300">{$t('days.milestones.origin')}</span>
												{:else}
													<span class="px-1.5 py-px rounded text-[10px] bg-muted text-muted-foreground tabular-nums">{offsetLabel(row.date)}</span>
												{/if}
												{#if row.milestone?.end}
													<span class="px-1.5 py-px rounded text-[10px] bg-foreground/10 text-foreground/80">■ {$t('days.milestones.endLabel')}</span>
												{/if}
											</div>
											<div class="mt-0.5 text-xs text-muted-foreground tabular-nums">
												{row.date} · {weekday(row.date, intl)} · <span class={passed ? '' : 'text-amber-700 dark:text-amber-300'}>{relativeLabel(row.date, row.origin)}</span>
											</div>
											{#if row.note}
												<p class="mt-1.5 text-sm text-muted-foreground whitespace-pre-line break-words">{row.note}</p>
											{/if}
										</div>
										{#if row.milestone}
											{@const m = row.milestone}
											<div class="flex items-center gap-0.5 flex-shrink-0 sm:opacity-0 sm:group-hover:opacity-100 sm:focus-within:opacity-100 transition-opacity">
												<button type="button" onclick={() => (editing = { milestone: m })} class="p-1.5 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60" title={$t('days.edit')} aria-label={$t('days.edit')}>
													<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536M4 20h4L18.5 9.5a2.5 2.5 0 00-3.536-3.536L4.5 16.5 4 20z" /></svg>
												</button>
												<button type="button" onclick={() => remove(m)} class="p-1.5 rounded-lg text-muted-foreground hover:text-destructive hover:bg-destructive/10" title={$t('days.delete')} aria-label={$t('days.delete')}>
													<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6M9 7V4a1 1 0 011-1h4a1 1 0 011 1v3M4 7h16" /></svg>
												</button>
											</div>
										{:else if row.origin}
											<a href="/days/{day.id}" class="text-xs text-muted-foreground hover:text-foreground flex-shrink-0 pt-0.5">{$t('days.edit')}</a>
										{/if}
									</div>
								</div>
							</li>
						{/each}
						{#if todayIndex === rows.length}
							<li class="relative flex items-center gap-3 py-1.5">
								<span class="w-7 flex justify-center flex-shrink-0"><span class="w-2.5 h-2.5 rounded-full bg-amber-600 ring-4 ring-amber-500/15"></span></span>
								<span class="flex-1 border-t border-dashed border-amber-500/50"></span>
								<span class="text-[11px] font-medium text-amber-700 dark:text-amber-400 tabular-nums">{$t('days.milestones.today')} · {today}</span>
							</li>
						{/if}
					</ol>
					{#if milestones.length === 0}
						<button
							type="button"
							onclick={() => (editing = {})}
							class="mt-2 w-full py-6 rounded-xl border-2 border-dashed border-border hover:border-primary/40 hover:bg-muted/30 transition-colors text-sm text-muted-foreground"
						>{$t('days.milestones.empty')}</button>
					{/if}
				{/if}
			</div>
		{/if}
	</main>

	<Footer maxWidth="6xl" />
</div>

{#if editing && day}
	<MilestoneFormModal dayId={day.id} milestone={editing.milestone ?? null} {endTaken} onclose={() => (editing = null)} {onsaved} />
{/if}
