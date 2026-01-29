/**
 * Format date to YYYY-MM-DD (local timezone)
 */
export function formatDate(date: Date): string {
	const year = date.getFullYear();
	const month = String(date.getMonth() + 1).padStart(2, '0');
	const day = String(date.getDate()).padStart(2, '0');
	return `${year}-${month}-${day}`;
}

/**
 * Parse YYYY-MM-DD string to Date
 */
export function parseDate(dateStr: string): Date {
	return new Date(dateStr + 'T00:00:00');
}

/**
 * Get today's date string
 */
export function getToday(): string {
	return formatDate(new Date());
}

/**
 * Get previous day
 */
export function getPreviousDay(dateStr: string): string {
	const date = parseDate(dateStr);
	date.setDate(date.getDate() - 1);
	return formatDate(date);
}

/**
 * Get next day
 */
export function getNextDay(dateStr: string): string {
	const date = parseDate(dateStr);
	date.setDate(date.getDate() + 1);
	return formatDate(date);
}

/**
 * Format date for display (e.g., "January 28, 2024")
 */
export function formatDisplayDate(dateStr: string): string {
	const date = parseDate(dateStr);
	return date.toLocaleDateString('en-US', {
		year: 'numeric',
		month: 'long',
		day: 'numeric'
	});
}

/**
 * Format short date for mobile display (e.g., "Jan 28")
 */
export function formatShortDate(dateStr: string): string {
	const date = parseDate(dateStr);
	return date.toLocaleDateString('en-US', {
		month: 'short',
		day: 'numeric'
	});
}

/**
 * Get day of week (e.g., "Mon")
 */
export function getDayOfWeek(dateStr: string): string {
	const date = parseDate(dateStr);
	return date.toLocaleDateString('en-US', { weekday: 'short' });
}

/**
 * Check if date is today
 */
export function isToday(dateStr: string): boolean {
	return dateStr === getToday();
}

/**
 * Get start and end of month
 */
export function getMonthRange(year: number, month: number): { start: string; end: string } {
	const start = new Date(year, month - 1, 1);
	const end = new Date(year, month, 0);
	return {
		start: formatDate(start),
		end: formatDate(end)
	};
}

/**
 * Get calendar days for a month (including padding days)
 */
export function getCalendarDays(year: number, month: number): Date[] {
	const firstDay = new Date(year, month - 1, 1);
	const lastDay = new Date(year, month, 0);
	const startDay = firstDay.getDay(); // 0 = Sunday
	const daysInMonth = lastDay.getDate();

	const days: Date[] = [];

	// Add padding days from previous month
	for (let i = 0; i < startDay; i++) {
		const day = new Date(year, month - 1, -startDay + i + 1);
		days.push(day);
	}

	// Add days of current month
	for (let i = 1; i <= daysInMonth; i++) {
		days.push(new Date(year, month - 1, i));
	}

	// Add padding days from next month
	const endDay = lastDay.getDay();
	const remainingDays = 6 - endDay;
	for (let i = 1; i <= remainingDays; i++) {
		days.push(new Date(year, month, i));
	}

	return days;
}
