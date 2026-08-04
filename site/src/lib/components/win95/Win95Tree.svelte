<script lang="ts">
	/**
	 * WinHelp / HTML-Help style contents tree: a book at the root, one folder
	 * per year and per month, one page per entry.
	 *
	 * The dotted rails are rebuilt the way the real control drew them — every
	 * row carries the "does this ancestor still have siblings below?" flags, so
	 * a vertical line is only painted where the branch actually continues.
	 */
	import { tick } from 'svelte';
	import Win95Icon from './Win95Icon.svelte';
	import type { CalendarDiaryMeta } from '$lib/api/diaries';
	import { getDayOfWeek, getPreviousDay, getToday } from '$lib/utils/date';
	import { t, ta } from '$lib/i18n';

	export let entries: CalendarDiaryMeta[] = [];
	export let currentDate: string;
	export let loading = false;
	export let onSelect: (date: string) => void;
	export let onToday: () => void;
	export let onYesterday: () => void;
	/** The CE sheet already has a title bar saying the same thing — drop the caption there. */
	export let showCaption = true;

	type Kind = 'root' | 'year' | 'month' | 'day';

	/** An entry, or the stand-in node for a day that hasn't been written yet. */
	interface TreeEntry extends CalendarDiaryMeta {
		placeholder?: boolean;
	}

	interface Row {
		key: string;
		kind: Kind;
		label: string;
		badge: string;
		date: string;
		mood: string;
		weather: string;
		/** A day node with no entry behind it yet — drawn as a blank page. */
		placeholder: boolean;
		depth: number;
		/**
		 * One flag per rail column left of the junction cell. Column `c` carries
		 * the trunk of the ancestor at depth `c + 1`, so it is only painted when
		 * that ancestor still has siblings below it.
		 */
		guides: boolean[];
		last: boolean;
		hasChildren: boolean;
	}

	/** Node keys that are currently expanded (`Record` keeps Svelte 4 reactive). */
	let open: Record<string, boolean> = { root: true };
	let scroller: HTMLDivElement | undefined;
	let lastRevealed = '';
	let revealedAfterLoad = false;

	$: monthNames = $ta('calendar.monthsShort');

	// ---------------------------------------------------------------- grouping

	interface MonthGroup {
		key: string;
		month: number;
		days: TreeEntry[];
	}
	interface YearGroup {
		key: string;
		year: number;
		months: MonthGroup[];
		count: number;
	}

	/**
	 * The open day is always represented, even before it has any content — that
	 * way "Today"/"Yesterday" (and any fresh date) always have a node to select
	 * and scroll to instead of silently landing on nothing.
	 */
	$: treeEntries = withCurrentDate(entries, currentDate);

	function withCurrentDate(list: CalendarDiaryMeta[], current: string): TreeEntry[] {
		if (!current || list.some((e) => e.date === current)) return list;
		return [...list, { date: current, mood: '', weather: '', placeholder: true }];
	}

	$: years = groupEntries(treeEntries);

	function groupEntries(list: TreeEntry[]): YearGroup[] {
		const byYear = new Map<number, Map<number, TreeEntry[]>>();

		for (const item of list) {
			if (!item?.date || item.date.length < 10) continue;
			const y = Number(item.date.slice(0, 4));
			const m = Number(item.date.slice(5, 7));
			if (!Number.isFinite(y) || !Number.isFinite(m)) continue;
			let months = byYear.get(y);
			if (!months) {
				months = new Map();
				byYear.set(y, months);
			}
			const days = months.get(m);
			if (days) days.push(item);
			else months.set(m, [item]);
		}

		// Newest first, the way you'd actually browse a journal.
		return [...byYear.entries()]
			.sort((a, b) => b[0] - a[0])
			.map(([year, months]) => {
				const monthList = [...months.entries()]
					.sort((a, b) => b[0] - a[0])
					.map(([month, days]) => ({
						key: `${year}-${String(month).padStart(2, '0')}`,
						month,
						days: days.slice().sort((a, b) => (a.date < b.date ? 1 : -1))
					}));
				return {
					key: String(year),
					year,
					months: monthList,
					count: monthList.reduce((n, m) => n + m.days.length, 0)
				};
			});
	}

	// ------------------------------------------------------------ flat rows

	$: rows = buildRows(years, open, monthNames);

	function buildRows(list: YearGroup[], opened: Record<string, boolean>, months: string[]): Row[] {
		const out: Row[] = [];
		const total = list.reduce((n, y) => n + y.count, 0);

		out.push({
			key: 'root',
			kind: 'root',
			label: 'Diarum',
			badge: total ? String(total) : '',
			date: '',
			mood: '',
			weather: '',
			placeholder: false,
			depth: 0,
			guides: [],
			last: true,
			hasChildren: list.length > 0
		});

		if (!opened.root) return out;

		list.forEach((year, yi) => {
			const yearLast = yi === list.length - 1;
			out.push({
				key: year.key,
				kind: 'year',
				label: String(year.year),
				badge: String(year.count),
				date: '',
				mood: '',
				weather: '',
				placeholder: false,
				depth: 1,
				guides: [],
				last: yearLast,
				hasChildren: year.months.length > 0
			});
			if (!opened[year.key]) return;

			year.months.forEach((month, mi) => {
				const monthLast = mi === year.months.length - 1;
				out.push({
					key: month.key,
					kind: 'month',
					label: months[month.month - 1] ?? String(month.month),
					badge: String(month.days.length),
					date: '',
					mood: '',
					weather: '',
					placeholder: false,
					depth: 2,
					guides: [!yearLast],
					last: monthLast,
					hasChildren: month.days.length > 0
				});
				if (!opened[month.key]) return;

				month.days.forEach((day, di) => {
					out.push({
						key: day.date,
						kind: 'day',
						label: String(Number(day.date.slice(8, 10))),
						badge: getDayOfWeek(day.date),
						date: day.date,
						mood: day.mood ?? '',
						weather: day.weather ?? '',
						placeholder: day.placeholder === true,
						depth: 3,
						guides: [!yearLast, !monthLast],
						last: di === month.days.length - 1,
						hasChildren: false
					});
				});
			});
		});

		return out;
	}

	// --------------------------------------------------------- auto-expand

	/** Opens the year and month holding `target`, leaving everything else alone. */
	function expandFor(target: string) {
		if (!target) return;
		open = { ...open, root: true, [target.slice(0, 4)]: true, [target.slice(0, 7)]: true };
	}

	async function scrollToDate(target: string) {
		// Two ticks: the first flushes the `open` change into new rows, the
		// second lets those rows lay out before we measure them.
		await tick();
		await tick();
		const el = scroller?.querySelector(`[data-date="${target}"]`) as HTMLElement | null;
		el?.scrollIntoView({ block: 'nearest' });
	}

	/**
	 * The auto-expand has to be folded into `open` *before* `rows` is derived
	 * from it. Svelte orders reactive statements by the dependencies it can see,
	 * and an assignment made inside a called function is invisible to it — as a
	 * bare `$:` block calling `expandFor` this was ordered *after* the `rows`
	 * statement. A tree mounted with `entries` already loaded (the CE sheet,
	 * which is created fresh every time it opens) therefore built its rows from
	 * the pre-expand map, and nothing invalidated them afterwards. The +/- glyph
	 * reads `open` live so it showed "expanded" while the child rows were
	 * missing — hence a year that had to be clicked twice.
	 *
	 * Deriving `open` here declares the dependency, so `rows` is always built
	 * from a map that already has the current date expanded.
	 */
	$: open = expandInitial(open, currentDate);

	/**
	 * Expands once per date and then leaves `open` alone, so `collapseAll` and
	 * manual toggles stick. The "have I done this date yet" flag is tracked here
	 * rather than reusing `lastRevealed`: that one is owned by the scroll block
	 * below, which Svelte happens to order *before* this statement, so reading it
	 * here would see it already set to the current date and skip every expand.
	 */
	let lastExpanded = '';

	function expandInitial(
		current: Record<string, boolean>,
		target: string
	): Record<string, boolean> {
		if (!target || target === lastExpanded) return current;
		lastExpanded = target;
		return { ...current, root: true, [target.slice(0, 4)]: true, [target.slice(0, 7)]: true };
	}

	$: if (currentDate && currentDate !== lastRevealed) {
		lastRevealed = currentDate;
		void scrollToDate(currentDate);
	}

	// The tree is still showing the loading line on first paint, so the reveal
	// above has nothing to find. Retry once the rows actually exist.
	$: if (!loading && !revealedAfterLoad && currentDate) {
		revealedAfterLoad = true;
		void scrollToDate(currentDate);
	}

	/**
	 * Today/Yesterday expand straight away so a collapsed tree reopens on the
	 * spot. The scroll is left to the reactive block above once the route
	 * lands — the target day may not have a node until it becomes the current
	 * date — unless we're already on it, in which case nothing will change and
	 * we have to scroll here.
	 */
	function jumpTo(target: string, navigateAway: () => void) {
		expandFor(target);
		if (target === currentDate) void scrollToDate(target);
		navigateAway();
	}

	function handleToday() {
		jumpTo(getToday(), onToday);
	}

	function handleYesterday() {
		jumpTo(getPreviousDay(getToday()), onYesterday);
	}

	function toggle(key: string) {
		open = { ...open, [key]: !open[key] };
	}

	function expandAll() {
		const next: Record<string, boolean> = { root: true };
		for (const year of years) {
			next[year.key] = true;
			for (const month of year.months) next[month.key] = true;
		}
		open = next;
	}

	/** Collapses back to the year list rather than hiding the whole tree. */
	function collapseAll() {
		open = { root: true };
	}

	function activate(row: Row) {
		if (row.kind === 'day') {
			onSelect(row.date);
		} else if (row.hasChildren) {
			toggle(row.key);
		}
	}

	function iconFor(row: Row): 'book' | 'folder' | 'folder-open' | 'page' | 'page-blank' {
		if (row.kind === 'root') return 'book';
		if (row.kind === 'day') return row.placeholder ? 'page-blank' : 'page';
		return open[row.key] ? 'folder-open' : 'folder';
	}

	$: today = getToday();
	$: monthTitle = (row: Row) =>
		row.kind === 'month' ? $ta('calendar.months')[Number(row.key.slice(5, 7)) - 1] ?? '' : '';
