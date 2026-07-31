<script lang="ts">
	/**
	 * The month grid from the Win95 common controls (SysMonthCal32): navy
	 * caption strip, single-letter weekday header, today's date boxed in red
	 * and repeated on the footer line.
	 *
	 * Entry metadata is passed in rather than fetched so the popup opens
	 * instantly and stays consistent with the contents tree.
	 */
	import type { CalendarDiaryMeta } from '$lib/api/diaries';
	import { formatDate, getCalendarDays, getToday } from '$lib/utils/date';
	import { t, ta } from '$lib/i18n';

	export let value: string;
	export let entries: CalendarDiaryMeta[] = [];
	export let onSelect: (date: string) => void;
	export let onClose: (() => void) | null = null;

	let viewYear = Number(value.slice(0, 4));
	let viewMonth = Number(value.slice(5, 7));
	let syncedTo = value;

	// Follow the selection when the entry changes underneath us.
	$: if (value !== syncedTo) {
		syncedTo = value;
		viewYear = Number(value.slice(0, 4));
		viewMonth = Number(value.slice(5, 7));
	}

	$: months = $ta('calendar.months');
	$: weekdays = $ta('calendar.weekdaysShort').map((d) => d.slice(0, 2));
	$: today = getToday();
	$: haveEntry = new Set(entries.map((e) => e.date));
	$: days = getCalendarDays(viewYear, viewMonth);

	function step(delta: number) {
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

	function inMonth(d: Date) {
		return d.getMonth() === viewMonth - 1;
	}

	function pick(d: Date) {
		const iso = formatDate(d);
		if (iso > today) return;
		onSelect(iso);
	}

	function jumpToToday() {
		viewYear = Number(today.slice(0, 4));
		viewMonth = Number(today.slice(5, 7));
		onSelect(today);
	}
</script>

<div class="cal w95-out">
	<div class="cal-head">
		<button type="button" class="arrow" on:click={() => step(-1)} aria-label={$t('calendar.previousMonth')}>
			<svg width="7" height="9" viewBox="0 0 7 9" shape-rendering="crispEdges"
				><path d="M5 0h2v9H5zM3 2h2v5H3zM1 4h2v1H1z" fill="#fff" /></svg
			>
		</button>
		<div class="cal-title">{viewYear} · {months[viewMonth - 1] ?? viewMonth}</div>
		<button type="button" class="arrow" on:click={() => step(1)} aria-label={$t('calendar.nextMonth')}>
			<svg width="7" height="9" viewBox="0 0 7 9" shape-rendering="crispEdges"
				><path d="M0 0h2v9H0zM2 2h2v5H2zM4 4h2v1H4z" fill="#fff" /></svg
			>
		</button>
	</div>

	<table class="cal-grid">
		<thead>
			<tr>
				{#each weekdays as wd}
					<th scope="col">{wd}</th>
				{/each}
			</tr>
		</thead>
		<tbody>
			{#each Array(Math.ceil(days.length / 7)) as _, week}
				<tr>
					{#each days.slice(week * 7, week * 7 + 7) as day}
						{@const iso = formatDate(day)}
						<td>
							<button
								type="button"
								class="cal-day"
								class:out={!inMonth(day)}
								class:sel={iso === value}
								class:has={haveEntry.has(iso)}
								class:now={iso === today}
								disabled={iso > today}
								on:click={() => pick(day)}
							>
								{day.getDate()}
							</button>
						</td>
					{/each}
				</tr>
			{/each}
		</tbody>
	</table>

	<div class="cal-foot">
		<button type="button" class="cal-today" on:click={jumpToToday}>
			<span class="dot"></span>
			<span>{$t('calendar.today')}: {today}</span>
		</button>
		{#if onClose}
			<button type="button" class="w95-btn cal-close" on:click={onClose}>{$t('win95.close')}</button>
		{/if}
	</div>
</div>

<style>
	.cal {
		width: 210px;
		padding: 3px;
		user-select: none;
	}

	.cal-head {
		display: flex;
		align-items: center;
		gap: 2px;
		height: 18px;
		padding: 0 2px;
		background: #000080;
		color: #fff;
	}

	.cal-title {
		flex: 1;
		text-align: center;
		font-size: 11px;
		font-weight: 700;
		white-space: nowrap;
		overflow: hidden;
	}

	.arrow {
		width: 15px;
		height: 15px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: transparent;
	}
	.arrow:hover {
		background: rgba(255, 255, 255, 0.18);
	}
	.arrow svg {
		display: block;
	}

	.cal-grid {
		width: 100%;
		border-collapse: collapse;
		table-layout: fixed;
		margin-top: 2px;
	}

	.cal-grid th {
		font-size: 11px;
		font-weight: 400;
		color: #000080;
		padding: 1px 0 2px;
		border-bottom: 1px solid #808080;
	}

	.cal-day {
		width: 100%;
		height: 17px;
		font-size: 11px;
		line-height: 1;
		color: #000;
		background: transparent;
		text-align: center;
	}

	.cal-day.out {
		color: #909090;
	}

	.cal-day.has {
		font-weight: 700;
		color: #000080;
	}

	.cal-day.now {
		box-shadow: inset 0 0 0 1px #c00000;
	}

	.cal-day.sel {
		background: #000080;
		color: #fff;
	}

	.cal-day:hover:not(:disabled):not(.sel) {
		background: #d0d0d0;
	}

	.cal-day:disabled {
		color: #b0b0b0;
	}
	.cal-day.out:disabled {
		color: #c8c8c8;
	}

	.cal-foot {
		display: flex;
		align-items: center;
		gap: 6px;
		margin-top: 3px;
		padding: 3px 2px 1px;
		border-top: 1px solid #808080;
		box-shadow: inset 0 1px 0 0 #fff;
	}

	.cal-today {
		display: flex;
		align-items: center;
		gap: 5px;
		font-size: 11px;
		color: #000;
		background: transparent;
		padding: 1px 2px;
	}
	.cal-today:hover {
		text-decoration: underline;
	}

	.dot {
		width: 9px;
		height: 9px;
		background: #c00000;
		border: 1px solid #800000;
		flex-shrink: 0;
	}

	.cal-close {
		margin-left: auto;
		min-height: 19px;
		padding: 2px 8px;
		font-size: 11px;
	}
</style>
