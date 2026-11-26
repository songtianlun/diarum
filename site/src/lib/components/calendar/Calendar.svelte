<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount, tick } from 'svelte';
	import { formatDate, getCalendarDays, getToday, getYearRange } from '$lib/utils/date';
	import { getDatesWithDiaries, type CalendarDiaryMeta } from '$lib/api/diaries';
	import { t, ta } from '$lib/i18n';
	import {
		cnHolidaysGlobalEnabled,
		cnHolidaysEnabled,
		holidayDaysByDate,
		ensureHolidayYears,
		getCNDayStatus,
		initHolidaySettings,
		toggleCNHolidays,
		type CNDayStatus
	} from '$lib/stores/holidays';

	export let currentYear: number;
	export let currentMonth: number;
	export let diaryMeta: CalendarDiaryMeta[] = [];

	type ViewMode = 'month' | 'year';
	let viewMode: ViewMode = 'month';
	export let yearViewActive = false;
	$: yearViewActive = viewMode === 'year';
	export let yearDiaryMeta: CalendarDiaryMeta[] = [];
	let yearLoading = false;
	let loadedYear: number | null = null;
	let transitionDirection: 'forward' | 'backward' = 'forward';
	let yearGridEl: HTMLDivElement;
	let wheelCooldown = false;
	let togglingHolidays = false;

	$: weekDays = $ta('calendar.weekdaysShort');
	$: weekDaysShort = $ta('calendar.weekdaysShort').map((d) => d.charAt(0));
	$: monthNames = $ta('calendar.months');
	$: monthNamesShort = $ta('calendar.monthsShort');

	$: calendarDays = getCalendarDays(currentYear, currentMonth);
	$: todayStr = getToday();
	$: metaByDate = new Map(diaryMeta.map(item => [item.date, item]));
	$: yearMetaByDate = new Map(yearDiaryMeta.map(item => [item.date, item]));

	onMount(() => {
		void initHolidaySettings(true);
	});

	$: if ($cnHolidaysEnabled) {
		const years = new Set<number>([currentYear]);
		for (const d of calendarDays) {
			years.add(d.getFullYear());
		}
		void ensureHolidayYears(Array.from(years));
	}

	async function handleHolidayToggle() {
		if (togglingHolidays) return;
		togglingHolidays = true;
		try {
			await toggleCNHolidays();
		} catch (e) {
			console.error('Failed to toggle CN holidays:', e);
		} finally {
			togglingHolidays = false;
		}
	}

	function getDayTooltip(dateStr: string, status: CNDayStatus | null): string {
		if (!status || !status.name) return dateStr;
		if (status.kind === 'holiday') {
			return `${dateStr} · ${$t('calendar.holidayTooltipOff', { name: status.name })}`;
		}
		if (status.kind === 'workday_override') {
			return `${dateStr} · ${$t('calendar.holidayTooltipWork', { name: status.name })}`;
		}
		return dateStr;
	}

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
		transitionDirection = 'backward';
		if (currentMonth === 1) {
			currentMonth = 12;
			currentYear--;
		} else {
			currentMonth--;
		}
	}

	function goToNextMonth() {
		transitionDirection = 'forward';
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

	// Year view functions
	async function enterYearView() {
		viewMode = 'year';
		await loadYearData(currentYear);
		scrollToCurrentMonth();
	}

	import { createEventDispatcher } from 'svelte';
	const dispatch = createEventDispatcher();

	function exitYearView(month: number) {
		currentMonth = month;
		viewMode = 'month';
		dispatch('monthchange');
	}

	async function loadYearData(year: number) {
		if (loadedYear === year) return;
		yearLoading = true;
		const range = getYearRange(year);
		yearDiaryMeta = await getDatesWithDiaries(range.start, range.end);
		loadedYear = year;
		yearLoading = false;
	}

	async function goToPreviousYear() {
		transitionDirection = 'backward';
		currentYear--;
		await loadYearData(currentYear);
	}

	async function goToNextYear() {
		transitionDirection = 'forward';
		currentYear++;
		await loadYearData(currentYear);
	}

	function goToCurrentYear() {
		const today = new Date();
		currentYear = today.getFullYear();
		loadYearData(currentYear);
	}

	function getMiniCalendarDays(year: number, month: number): (number | null)[] {
		const firstDay = new Date(year, month, 1);
		const lastDay = new Date(year, month + 1, 0);
		const startDay = firstDay.getDay();
		const daysInMonth = lastDay.getDate();

		const days: (number | null)[] = [];
		for (let i = 0; i < startDay; i++) {
			days.push(null);
		}
		for (let i = 1; i <= daysInMonth; i++) {
			days.push(i);
		}
		return days;
	}

	function yearHasDiary(month: number, day: number): boolean {
		const dateStr = `${currentYear}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
		return yearMetaByDate.has(dateStr);
	}

	function yearGetMeta(month: number, day: number): CalendarDiaryMeta | undefined {
		const dateStr = `${currentYear}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
		return yearMetaByDate.get(dateStr);
	}

	function isTodayMini(month: number, day: number): boolean {
		const dateStr = `${currentYear}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
		return dateStr === todayStr;
	}

	function isCurrentMonthMini(month: number): boolean {
		const today = new Date();
		return currentYear === today.getFullYear() && month === today.getMonth();
	}

	function handleMiniDayClick(e: Event, month: number, _day: number) {
		e.stopPropagation();
		exitYearView(month + 1);
	}

	function handleYearWheel(e: WheelEvent) {
		if (!yearGridEl || wheelCooldown || yearLoading) return;

		const el = yearGridEl;
		const atTop = el.scrollTop <= 0;
		const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 1;

		if ((atTop && e.deltaY < 0) || (atBottom && e.deltaY > 0)) {
			e.preventDefault();
			wheelCooldown = true;
			if (e.deltaY < 0) {
				goToPreviousYear();
			} else {
				goToNextYear();
			}
			setTimeout(() => { wheelCooldown = false; }, 600);
		}
	}

	async function scrollToCurrentMonth() {
		await tick();
		if (!yearGridEl) return;
		const today = new Date();
		if (currentYear !== today.getFullYear()) return;
		const targetMonth = today.getMonth();
		const cards = yearGridEl.querySelectorAll('.mini-month');
		if (cards[targetMonth]) {
			cards[targetMonth].scrollIntoView({ block: 'center', behavior: 'smooth' });
		}
	}
</script>

<div class="calendar">
	{#if viewMode === 'month'}
		<!-- Month View -->
		<div class="view-container animate-fade-in-only">
			<!-- Calendar Header -->
			<div class="flex items-center justify-between mb-6 px-2">
				<button
					on:click={goToPreviousMonth}
					class="p-2 rounded-lg hover:bg-muted/50 transition-all duration-200"
					title={$t('calendar.previousMonth')}
				>
					<svg class="w-5 h-5 text-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
					</svg>
				</button>

				<div class="flex items-center gap-2 sm:gap-3">
					<h2 class="text-lg font-semibold text-foreground flex items-center gap-1.5">
						<span>{monthNames[currentMonth - 1]}</span>
						<button
							on:click={enterYearView}
							class="year-button"
							title={$t('calendar.selectMonth')}
						>
							{currentYear}
						</button>
					</h2>
					<button
						on:click={goToToday}
						class="px-3 py-1 text-sm bg-primary text-primary-foreground rounded-md hover:opacity-90 transition-all duration-200"
					>
						{$t('calendar.today')}
					</button>
					{#if $cnHolidaysGlobalEnabled}
						<button
							type="button"
							on:click={handleHolidayToggle}
							disabled={togglingHolidays}
							aria-pressed={$cnHolidaysEnabled}
							title={$t('calendar.holidayToggleTitle')}
							class="holiday-toggle {$cnHolidaysEnabled ? 'holiday-toggle-active' : ''}"
						>
							{$t('calendar.holidayToggleShort')}
						</button>
					{/if}
				</div>

				<button
					on:click={goToNextMonth}
					class="p-2 rounded-lg hover:bg-muted/50 transition-all duration-200"
					title={$t('calendar.nextMonth')}
				>
					<svg class="w-5 h-5 text-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
					</svg>
				</button>
			</div>

			<!-- Week Days -->
			<div class="grid grid-cols-7 gap-2 mb-2">
				{#each weekDays as day, idx}
					<div
						class="text-center font-medium text-sm py-2
							{$cnHolidaysEnabled && (idx === 0 || idx === 6) ? 'text-rose-500/80 dark:text-rose-400/80' : 'text-muted-foreground'}"
					>
						{day}
					</div>
				{/each}
			</div>

			<!-- Calendar Days -->
			<div class="grid grid-cols-7 gap-2">
				{#each calendarDays as date, i}
					{@const dateStr = formatDate(date)}
					{@const inMonth = isCurrentMonth(date)}
					{@const meta = getDateMeta(date)}
					{@const holiday = $cnHolidaysEnabled ? getCNDayStatus(dateStr, $holidayDaysByDate) : null}
					{@const hasBadge = !!holiday?.badge}
					{@const showHolidayName = holiday?.kind === 'holiday' && holiday.isFirstHolidayDay && !!holiday.shortName}
					<button
						on:click={() => handleDateClick(date)}
						title={getDayTooltip(dateStr, holiday)}
						class="day aspect-square rounded-lg transition-all duration-200 flex flex-col items-center justify-center relative
							   {!inMonth ? 'text-muted-foreground/40' : holiday?.kind === 'holiday' ? 'text-rose-600 dark:text-rose-400 font-medium' : holiday?.kind === 'weekend' ? 'text-rose-500/80 dark:text-rose-400/80' : 'text-foreground'}
							   {isToday(date) ? 'bg-primary/10 ring-2 ring-primary font-semibold' : ''}
							   {hasDiary(date) && !isToday(date) ? 'bg-amber-500/10 dark:bg-amber-500/20' : ''}
							   {!isToday(date) && !hasDiary(date) ? 'hover:bg-muted/50' : ''}
							   {hasDiary(date) && !isToday(date) ? 'hover:bg-amber-500/20 dark:hover:bg-amber-500/30' : ''}"
						style="animation-delay: {i * 10}ms"
					>
						{#if hasBadge}
							<span
								class="holiday-badge {holiday?.kind === 'holiday' ? 'holiday-badge-off' : 'holiday-badge-work'}"
								class:opacity-50={!inMonth}
							>
								{holiday?.kind === 'holiday' ? $t('calendar.holidayBadgeOff') : $t('calendar.holidayBadgeWork')}
							</span>
						{/if}

						<span class="text-sm leading-none">{date.getDate()}</span>

						{#if showHolidayName}
							<span
								class="holiday-name hidden sm:block"
								class:opacity-50={!inMonth}
							>
								{holiday?.shortName}
							</span>
						{/if}

						{#if hasDiary(date)}
							{#if meta?.weather || meta?.mood}
								<div
									class="absolute top-1 flex items-center gap-0.5 text-[11px] leading-none {hasBadge ? 'left-1 justify-start' : 'inset-x-0 justify-center'}"
								>
									{#if meta?.weather}
										<span class="emoji-chip" title={$t('calendar.weatherLabel', { value: meta.weather })}>{meta.weather}</span>
									{/if}
									{#if meta?.mood}
										<span class="emoji-chip" title={$t('calendar.moodLabel', { value: meta.mood })}>{meta.mood}</span>
									{/if}
								</div>
							{:else}
								<span
									class="absolute w-1 h-1 bg-amber-500 rounded-full {showHolidayName ? 'bottom-1 left-1.5 sm:left-1.5' : 'bottom-1'}"
								></span>
							{/if}
						{/if}
					</button>
				{/each}
			</div>
		</div>
	{:else}
		<!-- Year View -->
		<div class="view-container animate-fade-in-only">
			<!-- Year Header -->
			<div class="flex items-center justify-between mb-5 px-2">
				<button
					on:click={goToPreviousYear}
					class="p-2 rounded-lg hover:bg-muted/50 transition-all duration-200"
					title={$t('calendar.previousYear')}
				>
					<svg class="w-5 h-5 text-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
					</svg>
				</button>

				<div class="flex items-center gap-2 sm:gap-3">
					<h2 class="text-lg font-semibold text-foreground">{currentYear}</h2>
					<button
						on:click={goToCurrentYear}
						class="px-3 py-1 text-sm bg-primary text-primary-foreground rounded-md hover:opacity-90 transition-all duration-200"
					>
						{$t('calendar.thisYearButton')}
					</button>
					{#if $cnHolidaysGlobalEnabled}
						<button
							type="button"
							on:click={handleHolidayToggle}
							disabled={togglingHolidays}
							aria-pressed={$cnHolidaysEnabled}
							title={$t('calendar.holidayToggleTitle')}
							class="holiday-toggle {$cnHolidaysEnabled ? 'holiday-toggle-active' : ''}"
						>
							{$t('calendar.holidayToggleShort')}
						</button>
					{/if}
				</div>

				<button
					on:click={goToNextYear}
					class="p-2 rounded-lg hover:bg-muted/50 transition-all duration-200"
					title={$t('calendar.nextYear')}
				>
					<svg class="w-5 h-5 text-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
					</svg>
				</button>
			</div>

			<!-- Year Grid (scrollable) -->
			<!-- svelte-ignore a11y-no-static-element-interactions -->
			<div
				class="year-scroll-container"
				bind:this={yearGridEl}
				on:wheel={handleYearWheel}
			>
				{#if yearLoading}
					<div class="flex items-center justify-center py-12">
						<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
						</svg>
					</div>
				{:else}
					<div class="year-grid">
						{#each Array(12) as _, monthIdx}
							<button
								class="mini-month"
								class:mini-month-current={isCurrentMonthMini(monthIdx)}
								on:click={() => exitYearView(monthIdx + 1)}
								style="animation-delay: {monthIdx * 30}ms"
							>
								<div class="mini-month-name" class:text-primary={isCurrentMonthMini(monthIdx)}>
									{monthNamesShort[monthIdx]}
								</div>
								<div class="mini-cal-grid">
									{#each weekDaysShort as wd}
										<div class="mini-weekday">{wd}</div>
									{/each}
									{#each getMiniCalendarDays(currentYear, monthIdx) as day}
										{#if day === null}
											<div class="mini-day-empty"></div>
										{:else}
											{@const miniDateStr = `${currentYear}-${String(monthIdx + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`}
											{@const miniHoliday = $cnHolidaysEnabled ? getCNDayStatus(miniDateStr, $holidayDaysByDate) : null}
											<!-- svelte-ignore a11y-click-events-have-key-events -->
											<div
												class="mini-day"
												class:mini-day-today={isTodayMini(monthIdx, day)}
												class:mini-day-has-diary={yearHasDiary(monthIdx, day)}
												class:mini-day-holiday={miniHoliday?.kind === 'holiday'}
												class:mini-day-workday-override={miniHoliday?.kind === 'workday_override'}
												title={getDayTooltip(miniDateStr, miniHoliday)}
												on:click|stopPropagation={(e) => handleMiniDayClick(e, monthIdx, day)}
												role="button"
												tabindex="-1"
											>
												<span class="mini-day-number">{day}</span>
											</div>
										{/if}
									{/each}
								</div>
							</button>
						{/each}
					</div>

					<!-- Scroll hint -->
					<div class="scroll-hint">
						<span class="scroll-hint-text">{$t('calendar.scrollToSwitchYear')}</span>
					</div>
				{/if}
			</div>
		</div>
	{/if}
</div>

<style>
	.calendar {
		width: 100%;
	}

	.view-container {
		width: 100%;
	}

	/* Year button in month view header */
	.year-button {
		display: inline-flex;
		align-items: center;
		padding: 0.125rem 0.5rem;
		border-radius: 0.375rem;
		transition: all 0.2s ease;
		position: relative;
	}

	.year-button::after {
		content: '';
		position: absolute;
		bottom: 0;
		left: 50%;
		transform: translateX(-50%);
		width: 0;
		height: 1.5px;
		background: hsl(var(--primary));
		transition: width 0.2s ease;
	}

	.year-button:hover {
		color: hsl(var(--primary));
		background: hsl(var(--primary) / 0.08);
	}

	.year-button:hover::after {
		width: 70%;
	}

	/* Scrollable year container */
	.year-scroll-container {
		max-height: 440px;
		overflow-y: auto;
		overflow-x: hidden;
		scroll-behavior: smooth;
		-webkit-overflow-scrolling: touch;
		padding-right: 2px;
		position: relative;
	}

	/* Fade edges to hint scrollability */
	.year-scroll-container::after {
		content: '';
		position: sticky;
		bottom: 0;
		left: 0;
		right: 0;
		display: block;
		height: 24px;
		background: linear-gradient(to top, hsl(var(--card)), transparent);
		pointer-events: none;
		margin-top: -24px;
	}

	/* Scroll hint text */
	.scroll-hint {
		display: flex;
		justify-content: center;
		padding: 0.5rem 0 0.25rem;
	}

	.scroll-hint-text {
		font-size: 0.6875rem;
		color: hsl(var(--muted-foreground) / 0.5);
		user-select: none;
	}

	/* Year grid */
	.year-grid {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 0.5rem;
	}

	@media (max-width: 640px) {
		.year-grid {
			grid-template-columns: repeat(2, 1fr);
			gap: 0.375rem;
		}

		.year-scroll-container {
			max-height: 380px;
		}
	}

	/* Mini month card */
	.mini-month {
		display: flex;
		flex-direction: column;
		padding: 0.5rem;
		border-radius: 0.625rem;
		border: 1px solid hsl(var(--border) / 0.5);
		background: hsl(var(--card));
		transition: all 0.2s ease;
		cursor: pointer;
		text-align: left;
		animation: mini-month-in 0.3s ease-out both;
	}

	.mini-month:hover {
		border-color: hsl(var(--primary) / 0.4);
		background: hsl(var(--primary) / 0.04);
		box-shadow: 0 2px 8px hsl(var(--primary) / 0.08);
		transform: translateY(-1px);
	}

	.mini-month-current {
		border-color: hsl(var(--primary) / 0.3);
		background: hsl(var(--primary) / 0.04);
	}

	@keyframes mini-month-in {
		from {
			opacity: 0;
			transform: scale(0.95);
		}
		to {
			opacity: 1;
			transform: scale(1);
		}
	}

	/* Mini month name */
	.mini-month-name {
		font-size: 0.8125rem;
		font-weight: 600;
		margin-bottom: 0.25rem;
		padding-left: 0.125rem;
		color: hsl(var(--foreground));
	}

	/* Mini calendar grid */
	.mini-cal-grid {
		display: grid;
		grid-template-columns: repeat(7, 1fr);
		gap: 0px;
	}

	.mini-weekday {
		text-align: center;
		font-size: 0.5625rem;
		color: hsl(var(--muted-foreground) / 0.6);
		padding: 0.0625rem 0;
		font-weight: 500;
		user-select: none;
	}

	.mini-day-empty {
		aspect-ratio: 1;
	}

	.mini-day {
		aspect-ratio: 1;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: 0.25rem;
		transition: all 0.15s ease;
		cursor: pointer;
		position: relative;
	}

	.mini-day:hover {
		background: hsl(var(--muted) / 0.8);
	}

	.mini-day-number {
		font-size: 0.5625rem;
		line-height: 1;
		color: hsl(var(--foreground) / 0.7);
		user-select: none;
	}

	.mini-day-today {
		background: hsl(var(--primary) / 0.15);
		border-radius: 50%;
	}

	.mini-day-today .mini-day-number {
		color: hsl(var(--primary));
		font-weight: 700;
	}

	.mini-day-has-diary {
		background: hsl(38 92% 50% / 0.15);
	}

	.mini-day-has-diary .mini-day-number {
		color: hsl(38 92% 40%);
		font-weight: 600;
	}

	.mini-day-has-diary:hover {
		background: hsl(38 92% 50% / 0.25);
	}

	:global(.dark) .mini-day-has-diary {
		background: hsl(38 92% 50% / 0.2);
	}

	:global(.dark) .mini-day-has-diary .mini-day-number {
		color: hsl(38 92% 65%);
	}

	:global(.dark) .mini-day-has-diary:hover {
		background: hsl(38 92% 50% / 0.3);
	}

	/* Today + has diary */
	.mini-day-today.mini-day-has-diary {
		background: hsl(var(--primary) / 0.15);
		box-shadow: 0 0 0 1.5px hsl(38 92% 50% / 0.4);
	}

	.mini-day-today.mini-day-has-diary .mini-day-number {
		color: hsl(var(--primary));
	}

	@media (max-width: 640px) {
		.day {
			font-size: 0.75rem;
		}

		.emoji-chip {
			font-size: 0.56rem;
			padding: 0.08rem 0.2rem;
		}

		.mini-month {
			padding: 0.375rem;
		}

		.mini-month-name {
			font-size: 0.75rem;
		}

		.mini-day-number {
			font-size: 0.5rem;
		}

		.mini-weekday {
			font-size: 0.5rem;
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

	.holiday-toggle {
		padding: 0.2rem 0.5rem;
		font-size: 0.75rem;
		font-weight: 500;
		line-height: 1.25;
		border-radius: 0.375rem;
		border: 1px solid hsl(var(--border) / 0.7);
		color: hsl(var(--muted-foreground));
		background: transparent;
		transition: all 0.2s ease;
	}

	.holiday-toggle:hover:not(:disabled) {
		background: hsl(var(--muted) / 0.5);
		color: hsl(var(--foreground));
	}

	.holiday-toggle-active {
		border-color: rgb(244 63 94 / 0.35);
		background: rgb(244 63 94 / 0.08);
		color: rgb(225 29 72);
	}

	:global(.dark) .holiday-toggle-active {
		border-color: rgb(251 113 133 / 0.35);
		background: rgb(244 63 94 / 0.16);
		color: rgb(251 113 133);
	}

	.holiday-badge {
		position: absolute;
		top: 0.25rem;
		right: 0.25rem;
		font-size: 0.5625rem;
		font-weight: 600;
		line-height: 1;
		padding: 0.125rem 0.2rem;
		border-radius: 0.25rem;
		pointer-events: none;
		user-select: none;
	}

	.holiday-badge-off {
		color: rgb(225 29 72);
		background: rgb(244 63 94 / 0.12);
	}

	:global(.dark) .holiday-badge-off {
		color: rgb(253 164 175);
		background: rgb(244 63 94 / 0.22);
	}

	.holiday-badge-work {
		color: rgb(71 85 105);
		background: rgb(100 116 139 / 0.14);
	}

	:global(.dark) .holiday-badge-work {
		color: rgb(203 213 225);
		background: rgb(148 163 184 / 0.2);
	}

	.holiday-name {
		position: absolute;
		bottom: 0.2rem;
		left: 0.15rem;
		right: 0.15rem;
		text-align: center;
		font-size: 0.625rem;
		line-height: 1.1;
		color: rgb(225 29 72 / 0.85);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		pointer-events: none;
		user-select: none;
	}

	:global(.dark) .holiday-name {
		color: rgb(251 113 133 / 0.9);
	}

	@media (max-width: 640px) {
		.holiday-badge {
			top: 0.125rem;
			right: 0.125rem;
			font-size: 0.5rem;
			padding: 0.08rem 0.14rem;
		}
	}

	.mini-day-holiday .mini-day-number {
		color: rgb(225 29 72);
		font-weight: 600;
	}

	:global(.dark) .mini-day-holiday .mini-day-number {
		color: rgb(251 113 133);
	}

	.mini-day-holiday:not(.mini-day-today):not(.mini-day-has-diary) {
		background: rgb(244 63 94 / 0.08);
	}

	.mini-day-workday-override::after {
		content: '';
		position: absolute;
		bottom: 1px;
		left: 28%;
		right: 28%;
		height: 1.5px;
		border-radius: 999px;
		background: rgb(100 116 139 / 0.75);
	}

	:global(.dark) .mini-day-workday-override::after {
		background: rgb(148 163 184 / 0.85);
	}
</style>
