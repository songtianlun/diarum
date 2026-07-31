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
	import { getDayOfWeek, getToday } from '$lib/utils/date';
	import { t, ta } from '$lib/i18n';

	export let entries: CalendarDiaryMeta[] = [];
	export let currentDate: string;
	export let loading = false;
	export let onSelect: (date: string) => void;
	export let onToday: () => void;

	type Kind = 'root' | 'year' | 'month' | 'day';

	interface Row {
		key: string;
		kind: Kind;
		label: string;
		badge: string;
		date: string;
		mood: string;
		weather: string;
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
	let lastScrolledTo = '';

	$: monthNames = $ta('calendar.monthsShort');

	// ---------------------------------------------------------------- grouping

	interface MonthGroup {
		key: string;
		month: number;
		days: CalendarDiaryMeta[];
	}
	interface YearGroup {
		key: string;
		year: number;
		months: MonthGroup[];
		count: number;
	}

	$: years = groupEntries(entries);

	function groupEntries(list: CalendarDiaryMeta[]): YearGroup[] {
		const byYear = new Map<number, Map<number, CalendarDiaryMeta[]>>();

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

	/**
	 * Keep the branch holding the open entry expanded, and reveal it. Only fires
	 * when the date actually changes so manual collapsing isn't fought.
	 */
	$: if (currentDate && currentDate !== lastScrolledTo) {
		lastScrolledTo = currentDate;
		open = {
			...open,
			root: true,
			[currentDate.slice(0, 4)]: true,
			[currentDate.slice(0, 7)]: true
		};
		void revealSelected();
	}

	async function revealSelected() {
		await tick();
		const el = scroller?.querySelector('[data-selected="true"]') as HTMLElement | null;
		el?.scrollIntoView({ block: 'nearest' });
	}

	function toggle(key: string) {
		open = { ...open, [key]: !open[key] };
	}

	function activate(row: Row) {
		if (row.kind === 'day') {
			onSelect(row.date);
		} else if (row.hasChildren) {
			toggle(row.key);
		}
	}

	function iconFor(row: Row): 'book' | 'folder' | 'folder-open' | 'page' {
		if (row.kind === 'root') return 'book';
		if (row.kind === 'day') return 'page';
		return open[row.key] ? 'folder-open' : 'folder';
	}

	$: today = getToday();
	$: todayHasEntry = entries.some((e) => e.date === today);
	// Days that exist only as a route (today, before it is ever written) still
	// need to be reachable — the button below covers that case.
	$: monthTitle = (row: Row) =>
		row.kind === 'month' ? $ta('calendar.months')[Number(row.key.slice(5, 7)) - 1] ?? '' : '';
</script>

<div class="tree-pane">
	<div class="w95-pane-caption">
		<Win95Icon name="contents" size={13} />
		<span>{$t('win95.contentsTitle')}</span>
	</div>

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
							data-selected={row.kind === 'day' && row.date === currentDate}
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
		<button type="button" class="w95-btn today-btn" on:click={onToday}>
			<Win95Icon name={todayHasEntry ? 'page' : 'page-blank'} size={14} />
			<span>{$t('win95.todayPlain')}</span>
		</button>
		<span class="w95-hint">{$t('win95.contentsHint')}</span>
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
		align-items: center;
		gap: 6px;
		min-width: 0;
	}

	.today-btn {
		flex-shrink: 0;
	}

	.tree-foot .w95-hint {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.emo {
		font-size: 11px;
		line-height: 1;
	}
</style>
