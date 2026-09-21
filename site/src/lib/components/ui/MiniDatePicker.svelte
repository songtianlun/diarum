<script lang="ts">
	/**
	 * Compact popover date picker for the classic diary view's date button.
	 * Three nested pickers (year grid / month grid / day grid) so jumping
	 * from, say, 2019 to today never means clicking "previous month" 80
	 * times. Days that have a diary entry are dotted; entry metadata for the
	 * visible year is fetched lazily and cached per year for the popover's
	 * lifetime.
	 */
	import { onMount, tick } from 'svelte';
	import { getDatesWithDiaries, type CalendarDiaryMeta } from '$lib/api/diaries';
	import { formatDate, getCalendarDays, getToday, getYearRange } from '$lib/utils/date';
	import { t, ta } from '$lib/i18n';

	export let value: string;
	export let onSelect: (date: string) => void;
	export let onClose: () => void;
	/** Anchors the popover above the trigger instead of below it. */
	export let dropUp = false;
	/** Anchors the popover to the right edge of the trigger instead of the left. */
	export let alignRight = false;

	type Mode = 'day' | 'month' | 'year';
	let mode: Mode = 'day';

	let viewYear = Number(value.slice(0, 4));
	let viewMonth = Number(value.slice(5, 7));
	let yearGridStart = viewYear - (viewYear % 12);

	const today = getToday();
	const todayYear = Number(today.slice(0, 4));
	const todayMonth = Number(today.slice(5, 7));

	// entries cache, keyed by year
	const entriesByYear = new Map<number, CalendarDiaryMeta[]>();
	let loadedEntries: CalendarDiaryMeta[] = [];
	let loading = false;

	async function loadYear(year: number) {
		const cached = entriesByYear.get(year);
		if (cached) {
			loadedEntries = cached;
			return;
		}
		loading = true;
		try {
			const range = getYearRange(year);
			const entries = await getDatesWithDiaries(range.start, range.end);
			entriesByYear.set(year, entries);
			if (viewYear === year) loadedEntries = entries;
		} finally {
			loading = false;
		}
	}

	$: void loadYear(viewYear);

	$: monthNames = $ta('calendar.months');
	$: monthNamesShort = $ta('calendar.monthsShort');
	$: weekdays = $ta('calendar.weekdaysShort').map((d) => d.charAt(0));
	$: haveEntrySet = new Set(loadedEntries.map((e) => e.date));
	$: monthsWithEntries = new Set(loadedEntries.map((e) => Number(e.date.slice(5, 7))));
	$: days = getCalendarDays(viewYear, viewMonth);
	$: metaByDate = new Map(loadedEntries.map((e) => [e.date, e]));

	function inMonth(d: Date) {
		return d.getMonth() === viewMonth - 1;
	}

	function pickDay(d: Date) {
		const iso = formatDate(d);
		if (iso > today) return;
		onSelect(iso);
	}

	function stepMonth(delta: number) {
		const next = viewMonth + delta;
		if (next < 1) {
			viewMonth = 12;
			viewYear -= 1;
		} else if (next > 12) {
			viewMonth = 1;
			viewYear += 1;
		} else {
			viewMonth = next;
		}
	}

	function stepYearGrid(delta: number) {
		yearGridStart += delta * 12;
	}

	function pickMonth(m: number) {
		viewMonth = m;
		mode = 'day';
	}

	function pickYear(y: number) {
		viewYear = y;
		mode = 'month';
	}

	function openMonthPicker() {
		mode = mode === 'month' ? 'day' : 'month';
	}

	function openYearPicker() {
		yearGridStart = viewYear - (viewYear % 12);
		mode = mode === 'year' ? 'day' : 'year';
	}

	function jumpToToday() {
		viewYear = todayYear;
		viewMonth = todayMonth;
		mode = 'day';
		onSelect(today);
	}

	function handleKey(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.stopPropagation();
			onClose();
		}
	}

	let panelEl: HTMLDivElement;
	onMount(async () => {
		await tick();
		panelEl?.focus();
	});
</script>

<svelte:window on:keydown={handleKey} />

<!-- Click-away scrim -->
<button class="scrim" tabindex="-1" aria-label={$t('datePicker.close')} on:click={onClose}></button>

<div
	class="picker-panel"
	class:drop-up={dropUp}
	class:align-right={alignRight}
	bind:this={panelEl}
	tabindex="-1"
	role="dialog"
	aria-label={$t('entryNav.calendar')}
