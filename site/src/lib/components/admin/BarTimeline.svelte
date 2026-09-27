<script lang="ts">
	/**
	 * Column chart of requests per bucket with the error share stacked on
	 * top. Scales to its container; hovering or tapping a column shows its
	 * numbers.
	 */
	import { getIntlLocale } from '$lib/i18n';

	interface Point {
		start: string;
		count: number;
		errors: number;
	}

	let {
		points = [],
		bucket = 'hour',
		height = 160,
		requestsLabel = 'Requests',
		errorsLabel = 'Errors',
		emptyLabel = 'No data'
	}: {
		points?: Point[];
		bucket?: 'hour' | 'day';
		height?: number;
		requestsLabel?: string;
		errorsLabel?: string;
		emptyLabel?: string;
	} = $props();

	let width = $state(0);
	let hover = $state<number | null>(null);

	const pad = { top: 8, right: 4, bottom: 20, left: 36 };
	let max = $derived(Math.max(1, ...points.map((p) => p.count)));
	let niceMax = $derived(niceCeil(max));
	let plotW = $derived(Math.max(0, width - pad.left - pad.right));
	let plotH = $derived(height - pad.top - pad.bottom);
	let step = $derived(points.length ? plotW / points.length : 0);
	let barW = $derived(Math.max(1, step * 0.72));
	let labelEvery = $derived(Math.max(1, Math.ceil(points.length / Math.max(1, Math.floor(plotW / 56)))));
	let total = $derived(points.reduce((sum, p) => sum + p.count, 0));

	function niceCeil(value: number): number {
		const exp = Math.pow(10, Math.floor(Math.log10(value)));
		for (const m of [1, 2, 2.5, 5, 10]) {
			if (m * exp >= value) return m * exp;
		}
		return 10 * exp;
	}

	function y(value: number): number {
		return pad.top + plotH - (value / niceMax) * plotH;
	}

	function label(start: string, long = false): string {
		const date = new Date(start);
		if (bucket === 'hour') {
			return long
				? date.toLocaleString(getIntlLocale(), { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
				: date.toLocaleTimeString(getIntlLocale(), { hour: '2-digit', minute: '2-digit' });
		}
		return date.toLocaleDateString(getIntlLocale(), long ? { year: 'numeric', month: 'short', day: 'numeric', weekday: 'short' } : { month: 'numeric', day: 'numeric' });
	}

	function pick(event: PointerEvent) {
		const rect = (event.currentTarget as SVGElement).getBoundingClientRect();
		const x = event.clientX - rect.left - pad.left;
		const index = Math.floor(x / step);
		hover = index >= 0 && index < points.length ? index : null;
	}
</script>

<div class="relative w-full select-none" bind:clientWidth={width} style="height: {height}px">
	{#if total === 0}
		<div class="absolute inset-0 flex items-center justify-center text-sm text-muted-foreground">{emptyLabel}</div>
	{/if}
	{#if width > 0}
		<svg
			{width}
			{height}
			role="img"
			aria-label={requestsLabel}
			class="overflow-visible touch-pan-y"
			onpointermove={pick}
			onpointerdown={pick}
			onpointerleave={() => (hover = null)}
		>
			{#each [0, 0.5, 1] as f}
				<line x1={pad.left} x2={width - pad.right} y1={y(niceMax * f)} y2={y(niceMax * f)} class="stroke-border/60" stroke-dasharray={f === 0 ? '' : '3 3'} />
				<text x={pad.left - 6} y={y(niceMax * f) + 3} text-anchor="end" class="fill-muted-foreground text-[10px] tabular-nums">{Math.round(niceMax * f).toLocaleString(getIntlLocale())}</text>
			{/each}
			{#each points as point, i}
				{@const x = pad.left + i * step + (step - barW) / 2}
				<g opacity={hover === null || hover === i ? 1 : 0.45}>
					{#if point.count > 0}
						<rect {x} y={y(point.count)} width={barW} height={Math.max(1, y(0) - y(point.count))} rx={Math.min(2, barW / 2)} class="fill-primary/70" />
					{/if}
					{#if point.errors > 0}
						<rect {x} y={y(point.errors)} width={barW} height={Math.max(1, y(0) - y(point.errors))} rx={Math.min(2, barW / 2)} class="fill-rose-500/85" />
					{/if}
				</g>
				{#if i % labelEvery === 0}
					<text x={pad.left + i * step + step / 2} y={height - 4} text-anchor="middle" class="fill-muted-foreground text-[10px]">{label(point.start)}</text>
				{/if}
			{/each}
		</svg>
		{#if hover !== null && points[hover]}
			{@const point = points[hover]}
			{@const left = Math.min(Math.max(pad.left + hover * step + step / 2, 70), width - 70)}
			<div class="pointer-events-none absolute -top-2 z-10 -translate-x-1/2 -translate-y-full rounded-lg border border-border/60 bg-card/95 px-2.5 py-1.5 text-xs shadow-md backdrop-blur whitespace-nowrap" style="left: {left}px">
				<div class="font-medium text-foreground">{label(point.start, true)}</div>
				<div class="mt-0.5 flex items-center gap-1.5 text-muted-foreground"><span class="h-2 w-2 rounded-sm bg-primary/70"></span>{requestsLabel} <span class="ml-auto pl-3 tabular-nums text-foreground">{point.count.toLocaleString(getIntlLocale())}</span></div>
				<div class="flex items-center gap-1.5 text-muted-foreground"><span class="h-2 w-2 rounded-sm bg-rose-500/85"></span>{errorsLabel} <span class="ml-auto pl-3 tabular-nums text-foreground">{point.errors.toLocaleString(getIntlLocale())}</span></div>
			</div>
		{/if}
	{/if}
</div>