</script>

<div class="tree-pane">
	{#if showCaption}
		<div class="w95-pane-caption">
			<Win95Icon name="contents" size={13} />
			<span>{$t('win95.contentsTitle')}</span>
		</div>
	{/if}

	<div class="w95-field tree-body" bind:this={scroller}>
		{#if loading}
			<div class="w95-tree-empty">{$t('win95.treeLoading')}</div>
		{:else if rows.length <= 1}
			<div class="w95-tree" role="tree" aria-label={$t('win95.contentsTitle')}>
				<div class="w95-tree-empty">{$t('win95.treeEmpty')}</div>
			</div>
		{:else}
			<div class="w95-tree" role="tree" aria-label={$t('win95.contentsTitle')}>
				{#each rows as row (row.key)}
					<div class="w95-tree-row">
						{#each row.guides as guide}
							<span class="w95-rail" class:line={guide}></span>
						{/each}

						{#if row.depth > 0}
							<span class="w95-knob">
								<span class="trunk" class:half={row.last}></span>
								<span class="elbow"></span>
							</span>
						{/if}

						<span class="w95-btncell" class:root={row.depth === 0}>
							<span class="cross"></span>
							{#if row.hasChildren}
								<button
									type="button"
									class="w95-toggle"
									class:plus={!open[row.key]}
									aria-label={open[row.key] ? '-' : '+'}
									aria-expanded={open[row.key] ? 'true' : 'false'}
									on:click={() => toggle(row.key)}
								></button>
							{/if}
						</span>

						<button
							type="button"
							class="w95-node"
							class:selected={row.kind === 'day' && row.date === currentDate}
							data-date={row.date || undefined}
							title={row.kind === 'day' ? row.date : monthTitle(row)}
							on:click={() => activate(row)}
							on:dblclick={() => row.hasChildren && toggle(row.key)}
						>
							<Win95Icon name={iconFor(row)} size={14} />
							<span class="label">{row.label}</span>
							{#if row.badge}
								<span class="dim">{row.kind === 'day' ? row.badge : `(${row.badge})`}</span>
							{/if}
							{#if row.weather}<span class="emo">{row.weather}</span>{/if}
							{#if row.mood}<span class="emo">{row.mood}</span>{/if}
							{#if row.date === today}
								<span class="dim">•</span>
							{/if}
						</button>
					</div>
				{/each}
			</div>
		{/if}
	</div>

	<div class="tree-foot">
		<button
			type="button"
			class="w95-btn foot-btn"
			title={$t('win95.todayPlain')}
			on:click={handleToday}
		>
			{$t('win95.todayPlain')}
		</button>
		<button
			type="button"
			class="w95-btn foot-btn"
			title={$t('win95.yesterday')}
			on:click={handleYesterday}
		>
			{$t('win95.yesterday')}
		</button>
		<button
			type="button"
			class="w95-btn foot-btn"
			title={$t('win95.expandAll')}
			aria-label={$t('win95.expandAll')}
			on:click={expandAll}
		>
			<Win95Icon name="tree-expand" size={16} />
		</button>
		<button
			type="button"
			class="w95-btn foot-btn"
			title={$t('win95.collapseAll')}
			aria-label={$t('win95.collapseAll')}
			on:click={collapseAll}
		>
			<Win95Icon name="tree-collapse" size={16} />
		</button>
	</div>
</div>

<style>
	.tree-pane {
		display: flex;
		flex-direction: column;
		min-height: 90px;
		flex: 3 1 0;
	}

	.tree-body {
		flex: 1 1 auto;
		min-height: 0;
		overflow: auto;
		margin-bottom: 3px;
	}

	.tree-foot {
		flex-shrink: 0;
		display: flex;
		align-items: stretch;
		gap: 2px;
		min-width: 0;
	}

	/* All four share the row evenly; the two labelled ones drop to 11px so
	   "Yesterday" still fits a quarter of a 268px rail. */
	.tree-foot .foot-btn {
		flex: 1;
		min-width: 0;
		padding: 3px 2px;
		font-size: 11px;
		overflow: hidden;
		white-space: nowrap;
	}

	/* Out-specifies the shared `.w95-btn:active` text padding. */
	.tree-foot .foot-btn:active {
		padding: 4px 1px 2px 3px;
	}

	.emo {
		font-size: 11px;
		line-height: 1;
	}
</style>
