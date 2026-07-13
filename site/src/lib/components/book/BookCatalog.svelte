<script lang="ts">
	import { onMount, tick } from 'svelte';
	import type { CalendarDiaryMeta } from '$lib/api/diaries';
	import { formatDate, getToday, addMonths } from '$lib/utils/date';

	export let entries: CalendarDiaryMeta[] = [];
	export let currentDate: string;
	export let onSelect: (date: string) => void = () => {};

	const WEEKDAYS = ['S', 'M', 'T', 'W', 'T', 'F', 'S'];
	const today = getToday();

	interface MonthBlock {
		key: string; // YYYY-MM
		label: string;
		blanks: number;
		days: { date: string; day: number; future: boolean }[];
	}

	$: metaByDate = new Map(entries.map((e) => [e.date, e]));

	function monthStart(dateStr: string): string {
		return dateStr.slice(0, 7) + '-01';
	}

	$: months = buildMonths(entries);

	function buildMonths(list: CalendarDiaryMeta[]): MonthBlock[] {
		let first = today;
		let last = today;
		for (const e of list) {
			if (e.date < first) first = e.date;
			if (e.date > last) last = e.date;
		}
		// earliest month -3 … latest month +1 (latest includes today so "today" is always reachable)
		const start = addMonths(monthStart(first), -3);
		const end = addMonths(monthStart(last), 1);

		const blocks: MonthBlock[] = [];
		let cursor = start;
		while (cursor <= end) {
			const year = Number(cursor.slice(0, 4));
			const month = Number(cursor.slice(5, 7));
			const firstDay = new Date(year, month - 1, 1);
			const daysInMonth = new Date(year, month, 0).getDate();
			const days = [];
			for (let i = 1; i <= daysInMonth; i++) {
				const dateStr = formatDate(new Date(year, month - 1, i));
				days.push({ date: dateStr, day: i, future: dateStr > today });
			}
			blocks.push({
				key: cursor.slice(0, 7),
				label: firstDay.toLocaleDateString('en-US', { month: 'long', year: 'numeric' }),
				blanks: firstDay.getDay(),
				days
			});
			cursor = addMonths(cursor, 1);
		}
		return blocks;
	}

	let scrollEl: HTMLElement;

	onMount(async () => {
		await tick();
		const target = scrollEl?.querySelector(`[data-month="${currentDate.slice(0, 7)}"]`);
		target?.scrollIntoView({ block: 'start' });
	});
</script>

<div class="catalog" bind:this={scrollEl}>
	<div class="catalog-grid">
		{#each months as m (m.key)}
			<section class="month" data-month={m.key}>
				<h3 class="month-label">{m.label}</h3>
				<div class="weekdays">
					{#each WEEKDAYS as w, i}
						<div class="weekday" class:weekend={i === 0 || i === 6}>{w}</div>
					{/each}
				</div>
				<div class="days">
					{#each Array(m.blanks) as _}
						<div></div>
					{/each}
					{#each m.days as d (d.date)}
						{@const meta = metaByDate.get(d.date)}
						<button
							class="day"
							class:has-entry={!!meta}
							class:is-today={d.date === today}
							class:is-current={d.date === currentDate}
							class:is-future={d.future}
							disabled={d.future}
							title="{d.date}{meta?.mood ? ' · ' + meta.mood : ''}{meta?.weather ? ' ' + meta.weather : ''}"
							on:click={() => onSelect(d.date)}
						>
							<span class="day-num">{d.day}</span>
							{#if meta}
								{#if meta.mood}
									<span class="day-mood">{meta.mood}</span>
								{:else}
									<span class="day-dot"></span>
								{/if}
							{/if}
						</button>
					{/each}
				</div>
			</section>
		{/each}
	</div>
</div>

<style>
	.catalog {
		position: absolute;
		inset: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		padding: 1rem;
		scroll-padding-top: 0.5rem;
	}

	.catalog-grid {
		max-width: 72rem;
		margin: 0 auto;
		display: grid;
		grid-template-columns: 1fr;
		gap: 1.25rem;
	}
	@media (min-width: 640px) {
		.catalog-grid {
			grid-template-columns: repeat(2, 1fr);
		}
	}
	@media (min-width: 1024px) {
		.catalog-grid {
			grid-template-columns: repeat(3, 1fr);
		}
	}

	.month {
		background: hsl(var(--card));
		border: 1px solid hsl(var(--border) / 0.6);
		border-radius: 0.75rem;
		padding: 0.85rem 0.9rem 1rem;
		scroll-margin-top: 0.5rem;
	}

	.month-label {
		font-family: ui-serif, Georgia, serif;
		font-size: 0.95rem;
		font-weight: 600;
		margin-bottom: 0.5rem;
		color: hsl(var(--foreground));
	}

	.weekdays,
	.days {
		display: grid;
		grid-template-columns: repeat(7, 1fr);
		gap: 2px;
	}

	.weekday {
		text-align: center;
		font-size: 0.62rem;
		font-weight: 600;
		color: hsl(var(--muted-foreground));
		padding-bottom: 0.25rem;
	}
	.weekday.weekend {
		opacity: 0.6;
	}

	.day {
		position: relative;
		aspect-ratio: 1 / 0.92;
		min-height: 2.1rem;
		border-radius: 0.45rem;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: flex-start;
		padding-top: 0.15rem;
		font-size: 0.68rem;
		color: hsl(var(--muted-foreground));
		transition: background-color 0.15s ease, transform 0.15s ease;
	}
	.day:not(:disabled):hover {
		background: hsl(var(--muted) / 0.8);
		transform: translateY(-1px);
	}
	.day.has-entry {
		background: hsl(var(--accent) / 0.85);
		color: hsl(var(--accent-foreground));
		font-weight: 600;
	}
	.day.has-entry:not(:disabled):hover {
		background: hsl(var(--accent));
	}
	.day.is-today {
		box-shadow: inset 0 0 0 1.5px hsl(var(--ring) / 0.8);
	}
	.day.is-current {
		box-shadow: inset 0 0 0 2px hsl(var(--primary));
	}
	.day.is-future {
		opacity: 0.3;
		cursor: default;
	}

	.day-num {
		line-height: 1.1;
	}
	.day-mood {
		font-size: 0.85rem;
		line-height: 1;
		margin-top: 0.05rem;
	}
	.day-dot {
		width: 5px;
		height: 5px;
		border-radius: 9999px;
		background: hsl(var(--primary) / 0.75);
		margin-top: 0.35rem;
	}
</style>
