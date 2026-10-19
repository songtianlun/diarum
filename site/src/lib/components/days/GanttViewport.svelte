<script lang="ts">
	import { tick, type Snippet } from 'svelte';
	import { t } from '$lib/i18n';

	/**
	 * The scrolling frame both Gantt charts sit in. A range picker narrows
	 * the visible window (a month, a year, …) by stretching the chart wider
	 * than the frame; the frame then scrolls sideways, and a mouse can drag
	 * it to pan. The chart marks its plotting area with `data-gantt-track`
	 * and sizes its label column with the `--gantt-label` variable (set on
	 * this frame through `labelClass`), so the stretch leaves labels alone.
	 */
	let {
		total,
		focus,
		storageKey,
		minWidth,
		gutter,
		labelClass,
		span = $bindable(total),
		children
	}: {
		/** Days the whole chart covers. */
		total: number;
		/** Where today sits along the chart, 0–100; the window opens centred on it. */
		focus: number;
		storageKey: string;
		/** Narrowest the chart may get before the frame scrolls anyway. */
		minWidth: number;
		/** Space in px kept to the right of the plotting area. */
		gutter: number;
		labelClass: string;
		/** Days visible in the window, for spacing the axis ticks. */
		span?: number;
		children: Snippet;
	} = $props();

	const RANGES: { days: number; unit: 'month' | 'year'; n: number }[] = [
		{ days: 31, unit: 'month', n: 1 },
		{ days: 92, unit: 'month', n: 3 },
		{ days: 183, unit: 'month', n: 6 },
		{ days: 365, unit: 'year', n: 1 },
		{ days: 365 * 3, unit: 'year', n: 3 }
	];

	function stored(): number {
		try {
			return Number(localStorage.getItem(storageKey)) || 0;
		} catch {
			return 0;
		}
	}

	/** Chosen window in days; 0 shows everything. */
	let range = $state(stored());
	// Only ranges noticeably narrower than the whole chart are worth offering.
	let options = $derived(RANGES.filter((r) => r.days < total * 0.8));
	let zoom = $derived(range > 0 && range < total * 0.8 ? total / range : 1);

	$effect(() => {
		span = zoom > 1 ? range : total;
	});

	function choose(days: number) {
		range = days;
		try {
			localStorage.setItem(storageKey, String(days));
		} catch {
			// per-device preference only
		}
	}

	let scroller: HTMLDivElement | undefined = $state();

	function centre(at: number) {
		const track = scroller?.querySelector<HTMLElement>('[data-gantt-track]');
		if (!scroller || !track) return;
		const frame = scroller.getBoundingClientRect();
		const rect = track.getBoundingClientRect();
		// The track's start in content coordinates; the sticky labels cover that much of the frame.
		const trackX = scroller.scrollLeft + rect.left - frame.left;
		scroller.scrollLeft = trackX + (rect.width * at) / 100 - (trackX + scroller.clientWidth) / 2;
	}

	// Open each new window on today.
	$effect(() => {
		void zoom;
		tick().then(() => centre(focus));
	});

	// Dragging with a mouse pans; touch already scrolls on its own.
	let drag = $state<{ x: number; left: number; moved: boolean } | null>(null);
	let swallowClick = false;

	function pointerdown(e: PointerEvent) {
		if (zoom <= 1 || e.pointerType !== 'mouse' || e.button !== 0 || !scroller) return;
		if ((e.target as Element).closest('[data-gantt-marker], button')) return;
		drag = { x: e.clientX, left: scroller.scrollLeft, moved: false };
	}

	function pointermove(e: PointerEvent) {
		if (!drag || !scroller) return;
		const dx = e.clientX - drag.x;
		if (Math.abs(dx) > 4) drag.moved = true;
		if (drag.moved) scroller.scrollLeft = drag.left - dx;
	}

	function pointerup() {
		if (drag?.moved) swallowClick = true;
		drag = null;
	}

	// The release after a drag would otherwise open the row under the cursor.
	function click(e: MouseEvent) {
		if (!swallowClick) return;
		swallowClick = false;
		e.preventDefault();
		e.stopPropagation();
	}
</script>

<svelte:window onpointermove={pointermove} onpointerup={pointerup} onpointercancel={() => (drag = null)} />

{#if options.length > 0}
	<div class="mb-3 flex flex-wrap items-center justify-end gap-2">
		{#if zoom > 1}
			<button
				type="button"
				onclick={() => centre(focus)}
				class="px-2.5 py-1 rounded-lg text-xs text-amber-700 dark:text-amber-300 hover:bg-amber-500/10 transition-colors"
			>{$t('days.gantt.range.today')}</button>
		{/if}
		<div class="inline-flex p-0.5 rounded-lg bg-muted/60 text-xs" role="group" aria-label={$t('days.gantt.range.label')}>
			{#each options as option (option.days)}
				<button
					type="button"
					onclick={() => choose(option.days)}
					aria-pressed={zoom > 1 && range === option.days}
					class="px-2.5 py-1 rounded-md tabular-nums transition-colors {zoom > 1 && range === option.days
						? 'bg-card text-foreground shadow-sm'
						: 'text-muted-foreground hover:text-foreground'}"
				>{$t(`days.gantt.range.${option.unit}`, { n: option.n })}</button>
			{/each}
			<button
				type="button"
				onclick={() => choose(0)}
				aria-pressed={zoom === 1}
				class="px-2.5 py-1 rounded-md transition-colors {zoom === 1 ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}"
			>{$t('days.gantt.range.all')}</button>
		</div>
	</div>
{/if}

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	bind:this={scroller}
	class="overflow-x-auto overscroll-x-contain {labelClass} {zoom > 1 ? 'select-none' : ''} {drag?.moved ? 'cursor-grabbing' : zoom > 1 ? 'cursor-grab' : ''}"
	onpointerdown={pointerdown}
	onclickcapture={click}
	ondragstart={(e) => zoom > 1 && e.preventDefault()}
>
	<div
		style="min-width: {minWidth}px; {zoom > 1
			? `width: calc(var(--gantt-label) + ${gutter}px + (max(100%, ${minWidth}px) - var(--gantt-label) - ${gutter}px) * ${zoom})`
			: ''}"
	>
		{@render children()}
	</div>
</div>
