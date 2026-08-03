<script lang="ts">
	import { onMount } from 'svelte';
	import { locale } from '$lib/i18n';
	import { formatHumanNumber } from '$lib/utils/number';
	import type { ChartPoint } from './types';

	export let points: ChartPoint[] = [];
	export let height = 220;
	/** Unit appended to tooltip values, e.g. "words". */
	export let unit = '';
	export let emptyLabel = 'No data';
	/** Draw a dot on every point. Off by default so dense series stay clean. */
	export let showDots = false;

	let container: HTMLDivElement;
	let width = 0;
	let hoverIndex: number | null = null;

	// Unique per instance so multiple charts on one page keep their own fill.
	const gradientId = `chart-area-${Math.random().toString(36).slice(2, 9)}`;
	const padding = { top: 18, right: 12, bottom: 24, left: 40 };

	$: innerWidth = Math.max(width - padding.left - padding.right, 1);
	$: innerHeight = Math.max(height - padding.top - padding.bottom, 1);
	$: maxValue = points.length ? Math.max(...points.map((p) => p.value)) : 0;
	// A flat-zero series still deserves a sensible axis instead of a divide by zero.
	$: scaleMax = niceCeil(maxValue);
	$: ticks = [0, scaleMax / 2, scaleMax];
	$: coords = points.map((point, index) => ({
		x:
			padding.left +
			(points.length === 1 ? innerWidth / 2 : (index / (points.length - 1)) * innerWidth),
		y: padding.top + innerHeight - (point.value / scaleMax) * innerHeight
	}));
	$: linePath = buildSmoothPath(coords);
	$: areaPath = coords.length
		? `${linePath} L ${round(coords[coords.length - 1].x)} ${round(padding.top + innerHeight)} L ${round(coords[0].x)} ${round(padding.top + innerHeight)} Z`
		: '';
	$: xLabels = pickXLabels(points, width);
	// Dots would turn into a solid bar on a long daily series.
	$: dotsVisible = showDots && points.length <= 60;
	$: active =
		hoverIndex !== null && coords[hoverIndex]
			? { point: points[hoverIndex], coord: coords[hoverIndex] }
			: null;
	// Keep the tooltip inside the chart bounds on narrow screens.
	$: tooltipX = active ? Math.min(Math.max(active.coord.x, 58), Math.max(width - 58, 58)) : 0;

	/** Round the axis top up to a friendly 1/2/5 x 10^n value. */
	function niceCeil(value: number): number {
		if (!value || value <= 0) return 10;
		const magnitude = Math.pow(10, Math.floor(Math.log10(value)));
		const normalized = value / magnitude;
		const step = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10;
		return step * magnitude;
	}

	function round(value: number): number {
		return Math.round(value * 100) / 100;
	}

	/**
	 * Cubic smoothing with a low tension, which reads as an organic curve
	 * without the overshoot a naive spline produces. Long series fall back to
	 * straight segments: they are faster and more honest at that density.
	 */
	function buildSmoothPath(list: { x: number; y: number }[]): string {
		if (!list.length) return '';
		if (list.length === 1) return `M ${round(list[0].x)} ${round(list[0].y)}`;
		if (list.length > 90) {
			return list.map((p, i) => `${i === 0 ? 'M' : 'L'} ${round(p.x)} ${round(p.y)}`).join(' ');
		}

		const tension = 0.2;
		let path = `M ${round(list[0].x)} ${round(list[0].y)}`;
		for (let i = 0; i < list.length - 1; i++) {
			const p0 = list[i - 1] ?? list[i];
			const p1 = list[i];
			const p2 = list[i + 1];
			const p3 = list[i + 2] ?? p2;
			const c1x = p1.x + (p2.x - p0.x) * tension;
			const c1y = p1.y + (p2.y - p0.y) * tension;
			const c2x = p2.x - (p3.x - p1.x) * tension;
			const c2y = p2.y - (p3.y - p1.y) * tension;
			path += ` C ${round(c1x)} ${round(c1y)}, ${round(c2x)} ${round(c2y)}, ${round(p2.x)} ${round(p2.y)}`;
		}
		return path;
	}

	/** Thin x-axis labels down to whatever comfortably fits the width. */
	function pickXLabels(list: ChartPoint[], w: number): { index: number; label: string }[] {
		if (!list.length || w <= 0) return [];
		const maxLabels = Math.max(2, Math.min(7, Math.floor(w / 68)));
		if (list.length <= maxLabels) {
			return list.map((point, index) => ({ index, label: point.label }));
		}
		const step = (list.length - 1) / (maxLabels - 1);
		const result: { index: number; label: string }[] = [];
		let previous = -1;
		for (let i = 0; i < maxLabels; i++) {
			const index = Math.round(i * step);
			if (index === previous) continue;
			previous = index;
			result.push({ index, label: list[index].label });
		}
		return result;
	}

	function handlePointer(event: PointerEvent) {
		if (!points.length || innerWidth <= 0) return;
		const rect = (event.currentTarget as SVGRectElement).getBoundingClientRect();
		const ratio = Math.min(Math.max((event.clientX - rect.left) / rect.width, 0), 1);
		hoverIndex = Math.round(ratio * (points.length - 1));
	}

	function clearHover() {
		hoverIndex = null;
	}

	onMount(() => {
		width = container.clientWidth;
		const observer = new ResizeObserver((entries) => {
			width = entries[0]?.contentRect.width ?? 0;
		});
		observer.observe(container);
		return () => observer.disconnect();
	});
