export interface ChartPoint {
	/** Short label shown on the x-axis, e.g. "8/3" or "2024". */
	label: string;
	/** Full label shown in the tooltip, e.g. "2026-08-03". */
	title?: string;
	value: number;
}
