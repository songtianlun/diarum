<script lang="ts" module>
	/** One chart row drawn small in the slider's overview, in days from the domain start. */
	export interface OverviewRow {
		from: number;
		to: number;
		tone: 'up' | 'down' | 'muted';
		marks?: number[];
	}
</script>

<script lang="ts">
	import { untrack, type Snippet } from 'svelte';
	import { addDays } from '$lib/utils/days';
	import { t, locale } from '$lib/i18n';

	/**
	 * The window both Gantt charts look through. A slider under the chart
	 * shows every row in miniature with the visible stretch highlighted:
	 * drag the stretch to pan, drag either handle to widen or narrow it, tap
	 * bare track to jump there. On the chart itself, dragging pans, a pinch
	 * or ctrl-scroll zooms around the fingers or cursor, and a sideways
	 * scroll pans. Presets (1M … All) open a window centred on today.
	 *
	 * The chart marks its plotting area with `data-gantt-track` and sizes its
	 * label column with `--gantt-label` (set through `labelClass`), so the
	 * slider lines up under the plot. `from` and `to` are the visible window
	 * in days from the domain start.
	 */
	let {
		start,
		total,
		today,
		overview,
		storageKey,
		gutter,
		labelClass,
		from = $bindable(0),
		to = $bindable(total),
		children
	}: {
		/** First date of the domain. */
		start: string;
		/** Days the domain covers. */
		total: number;
		/** Today, in days from the domain start. */
		today: number;
		overview: OverviewRow[];
		storageKey: string;
		/** Space in px kept to the right of the plotting area. */
		gutter: number;
		labelClass: string;
		from?: number;
		to?: number;
		children: Snippet;
	} = $props();

	let intl = $derived($locale === 'zh' ? 'zh-CN' : 'en-US');

	const RANGES: { days: number; unit: 'month' | 'year'; n: number }[] = [
		{ days: 31, unit: 'month', n: 1 },
		{ days: 92, unit: 'month', n: 3 },
		{ days: 183, unit: 'month', n: 6 },
		{ days: 365, unit: 'year', n: 1 },
		{ days: 365 * 3, unit: 'year', n: 3 }
	];

	let minSpan = $derived(Math.min(total, 7));
	let span = $derived(to - from);
	let zoomed = $derived(span < total - 0.5);
	let options = $derived(RANGES.filter((r) => r.days < total * 0.8));

	function clamp(n: number, lo: number, hi: number) {
		return Math.min(hi, Math.max(lo, n));
	}

	function setWindow(a: number, b: number) {
		const width = clamp(b - a, minSpan, total);
		const left = clamp(a, 0, total - width);
		from = left;
		to = left + width;
	}

	function centreOn(day: number, width: number) {
		setWindow(day - width / 2, day + width / 2);
	}

	function zoomAt(anchor: number, factor: number) {
		const width = clamp(span * factor, minSpan, total);
		const left = anchor - ((anchor - from) * width) / span;
		setWindow(left, left + width);
	}

	function remember() {
		try {
			localStorage.setItem(storageKey, String(zoomed ? Math.round(span) : 0));
		} catch {
			// per-device preference only
		}
	}

	function choose(days: number) {
		if (days > 0) centreOn(today, days);
		else setWindow(0, total);
		touched = true;
		remember();
	}

	// Until someone moves the window, it follows the data: the remembered
	// width centred on today, or everything. Afterwards it only stays in bounds.
	let touched = false;
	$effect(() => {
		const domain = total;
		untrack(() => {
			if (touched) {
				setWindow(from, to);
				return;
			}
			let stored = 0;
			try {
				stored = Number(localStorage.getItem(storageKey)) || 0;
			} catch {
				// no stored preference
			}
			if (stored > 0 && stored < domain * 0.8) centreOn(today, stored);
			else setWindow(0, domain);
		});
	});

	function label(day: number): string {
		return new Date(`${addDays(start, Math.round(day))}T00:00:00`).toLocaleDateString(intl, { year: 'numeric', month: 'short', day: 'numeric' });
	}

	// --- Slider ---------------------------------------------------------------

	let track: HTMLDivElement | undefined = $state();
	let slide = $state<{ mode: 'left' | 'right' | 'move'; x: number; from: number; to: number } | null>(null);

	function dayAt(clientX: number): number {
		if (!track) return 0;
		const rect = track.getBoundingClientRect();
		return ((clientX - rect.left) / rect.width) * total;
	}

	function slideDown(e: PointerEvent) {
		if (e.button !== 0 || !track) return;
		const part = (e.target as HTMLElement).closest<HTMLElement>('[data-part]')?.dataset.part;
		let mode: 'left' | 'right' | 'move' = 'move';
		if (part === 'left' || part === 'right') mode = part;
		else if (part !== 'window') centreOn(dayAt(e.clientX), span);
		slide = { mode, x: e.clientX, from, to };
		touched = true;
		track.setPointerCapture(e.pointerId);
		e.preventDefault();
	}

	function slideMove(e: PointerEvent) {
		if (!slide || !track) return;
		const delta = ((e.clientX - slide.x) / track.getBoundingClientRect().width) * total;
		if (slide.mode === 'move') setWindow(slide.from + delta, slide.to + delta);
		else if (slide.mode === 'left') {
			const a = clamp(slide.from + delta, 0, slide.to - minSpan);
			setWindow(a, slide.to);
		} else {
			const b = clamp(slide.to + delta, slide.from + minSpan, total);
			setWindow(slide.from, b);
		}
	}

	function slideUp() {
		if (!slide) return;
		slide = null;
		remember();
	}

	function slideKey(e: KeyboardEvent, part: 'left' | 'right' | 'move') {
		const step = (e.shiftKey ? 30 : 1) * (e.key === 'ArrowLeft' ? -1 : e.key === 'ArrowRight' ? 1 : 0);
		if (!step) return;
		e.preventDefault();
		touched = true;
		if (part === 'move') setWindow(from + step, to + step);
		else if (part === 'left') setWindow(clamp(from + step, 0, to - minSpan), to);
		else setWindow(from, clamp(to + step, from + minSpan, total));
		remember();
	}

	// --- Chart gestures -------------------------------------------------------

	let chart: HTMLDivElement | undefined = $state();
	// Pointers down on the chart, by id, with where each one is now.
	const pointers = new Map<number, number>();
	let pan = $state<{ x: number; from: number; moved: boolean } | null>(null);
	let pinch: { gap: number; anchor: number; span: number; from: number } | null = null;
	let swallowClick = false;

	function plot(): DOMRect | null {
		return chart?.querySelector<HTMLElement>('[data-gantt-track]')?.getBoundingClientRect() ?? null;
	}

	function dayInPlot(clientX: number, rect: DOMRect): number {
		return from + ((clientX - rect.left) / rect.width) * span;
	}

	function chartDown(e: PointerEvent) {
		if (e.pointerType === 'mouse' && e.button !== 0) return;
		pointers.set(e.pointerId, e.clientX);
		if (pointers.size === 2) {
			const rect = plot();
			const [a, b] = [...pointers.values()];
			if (rect) pinch = { gap: Math.max(Math.abs(a - b), 1), anchor: dayInPlot((a + b) / 2, rect), span, from };
			pan = null;
			return;
		}
		if (pointers.size > 1 || !zoomed) return;
		if (e.pointerType === 'mouse' && (e.target as Element).closest('[data-gantt-marker], button')) return;
		pan = { x: e.clientX, from, moved: false };
	}

	function chartMove(e: PointerEvent) {
		if (!pointers.has(e.pointerId)) return;
		pointers.set(e.pointerId, e.clientX);
		const rect = plot();
		if (!rect) return;
		if (pinch && pointers.size === 2) {
			const [a, b] = [...pointers.values()];
			const width = clamp((pinch.span * pinch.gap) / Math.max(Math.abs(a - b), 1), minSpan, total);
			// Keep the day that was under the fingers' midpoint under it.
			const ratio = ((a + b) / 2 - rect.left) / rect.width;
			setWindow(pinch.anchor - ratio * width, pinch.anchor - ratio * width + width);
			touched = true;
			swallowClick = true;
			return;
		}
		if (!pan) return;
		const dx = e.clientX - pan.x;
		if (!pan.moved) {
			if (Math.abs(dx) < 6) return;
			pan.moved = true;
			chart?.setPointerCapture(e.pointerId);
		}
		touched = true;
		const shift = (-dx / rect.width) * span;
		setWindow(pan.from + shift, pan.from + shift + span);
	}

	function chartUp(e: PointerEvent) {
		if (!pointers.delete(e.pointerId)) return;
		if (pan?.moved || pinch) {
			swallowClick = true;
			remember();
		}
		if (pointers.size < 2) pinch = null;
		pan = null;
	}

	// The release after a drag would otherwise open the row under it.
	function chartClick(e: MouseEvent) {
		if (!swallowClick) return;
		swallowClick = false;
		e.preventDefault();
		e.stopPropagation();
	}

	// Wheel needs a non-passive listener to keep the page from scrolling.
	$effect(() => {
		const el = chart;
		if (!el) return;
		const wheel = (e: WheelEvent) => {
			const rect = plot();
			if (!rect) return;
			if (e.ctrlKey || e.metaKey) {
				e.preventDefault();
				zoomAt(dayInPlot(e.clientX, rect), Math.exp(e.deltaY * 0.01));
			} else if (Math.abs(e.deltaX) > Math.abs(e.deltaY) && zoomed) {
				e.preventDefault();
				const shift = (e.deltaX / rect.width) * span;
				setWindow(from + shift, to + shift);
			} else return;
			touched = true;
			remember();
		};
		el.addEventListener('wheel', wheel, { passive: false });
		return () => el.removeEventListener('wheel', wheel);
	});

	function pct(day: number) {
		return (day / total) * 100;
	}

	let rowHeight = $derived(Math.max(1.5, Math.min(4, 26 / Math.max(overview.length, 1))));
