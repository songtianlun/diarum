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
	/** Show a range slider under the chart. Use for dense, unreadable series. */
	export let zoomable = false;
	/** How many points to show initially when zoomable; 0 shows everything. */
	export let initialWindow = 90;

	let container: HTMLDivElement;
	let sliderSvg: SVGSVGElement;
	let width = 0;
	let hoverIndex: number | null = null;

	// Visible window into `points`, as integer indices. Panning and zooming
	// only move this window; the data itself is never touched.
	let viewStart = 0;
	let viewCount = 0;

	// Plot-area drag (pan)
	let panning = false;
	let panStartX = 0;
	let panStartIndex = 0;
	let pointerMoved = false;

	// Slider drag
	type SliderMode = 'left' | 'right' | 'move';
	let sliderMode: SliderMode | null = null;
	let sliderGrabIndex = 0;
	let sliderGrabStart = 0;
	let sliderGrabEnd = 0;
	let sliderHoverX: number | null = null;

	// Unique per instance so multiple charts on one page keep their own fill.
	const uid = Math.random().toString(36).slice(2, 9);
	const padding = { top: 18, right: 12, bottom: 24, left: 40 };
	const PAN_THRESHOLD_PX = 4;
	const SLIDER_GAP = 10;
	const SLIDER_HEIGHT = 32;
	const HANDLE_WIDTH = 8;
	const HANDLE_HIT_PX = 12;

	// A new dataset re-opens on its most recent slice: a full year of days is
	// unreadable at once, and recent history is what people look at first.
	// Written inline (not via a helper) so Svelte can see which values this
	// assigns and order it ahead of everything that reads the window.
	// Everything here is derived into locals first: reading `viewCount` back
	// would make it a dependency of this block, so every pan and handle drag
	// would re-trigger the reset and snap the window straight back.
	$: {
		const total = points.length;
		const initial = zoomable && initialWindow > 0 ? Math.min(total, initialWindow) : total;
		viewCount = initial;
		viewStart = Math.max(total - initial, 0);
		hoverIndex = null;
	}

	// Short series are already readable; a slider would just be furniture.
	$: showSlider = zoomable && points.length > 20;
	$: totalHeight = height + (showSlider ? SLIDER_GAP + SLIDER_HEIGHT : 0);

	$: minViewCount = Math.max(2, Math.min(points.length, 5));
	$: visiblePoints = zoomable ? points.slice(viewStart, viewStart + viewCount) : points;
	$: viewEnd = viewStart + viewCount - 1;

	$: innerWidth = Math.max(width - padding.left - padding.right, 1);
	$: innerHeight = Math.max(height - padding.top - padding.bottom, 1);
	$: maxValue = visiblePoints.length ? Math.max(...visiblePoints.map((p) => p.value)) : 0;
	// A flat-zero series still deserves a sensible axis instead of a divide by zero.
	$: scaleMax = niceCeil(maxValue);
	$: ticks = [0, scaleMax / 2, scaleMax];
	$: coords = visiblePoints.map((point, index) => ({
		x: xAt(index, visiblePoints.length, innerWidth),
		y: padding.top + innerHeight - (point.value / scaleMax) * innerHeight
	}));
	$: linePath = buildSmoothPath(coords);
	$: areaPath = coords.length
		? `${linePath} L ${round(coords[coords.length - 1].x)} ${round(padding.top + innerHeight)} L ${round(coords[0].x)} ${round(padding.top + innerHeight)} Z`
		: '';
	$: xLabels = pickXLabels(visiblePoints, width);
	// Dots would turn into a solid bar on a long daily series.
	$: dotsVisible = showDots && visiblePoints.length <= 60;
	$: active =
		hoverIndex !== null && coords[hoverIndex]
			? { point: visiblePoints[hoverIndex], coord: coords[hoverIndex] }
			: null;
	// Keep the tooltip inside the chart bounds on narrow screens.
	$: tooltipX = active ? Math.min(Math.max(active.coord.x, 58), Math.max(width - 58, 58)) : 0;

	// --- Slider geometry --------------------------------------------------

	$: sliderScaleMax = niceCeil(points.length ? Math.max(...points.map((p) => p.value)) : 0);
	$: sliderCoords = points.map((point, index) => ({
		x: xAt(index, points.length, innerWidth),
		y: SLIDER_HEIGHT - 4 - (point.value / sliderScaleMax) * (SLIDER_HEIGHT - 10)
	}));
	$: sliderLinePath = sliderCoords.length
		? sliderCoords.map((p, i) => `${i === 0 ? 'M' : 'L'} ${round(p.x)} ${round(p.y)}`).join(' ')
		: '';
	$: sliderAreaPath = sliderCoords.length
		? `${sliderLinePath} L ${round(sliderCoords[sliderCoords.length - 1].x)} ${SLIDER_HEIGHT} L ${round(sliderCoords[0].x)} ${SLIDER_HEIGHT} Z`
		: '';
	$: trackLeft = padding.left;
	$: trackRight = padding.left + innerWidth;
	$: selectionLeft = xAt(viewStart, points.length, innerWidth);
	$: selectionRight = xAt(viewEnd, points.length, innerWidth);
	// Keep handles fully inside the track instead of half-hanging off the ends.
	$: handleXs = [selectionLeft, selectionRight].map((x) =>
		clamp(x, trackLeft + HANDLE_WIDTH / 2, trackRight - HANDLE_WIDTH / 2)
	);
	$: sliderCursor = cursorFor(sliderMode, sliderHoverX, selectionLeft, selectionRight, handleXs);

	/**
	 * Cursor tells the reader what the slider will do before they press.
	 * `handles` is a parameter, not an outer read, so this stays reactive.
	 */
	function cursorFor(
		mode: SliderMode | null,
		hoverX: number | null,
		left: number,
		right: number,
		handles: number[]
	): string {
		if (mode) return mode === 'move' ? 'cursor-grabbing' : 'cursor-ew-resize';
		if (hoverX === null) return 'cursor-pointer';
		if (
			Math.abs(hoverX - handles[0]) <= HANDLE_HIT_PX ||
			Math.abs(hoverX - handles[1]) <= HANDLE_HIT_PX
		) {
			return 'cursor-ew-resize';
		}
		return hoverX > left && hoverX < right ? 'cursor-grab' : 'cursor-pointer';
	}

	/**
	 * `plotWidth` is passed in rather than read from the outer scope so that
	 * every reactive statement calling this tracks it as a dependency — without
	 * it, coordinates stay pinned to the zero-width first render.
	 */
	function xAt(index: number, count: number, plotWidth: number): number {
		if (count <= 1) return padding.left + plotWidth / 2;
		return padding.left + (index / (count - 1)) * plotWidth;
	}

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

	function clamp(value: number, min: number, max: number): number {
		return Math.max(min, Math.min(max, value));
	}

	/**
	 * Cubic smoothing with a low tension, which reads as an organic curve
	 * without the overshoot a naive spline produces. Long series fall back to
	 * straight segments: they are both faster and more honest at that density.
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

	// --- Range slider -----------------------------------------------------

	function localX(clientX: number): number {
		const rect = sliderSvg.getBoundingClientRect();
		return clientX - rect.left;
	}

	function indexAtX(x: number): number {
		if (points.length <= 1) return 0;
		const ratio = clamp((x - padding.left) / innerWidth, 0, 1);
		return Math.round(ratio * (points.length - 1));
	}

	function setWindow(start: number, end: number) {
		const last = points.length - 1;
		const from = clamp(Math.round(start), 0, Math.max(points.length - minViewCount, 0));
		const to = clamp(Math.round(end), Math.min(from + minViewCount - 1, last), last);
		viewStart = from;
		viewCount = to - from + 1;
		hoverIndex = null;
	}

	function sliderPointerDown(event: PointerEvent) {
		if (!showSlider) return;
		const x = localX(event.clientX);
		const index = indexAtX(x);

		// Hit-test against where the handles are actually drawn (they are inset
		// at the track ends), not the raw selection edges.
		if (Math.abs(x - handleXs[0]) <= HANDLE_HIT_PX) {
			sliderMode = 'left';
		} else if (Math.abs(x - handleXs[1]) <= HANDLE_HIT_PX) {
			sliderMode = 'right';
		} else if (x > selectionLeft && x < selectionRight) {
			sliderMode = 'move';
		} else {
			// Clicking bare track re-centres the current window there, then the
			// same gesture keeps dragging it.
			const half = Math.floor((viewCount - 1) / 2);
			viewStart = clamp(index - half, 0, Math.max(points.length - viewCount, 0));
			sliderMode = 'move';
		}

		sliderGrabIndex = index;
		sliderGrabStart = viewStart;
		sliderGrabEnd = viewStart + viewCount - 1;
		sliderSvg.setPointerCapture?.(event.pointerId);
		event.preventDefault();
	}

	function sliderPointerMove(event: PointerEvent) {
		const x = localX(event.clientX);
		if (!sliderMode) {
			sliderHoverX = x;
			return;
		}
		const index = indexAtX(x);

		if (sliderMode === 'left') {
			setWindow(Math.min(index, sliderGrabEnd - minViewCount + 1), sliderGrabEnd);
		} else if (sliderMode === 'right') {
			setWindow(sliderGrabStart, Math.max(index, sliderGrabStart + minViewCount - 1));
		} else {
			const shift = index - sliderGrabIndex;
			const span = sliderGrabEnd - sliderGrabStart;
			const start = clamp(sliderGrabStart + shift, 0, points.length - 1 - span);
			setWindow(start, start + span);
		}
	}

	function sliderPointerUp(event: PointerEvent) {
		if (!sliderMode) return;
		sliderSvg.releasePointerCapture?.(event.pointerId);
		sliderMode = null;
	}

	// --- Plot area --------------------------------------------------------

	function clampStart(value: number): number {
		return clamp(Math.round(value), 0, Math.max(points.length - viewCount, 0));
	}

	function handlePointerDown(event: PointerEvent) {
		if (zoomable && viewCount < points.length) {
			panning = true;
			pointerMoved = false;
			panStartX = event.clientX;
			panStartIndex = viewStart;
			(event.currentTarget as Element).setPointerCapture?.(event.pointerId);
		}
		updateHover(event);
	}

	function handlePointerMove(event: PointerEvent) {
		if (panning) {
			const dx = event.clientX - panStartX;
			if (!pointerMoved && Math.abs(dx) < PAN_THRESHOLD_PX) return;
			pointerMoved = true;
			hoverIndex = null;
			const stepPx = innerWidth / Math.max(viewCount - 1, 1);
			viewStart = clampStart(panStartIndex - dx / stepPx);
			return;
		}
		updateHover(event);
	}

	function endPan(event: PointerEvent) {
		if (!panning) return;
		(event.currentTarget as Element).releasePointerCapture?.(event.pointerId);
		panning = false;
		// A drag should not leave a tooltip behind; a tap should keep it.
		if (pointerMoved) hoverIndex = null;
	}

	function updateHover(event: PointerEvent) {
		if (!visiblePoints.length) return;
		const rect = (event.currentTarget as SVGRectElement).getBoundingClientRect();
		if (rect.width <= 0) return;
		const ratio = clamp((event.clientX - rect.left) / rect.width, 0, 1);
		hoverIndex = Math.round(ratio * (visiblePoints.length - 1));
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

<div
	class="relative w-full select-none touch-pan-y"
	bind:this={container}
	style="height: {totalHeight}px"
>
	{#if width > 0 && visiblePoints.length}
		<svg {width} {height} role="presentation" aria-hidden="true" class="block">
			<defs>
				<linearGradient id="area-{uid}" x1="0" y1="0" x2="0" y2="1">
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

			<path d={areaPath} fill="url(#area-{uid})" />
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
				class={zoomable && viewCount < points.length
					? panning
						? 'cursor-grabbing'
						: 'cursor-grab'
					: ''}
				on:pointerdown={handlePointerDown}
				on:pointermove={handlePointerMove}
				on:pointerup={endPan}
				on:pointercancel={endPan}
				on:pointerleave={clearHover}
			/>
		</svg>

		<!-- Range slider: a miniature of the whole series with a draggable window -->
		{#if showSlider}
			<svg
				bind:this={sliderSvg}
				{width}
				height={SLIDER_HEIGHT}
				role="presentation"
				aria-hidden="true"
				class="block touch-none {sliderCursor}"
				style="margin-top: {SLIDER_GAP}px"
				on:pointerdown={sliderPointerDown}
				on:pointermove={sliderPointerMove}
				on:pointerup={sliderPointerUp}
				on:pointercancel={sliderPointerUp}
				on:pointerleave={() => (sliderHoverX = null)}
			>
				<defs>
					<clipPath id="track-{uid}">
						<rect x={trackLeft} y="0" width={innerWidth} height={SLIDER_HEIGHT} rx="6" />
					</clipPath>
					<clipPath id="window-{uid}">
						<rect
							x={selectionLeft}
							y="0"
							width={Math.max(selectionRight - selectionLeft, 1)}
							height={SLIDER_HEIGHT}
						/>
					</clipPath>
				</defs>

				<!-- Track: the whole series, greyed out -->
				<g clip-path="url(#track-{uid})">
					<rect x={trackLeft} y="0" width={innerWidth} height={SLIDER_HEIGHT} fill="hsl(var(--muted))" />
					<path d={sliderAreaPath} fill="hsl(var(--muted-foreground))" opacity="0.3" />
					<path
						d={sliderLinePath}
						fill="none"
						stroke="hsl(var(--muted-foreground))"
						stroke-width="1"
						opacity="0.55"
					/>

					<!-- Selected window: same preview, but in the accent colour -->
					<g clip-path="url(#window-{uid})">
						<rect
							x={trackLeft}
							y="0"
							width={innerWidth}
							height={SLIDER_HEIGHT}
							fill="hsl(var(--primary))"
							opacity="0.16"
						/>
						<path d={sliderAreaPath} fill="hsl(var(--primary))" opacity="0.45" />
						<path d={sliderLinePath} fill="none" stroke="hsl(var(--primary))" stroke-width="1.5" />
					</g>
				</g>

				<rect
					x={trackLeft}
					y="0.5"
					width={innerWidth}
					height={SLIDER_HEIGHT - 1}
					rx="6"
					fill="none"
					stroke="hsl(var(--border))"
					stroke-width="1"
				/>

				<!-- Handles -->
				{#each handleXs as handleX}
					<rect
						x={handleX - HANDLE_WIDTH / 2}
						y="0"
						width={HANDLE_WIDTH}
						height={SLIDER_HEIGHT}
						rx="4"
						fill="hsl(var(--primary))"
						stroke="hsl(var(--card))"
						stroke-width="1"
					/>
					{#each [-2, 2] as offset}
						<line
							x1={handleX + offset * 0.9}
							y1={SLIDER_HEIGHT / 2 - 4}
							x2={handleX + offset * 0.9}
							y2={SLIDER_HEIGHT / 2 + 4}
							stroke="hsl(var(--primary-foreground))"
							stroke-width="1"
							opacity="0.8"
						/>
					{/each}
				{/each}
			</svg>
		{/if}

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
