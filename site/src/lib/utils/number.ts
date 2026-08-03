import type { Locale } from '$lib/i18n';

interface AbbreviationUnit {
	limit: number;
	suffix: string;
}

// Ordered largest-limit first. Chinese counts in 万/亿 rather than thousands,
// so the two languages need genuinely different scales, not a translated suffix.
const UNITS_BY_LOCALE: Record<Locale, AbbreviationUnit[]> = {
	zh: [
		{ limit: 1e8, suffix: '亿' },
		{ limit: 1e4, suffix: '万' },
		{ limit: 1e3, suffix: '千' }
	],
	en: [
		{ limit: 1e9, suffix: 'B' },
		{ limit: 1e6, suffix: 'M' },
		{ limit: 1e3, suffix: 'k' }
	]
};

/** One decimal at most, with a bare integer preferred: 1 -> "1", 1.9 -> "1.9". */
function formatScaled(value: number): string {
	return Number.isInteger(value) ? String(value) : value.toFixed(1);
}

function roundToTenth(value: number): number {
	return Math.round(value * 10) / 10;
}

/**
 * Abbreviate a count the way each language reads it: 1,034 becomes "1k" in
 * English and "1千" in Chinese, 19,238 becomes "19.2k" / "1.9万".
 *
 * Values below the smallest unit are returned as-is, so short numbers never
 * get dressed up in a suffix they do not need.
 */
export function formatHumanNumber(value: number, locale: Locale): string {
	if (!Number.isFinite(value)) return '0';

	const sign = value < 0 ? '-' : '';
	const abs = Math.abs(value);
	const units = UNITS_BY_LOCALE[locale] ?? UNITS_BY_LOCALE.en;

	for (let i = 0; i < units.length; i++) {
		const { limit, suffix } = units[i];
		if (abs < limit) continue;

		const scaled = roundToTenth(abs / limit);
		// Rounding can push a value into the next unit up (9,999 would read as
		// "10千"), so promote it rather than print an out-of-range figure.
		const larger = units[i - 1];
		if (larger && scaled * limit >= larger.limit) {
			return sign + formatScaled(roundToTenth(abs / larger.limit)) + larger.suffix;
		}
		return sign + formatScaled(scaled) + suffix;
	}

	// Below the smallest unit the exact value is already readable — but keep
	// fractional inputs (chart axis ticks) from printing a long tail.
	return sign + formatScaled(roundToTenth(abs));
}
