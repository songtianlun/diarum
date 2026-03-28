export const MAX_DIARY_EMOJI_OPTION_LENGTH = 2;
export const MAX_DIARY_EMOJI_OPTION_COUNT = 12;

export const DEFAULT_MOOD_OPTIONS = [
	'😊',
	'😌',
	'🥳',
	'💪',
	'🤔',
	'😴',
	'😔',
	'😤'
];

export const DEFAULT_WEATHER_OPTIONS = [
	'☀️',
	'⛅',
	'☁️',
	'🌧️',
	'⛈️',
	'🌫️',
	'❄️',
	'🌬️'
];

export function countDisplayChars(value: string): number {
	return Array.from(value).length;
}

function sanitizeOptions(input: unknown, defaults: string[]): string[] {
	if (!Array.isArray(input)) {
		return [...defaults];
	}

	const seen = new Set<string>();
	const cleaned: string[] = [];

	for (const raw of input) {
		if (typeof raw !== 'string') continue;
		const value = raw.trim();
		if (!value) continue;
		if (countDisplayChars(value) > MAX_DIARY_EMOJI_OPTION_LENGTH) continue;
		if (seen.has(value)) continue;
		if (cleaned.length >= MAX_DIARY_EMOJI_OPTION_COUNT) break;
		seen.add(value);
		cleaned.push(value);
	}

	return cleaned.length > 0 ? cleaned : [...defaults];
}

export function sanitizeMoodOptions(input: unknown): string[] {
	return sanitizeOptions(input, DEFAULT_MOOD_OPTIONS);
}

export function sanitizeWeatherOptions(input: unknown): string[] {
	return sanitizeOptions(input, DEFAULT_WEATHER_OPTIONS);
}