>
	{#if mode === 'day'}
		<div class="picker-head">
			<button class="nav-arrow" on:click={() => stepMonth(-1)} title={$t('calendar.previousMonth')}>
				<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M15 19l-7-7 7-7" />
				</svg>
			</button>
			<div class="head-title">
				<button class="title-btn" on:click={openMonthPicker}>{monthNames[viewMonth - 1]}</button>
				<button class="title-btn" on:click={openYearPicker}>{viewYear}</button>
			</div>
			<button class="nav-arrow" on:click={() => stepMonth(1)} title={$t('calendar.nextMonth')}>
				<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M9 5l7 7-7 7" />
				</svg>
			</button>
		</div>

		<div class="weekday-row">
			{#each weekdays as w, i}
				<div class="weekday-cell" class:weekend={i === 0 || i === 6}>{w}</div>
			{/each}
		</div>

		<div class="day-grid" class:is-loading={loading}>
			{#each days as d (d.getTime())}
				{@const iso = formatDate(d)}
				{@const meta = metaByDate.get(iso)}
				<button
					class="day-cell"
					class:out={!inMonth(d)}
					class:is-today={iso === today}
					class:is-selected={iso === value}
					class:has-entry={haveEntrySet.has(iso)}
					disabled={iso > today}
					title={meta?.mood || meta?.weather ? `${iso} · ${[meta?.mood, meta?.weather].filter(Boolean).join(' ')}` : iso}
					on:click={() => pickDay(d)}
				>
					<span class="day-num">{d.getDate()}</span>
					{#if haveEntrySet.has(iso) && iso !== value}
						<span class="day-dot"></span>
					{/if}
				</button>
			{/each}
		</div>

		<div class="picker-foot">
			<button class="today-btn" on:click={jumpToToday}>
				<span class="today-dot"></span>
				{$t('datePicker.goToToday')}
			</button>
		</div>
	{:else if mode === 'month'}
		<div class="picker-head">
			<button class="nav-arrow" on:click={() => (viewYear -= 1)} title={$t('calendar.previousYear')}>
				<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M15 19l-7-7 7-7" />
				</svg>
			</button>
			<div class="head-title">
				<button class="title-btn" on:click={openYearPicker}>{viewYear}</button>
			</div>
			<button class="nav-arrow" on:click={() => (viewYear += 1)} title={$t('calendar.nextYear')}>
				<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M9 5l7 7-7 7" />
				</svg>
			</button>
		</div>

		<div class="month-grid">
			{#each monthNamesShort as m, i}
				<button
					class="month-cell"
					class:is-current={viewYear === todayYear && i + 1 === todayMonth}
					class:is-selected={viewYear === Number(value.slice(0, 4)) && i + 1 === Number(value.slice(5, 7))}
					class:has-entry={monthsWithEntries.has(i + 1)}
					disabled={viewYear > todayYear || (viewYear === todayYear && i + 1 > todayMonth)}
					on:click={() => pickMonth(i + 1)}
				>
					{m}
				</button>
			{/each}
		</div>
	{:else}
		<div class="picker-head">
			<button class="nav-arrow" on:click={() => stepYearGrid(-1)} title={$t('calendar.previousYear')}>
				<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M15 19l-7-7 7-7" />
				</svg>
			</button>
			<div class="head-title">
				<span class="head-static">{yearGridStart} - {yearGridStart + 11}</span>
			</div>
			<button class="nav-arrow" on:click={() => stepYearGrid(1)} title={$t('calendar.nextYear')}>
				<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M9 5l7 7-7 7" />
				</svg>
			</button>
		</div>

		<div class="year-grid">
			{#each Array(12) as _, i}
				{@const y = yearGridStart + i}
				<button
					class="year-cell"
					class:is-current={y === todayYear}
					class:is-selected={y === Number(value.slice(0, 4))}
					disabled={y > todayYear}
					on:click={() => pickYear(y)}
				>
					{y}
				</button>
			{/each}
		</div>
	{/if}
</div>

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 49;
		background: transparent;
		cursor: default;
	}

	.picker-panel {
		position: absolute;
		z-index: 50;
		top: calc(100% + 0.5rem);
		left: 0;
		width: 17.5rem;
		padding: 0.75rem;
		border-radius: 0.9rem;
		background: hsl(var(--card));
		border: 1px solid hsl(var(--border) / 0.6);
		box-shadow: 0 12px 32px hsl(var(--foreground) / 0.14), 0 2px 8px hsl(var(--foreground) / 0.08);
		outline: none;
		animation: pop-in 0.16s cubic-bezier(0.2, 0.9, 0.3, 1.2);
	}

	.picker-panel.drop-up {
		top: auto;
		bottom: calc(100% + 0.5rem);
	}

	.picker-panel.align-right {
		left: auto;
		right: 0;
	}

	@keyframes pop-in {
		from {
			opacity: 0;
			transform: translateY(-4px) scale(0.97);
		}
		to {
			opacity: 1;
			transform: translateY(0) scale(1);
		}
	}

	.picker-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 0.5rem;
	}

	.nav-arrow {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 1.75rem;
		height: 1.75rem;
		border-radius: 0.5rem;
		color: hsl(var(--foreground));
		transition: background-color 0.15s ease;
		flex-shrink: 0;
	}
	.nav-arrow:hover {
		background: hsl(var(--muted) / 0.7);
	}

	.head-title {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 0.25rem;
	}

	.head-static {
		font-size: 0.85rem;
		font-weight: 600;
		color: hsl(var(--foreground));
	}

	.title-btn {
		padding: 0.15rem 0.45rem;
		border-radius: 0.4rem;
		font-size: 0.85rem;
		font-weight: 600;
		color: hsl(var(--foreground));
		transition: background-color 0.15s ease, color 0.15s ease;
	}
	.title-btn:hover {
		background: hsl(var(--primary) / 0.1);
		color: hsl(var(--primary));
	}

	.weekday-row {
		display: grid;
		grid-template-columns: repeat(7, 1fr);
		margin-bottom: 0.15rem;
	}
	.weekday-cell {
		text-align: center;
		font-size: 0.65rem;
		font-weight: 600;
		color: hsl(var(--muted-foreground));
		padding-bottom: 0.2rem;
	}
	.weekday-cell.weekend {
		opacity: 0.6;
	}

	.day-grid {
		display: grid;
		grid-template-columns: repeat(7, 1fr);
		gap: 1px;
		transition: opacity 0.15s ease;
	}
	.day-grid.is-loading {
		opacity: 0.6;
	}

	.day-cell {
		position: relative;
		aspect-ratio: 1 / 1;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: 0.4rem;
		font-size: 0.72rem;
		color: hsl(var(--foreground));
		transition: background-color 0.15s ease, color 0.15s ease, transform 0.1s ease;
	}
	.day-cell.out {
		color: hsl(var(--muted-foreground) / 0.4);
	}
	.day-cell:not(:disabled):hover {
		background: hsl(var(--muted) / 0.75);
		transform: translateY(-1px);
	}
	.day-cell.has-entry {
		font-weight: 700;
		color: hsl(var(--primary));
	}
	.day-cell.is-today {
		box-shadow: inset 0 0 0 1.5px hsl(var(--ring) / 0.75);
	}
	.day-cell.is-selected {
		background: hsl(var(--primary));
		color: hsl(var(--primary-foreground));
	}
	.day-cell.is-selected:hover {
		background: hsl(var(--primary));
		transform: none;
	}
	.day-cell:disabled {
		color: hsl(var(--muted-foreground) / 0.3);
		cursor: default;
	}

	.day-num {
		line-height: 1;
	}
	.day-dot {
		position: absolute;
		bottom: 0.2rem;
		width: 3px;
		height: 3px;
		border-radius: 9999px;
		background: hsl(var(--primary) / 0.85);
	}

	.picker-foot {
		display: flex;
		justify-content: center;
		margin-top: 0.6rem;
		padding-top: 0.55rem;
		border-top: 1px solid hsl(var(--border) / 0.5);
	}

	.today-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.3rem 0.7rem;
		border-radius: 999px;
		font-size: 0.72rem;
		font-weight: 500;
		color: hsl(var(--foreground));
		background: hsl(var(--muted) / 0.6);
		transition: background-color 0.15s ease;
	}
	.today-btn:hover {
		background: hsl(var(--primary) / 0.14);
		color: hsl(var(--primary));
	}
	.today-dot {
		width: 6px;
		height: 6px;
		border-radius: 9999px;
		background: hsl(var(--primary));
	}

	/* ---- month grid ---- */
	.month-grid {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 0.35rem;
	}
	.month-cell {
		position: relative;
		padding: 0.55rem 0;
		border-radius: 0.5rem;
		font-size: 0.78rem;
		font-weight: 500;
		color: hsl(var(--foreground));
		background: hsl(var(--muted) / 0.35);
		transition: background-color 0.15s ease, color 0.15s ease, transform 0.1s ease;
	}
	.month-cell:not(:disabled):hover {
		background: hsl(var(--muted) / 0.8);
		transform: translateY(-1px);
	}
	.month-cell.has-entry {
		font-weight: 700;
		color: hsl(var(--primary));
	}
	.month-cell.is-current {
		box-shadow: inset 0 0 0 1.5px hsl(var(--ring) / 0.75);
	}
	.month-cell.is-selected {
		background: hsl(var(--primary));
		color: hsl(var(--primary-foreground));
	}
	.month-cell:disabled {
		color: hsl(var(--muted-foreground) / 0.3);
		background: transparent;
		cursor: default;
	}

	/* ---- year grid ---- */
	.year-grid {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 0.35rem;
	}
	.year-cell {
		padding: 0.55rem 0;
		border-radius: 0.5rem;
		font-size: 0.78rem;
		font-weight: 500;
		color: hsl(var(--foreground));
		background: hsl(var(--muted) / 0.35);
		transition: background-color 0.15s ease, color 0.15s ease, transform 0.1s ease;
	}
	.year-cell:not(:disabled):hover {
		background: hsl(var(--muted) / 0.8);
		transform: translateY(-1px);
	}
	.year-cell.is-current {
		box-shadow: inset 0 0 0 1.5px hsl(var(--ring) / 0.75);
	}
	.year-cell.is-selected {
		background: hsl(var(--primary));
		color: hsl(var(--primary-foreground));
	}
	.year-cell:disabled {
		color: hsl(var(--muted-foreground) / 0.3);
		background: transparent;
		cursor: default;
	}

	@media (max-width: 420px) {
		.picker-panel {
			width: 16rem;
		}
	}
</style>