</script>

<svelte:window onpointerup={slideUp} onpointercancel={slideUp} />

{#if options.length > 0}
	<div class="mb-3 flex justify-end">
		<div class="inline-flex p-0.5 rounded-lg bg-muted/60 text-xs" role="group" aria-label={$t('days.gantt.range.label')}>
			{#each options as option (option.days)}
				<button
					type="button"
					onclick={() => choose(option.days)}
					aria-pressed={zoomed && Math.abs(span - option.days) < 1}
					class="px-2.5 py-1 rounded-md tabular-nums transition-colors {zoomed && Math.abs(span - option.days) < 1
						? 'bg-card text-foreground shadow-sm'
						: 'text-muted-foreground hover:text-foreground'}"
				>{$t(`days.gantt.range.${option.unit}`, { n: option.n })}</button>
			{/each}
			<button
				type="button"
				onclick={() => choose(0)}
				aria-pressed={!zoomed}
				class="px-2.5 py-1 rounded-md transition-colors {!zoomed ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
			>{$t('days.gantt.range.all')}</button>
		</div>
	</div>
{/if}

<div class={labelClass}>
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		bind:this={chart}
		class="touch-pan-y {zoomed ? 'select-none' : ''} {pan?.moved ? 'cursor-grabbing' : zoomed ? 'cursor-grab' : ''}"
		onpointerdown={chartDown}
		onpointermove={chartMove}
		onpointerup={chartUp}
		onpointercancel={chartUp}
		onclickcapture={chartClick}
		ondragstart={(e) => e.preventDefault()}
	>
		{@render children()}
	</div>

	{#if total > minSpan}
		<!-- Range slider: every row in miniature, with the visible window on top -->
		<div class="flex items-center mt-3">
			<div class="w-[var(--gantt-label)] flex-shrink-0 pr-2 text-[10px] leading-tight text-muted-foreground tabular-nums">
				<div class="truncate">{label(from)}</div>
				<div class="truncate">– {label(to)}</div>
			</div>
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				bind:this={track}
				class="relative flex-1 h-9 rounded-lg bg-muted/50 border border-border/60 touch-none select-none {slide ? 'cursor-grabbing' : 'cursor-pointer'}"
				style="margin-right: {gutter}px"
				onpointerdown={slideDown}
				onpointermove={slideMove}
			>
				<div class="absolute inset-x-0 inset-y-1 overflow-hidden rounded-md pointer-events-none" aria-hidden="true">
					{#each overview as row, i (i)}
						{@const top = (i + 0.5) * (100 / overview.length)}
						<span
							class="absolute -translate-y-1/2 rounded-full {row.tone === 'up' ? 'bg-primary/45' : row.tone === 'down' ? 'bg-amber-500/45' : 'bg-muted-foreground/30'}"
							style="left: {pct(row.from)}%; width: max({pct(row.to - row.from)}%, 2px); top: {top}%; height: {rowHeight}px"
						></span>
						{#each row.marks ?? [] as mark}
							<span class="absolute w-[3px] -translate-x-1/2 -translate-y-1/2 rounded-full bg-amber-600/70" style="left: {pct(mark)}%; top: {top}%; height: {rowHeight + 2}px"></span>
						{/each}
					{/each}
					<span class="absolute inset-y-0 border-l border-dashed border-amber-500/80" style="left: {pct(today)}%"></span>
				</div>

				<!-- Outside the window, dimmed -->
				<span class="absolute inset-y-0 left-0 rounded-l-lg bg-card/55 pointer-events-none" style="width: {pct(from)}%" aria-hidden="true"></span>
				<span class="absolute inset-y-0 right-0 rounded-r-lg bg-card/55 pointer-events-none" style="width: {100 - pct(to)}%" aria-hidden="true"></span>

				<!-- The window -->
				<div
					data-part="window"
					role="slider"
					tabindex="0"
					aria-label={$t('days.gantt.range.label')}
					aria-valuemin={0}
					aria-valuemax={total}
					aria-valuenow={Math.round(from)}
					aria-valuetext="{label(from)} – {label(to)}"
					class="absolute -inset-y-px bg-primary/15 border-y-2 border-primary/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 {slide ? 'cursor-grabbing' : 'cursor-grab'}"
					style="left: {pct(from)}%; width: {pct(span)}%"
					onkeydown={(e) => slideKey(e, 'move')}
				></div>

				<!-- Handles reach outward, so even a narrow window keeps its middle to drag -->
				{#each ['left', 'right'] as const as side (side)}
					{@const at = side === 'left' ? from : to}
					<div
						data-part={side}
						role="slider"
						tabindex="0"
						aria-label={side === 'left' ? $t('days.gantt.range.start') : $t('days.gantt.range.end')}
						aria-valuemin={0}
						aria-valuemax={total}
						aria-valuenow={Math.round(at)}
						aria-valuetext={label(at)}
						class="group absolute -inset-y-1 w-6 flex items-center cursor-ew-resize z-10 focus-visible:outline-none {side === 'left' ? 'justify-end' : 'justify-start'}"
						style="left: calc({pct(at)}% - {side === 'left' ? 22 : 2}px)"
						onkeydown={(e) => slideKey(e, side)}
					>
						<span class="w-2.5 h-7 rounded-md bg-primary shadow-sm ring-2 ring-card flex items-center justify-center gap-px transition-transform group-hover:scale-x-125 group-focus-visible:ring-ring/60 {slide?.mode === side ? 'scale-x-125' : ''}">
							<span class="w-px h-3 bg-primary-foreground/80"></span>
							<span class="w-px h-3 bg-primary-foreground/80"></span>
						</span>
					</div>
				{/each}
			</div>
		</div>
	{/if}
</div>