</script>

<div class="relative w-full select-none touch-pan-y" bind:this={container} style="height: {height}px">
	{#if width > 0 && points.length}
		<svg {width} {height} role="presentation" aria-hidden="true">
			<defs>
				<linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
					<stop offset="0%" stop-color="hsl(var(--primary))" stop-opacity="0.22" />
					<stop offset="100%" stop-color="hsl(var(--primary))" stop-opacity="0" />
				</linearGradient>
			</defs>

			<!-- Horizontal guides + y-axis labels -->
			{#each ticks as tick}
				{@const y = padding.top + innerHeight - (tick / scaleMax) * innerHeight}
				<line
					x1={padding.left}
					y1={y}
					x2={width - padding.right}
					y2={y}
					stroke="hsl(var(--border))"
					stroke-width="1"
					stroke-dasharray={tick === 0 ? '0' : '3 4'}
					opacity="0.7"
				/>
				<text
					x={padding.left - 8}
					y={y + 3.5}
					text-anchor="end"
					class="fill-muted-foreground"
					style="font-size: 10px"
				>
					{formatHumanNumber(tick, $locale)}
				</text>
			{/each}

			<path d={areaPath} fill="url(#{gradientId})" />
			<path
				d={linePath}
				fill="none"
				stroke="hsl(var(--primary))"
				stroke-width="2"
				stroke-linecap="round"
				stroke-linejoin="round"
			/>

			{#if dotsVisible}
				{#each coords as coord}
					<circle
						cx={coord.x}
						cy={coord.y}
						r="2.5"
						fill="hsl(var(--card))"
						stroke="hsl(var(--primary))"
						stroke-width="1.5"
					/>
				{/each}
			{/if}

			{#if active}
				<line
					x1={active.coord.x}
					y1={padding.top}
					x2={active.coord.x}
					y2={padding.top + innerHeight}
					stroke="hsl(var(--primary))"
					stroke-width="1"
					opacity="0.35"
				/>
				<circle cx={active.coord.x} cy={active.coord.y} r="7" fill="hsl(var(--primary))" opacity="0.16" />
				<circle
					cx={active.coord.x}
					cy={active.coord.y}
					r="3.5"
					fill="hsl(var(--primary))"
					stroke="hsl(var(--card))"
					stroke-width="1.5"
				/>
			{/if}

			<!-- X-axis labels -->
			{#each xLabels as item}
				{#if coords[item.index]}
					<text
						x={coords[item.index].x}
						y={height - 6}
						text-anchor="middle"
						class="fill-muted-foreground"
						style="font-size: 10px"
					>
						{item.label}
					</text>
				{/if}
			{/each}

			<rect
				x={padding.left}
				y={padding.top}
				width={innerWidth}
				height={innerHeight}
				fill="transparent"
				on:pointermove={handlePointer}
				on:pointerdown={handlePointer}
				on:pointerleave={clearHover}
				on:pointercancel={clearHover}
				on:pointerup={clearHover}
			/>
		</svg>

		{#if active}
			<div
				class="pointer-events-none absolute z-10 -translate-x-1/2 rounded-lg border border-border/60 bg-card/95 px-2.5 py-1.5 shadow-lg"
				style="left: {tooltipX}px; top: 0px"
			>
				<div class="text-[10px] leading-tight text-muted-foreground whitespace-nowrap">
					{active.point.title ?? active.point.label}
				</div>
				<div class="text-xs font-semibold leading-tight text-foreground whitespace-nowrap">
					{active.point.value.toLocaleString()}{unit ? ` ${unit}` : ''}
				</div>
			</div>
		{/if}
	{:else if !points.length}
		<div class="absolute inset-0 flex items-center justify-center text-sm text-muted-foreground">
			{emptyLabel}
		</div>
	{/if}
</div>
