<script lang="ts">
	import { goto } from '$app/navigation';
	import { formatDate, getCalendarDays, getToday } from '$lib/utils/date';
	import type { CalendarDiaryMeta } from '$lib/api/diaries';

	export let currentYear: number;
	export let currentMonth: number;
	export let diaryMeta: CalendarDiaryMeta[] = [];

	const weekDays = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
	const monthNames = [
		'January', 'February', 'March', 'April', 'May', 'June',
		'July', 'August', 'September', 'October', 'November', 'December'
	];

	$: calendarDays = getCalendarDays(currentYear, currentMonth);
	$: todayStr = getToday();
	$: metaByDate = new Map(diaryMeta.map(item => [item.date, item]));

	function isCurrentMonth(date: Date): boolean {
		return date.getMonth() === currentMonth - 1;
	}

	function isToday(date: Date): boolean {
		return formatDate(date) === todayStr;
	}

	function hasDiary(date: Date): boolean {
		return metaByDate.has(formatDate(date));
	}

	function getDateMeta(date: Date): CalendarDiaryMeta | undefined {
		return metaByDate.get(formatDate(date));
	}

	function handleDateClick(date: Date) {
		goto(`/diary/${formatDate(date)}`);
	}

	function goToPreviousMonth() {
		if (currentMonth === 1) {
			currentMonth = 12;
			currentYear--;
		} else {
			currentMonth--;
		}
	}

	function goToNextMonth() {
		if (currentMonth === 12) {
			currentMonth = 1;
			currentYear++;
		} else {
			currentMonth++;
		}
	}

	function goToToday() {
		const today = new Date();
		currentYear = today.getFullYear();
		currentMonth = today.getMonth() + 1;
	}
</script>

<div class="calendar">
	<!-- Calendar Header -->
	<div class="flex items-center justify-between mb-6 px-2">
		<button
			on:click={goToPreviousMonth}
			class="p-2 rounded-lg hover:bg-muted/50 transition-all duration-200"
			title="Previous month"
		>
			<svg class="w-5 h-5 text-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
			</svg>
		</button>

		<div class="flex items-center gap-3">
			<h2 class="text-lg font-semibold text-foreground">
				{monthNames[currentMonth - 1]} {currentYear}
			</h2>
			<button
				on:click={goToToday}
				class="px-3 py-1 text-sm bg-primary text-primary-foreground rounded-md hover:opacity-90 transition-all duration-200"
			>
				Today
			</button>
		</div>

		<button
			on:click={goToNextMonth}
			class="p-2 rounded-lg hover:bg-muted/50 transition-all duration-200"
			title="Next month"
		>
			<svg class="w-5 h-5 text-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
			</svg>
		</button>
	</div>

	<!-- Week Days -->
	<div class="grid grid-cols-7 gap-2 mb-2">
		{#each weekDays as day}
			<div class="text-center font-medium text-muted-foreground text-sm py-2">{day}</div>
		{/each}
	</div>

	<!-- Calendar Days -->
	<div class="grid grid-cols-7 gap-2">
		{#each calendarDays as date, i}
			<button
				on:click={() => handleDateClick(date)}
				class="day aspect-square rounded-lg transition-all duration-200 flex flex-col items-center justify-center relative
					   {isCurrentMonth(date) ? 'text-foreground' : 'text-muted-foreground/40'}
					   {isToday(date) ? 'bg-primary/10 ring-2 ring-primary font-semibold' : ''}
					   {hasDiary(date) && !isToday(date) ? 'bg-amber-500/10 dark:bg-amber-500/20' : ''}
					   {!isToday(date) && !hasDiary(date) ? 'hover:bg-muted/50' : ''}
					   {hasDiary(date) && !isToday(date) ? 'hover:bg-amber-500/20 dark:hover:bg-amber-500/30' : ''}"
				style="animation-delay: {i * 10}ms"
			>
				<span class="text-sm">{date.getDate()}</span>

				{#if hasDiary(date)}
					{@const meta = getDateMeta(date)}
					{#if meta?.weather || meta?.mood}
						<div class="absolute inset-x-0 top-1.5 flex items-center justify-center gap-1 text-[11px] leading-none">
							{#if meta?.weather}
								<span class="emoji-chip" title={`Weather: ${meta.weather}`}>{meta.weather}</span>
							{/if}
							{#if meta?.mood}
								<span class="emoji-chip" title={`Mood: ${meta.mood}`}>{meta.mood}</span>
							{/if}
						</div>
					{:else}
						<span class="absolute bottom-1 w-1 h-1 bg-amber-500 rounded-full"></span>
					{/if}
				{/if}
			</button>
		{/each}
	</div>
</div>

<style>
	.calendar {
		width: 100%;
	}

	@media (max-width: 640px) {
		.day {
			font-size: 0.75rem;
		}

		.emoji-chip {
			font-size: 0.56rem;
			padding: 0.08rem 0.2rem;
		}
	}

	.emoji-chip {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		padding: 0.1rem 0.25rem;
		border-radius: 999px;
		background: color-mix(in srgb, var(--muted) 75%, transparent);
		backdrop-filter: blur(2px);
	}
</style>
