<script lang="ts">
	import { onMount, onDestroy, tick } from 'svelte';
	import type { LightboxItem } from '$lib/stores/lightbox';
	import { effectiveLiveMode, modeLabel, readLiveMode } from '$lib/utils/livePlayer';
	import { t } from '$lib/i18n';

	export let items: LightboxItem[] = [];
	export let index = 0;
	export let onClose: () => void;
	/** Set when an info panel is passed in the `info` slot. */
	export let hasInfo = false;

	const MIN_SCALE = 1;
	const MAX_SCALE = 6;
	const DOUBLE_TAP_SCALE = 2.5;
	const SWIPE_DISTANCE = 60;
	const DISMISS_DISTANCE = 110;

	let stage: HTMLDivElement;
	let imgEl: HTMLImageElement;
	let closeButton: HTMLButtonElement;
	let previousFocus: Element | null = null;

	let scale = 1;
	let tx = 0;
	let ty = 0;
	let animating = true;
	let loaded = false;
	let failed = false;
	let showInfo = true;

	// Gesture state
	const pointers = new Map<number, { x: number; y: number }>();
	let dragStart: { x: number; y: number; tx: number; ty: number } | null = null;
	let pinchStart: { dist: number; scale: number; cx: number; cy: number; tx: number; ty: number } | null = null;
	let swipe = { dx: 0, dy: 0 };
	let moved = false;
	// Pointer capture retargets later events to the stage, so remember where
	// the gesture started.
	let startedOnBackground = false;
	let lastTap = { time: 0, x: 0, y: 0 };

	$: item = items[index];
	$: hasPrev = index > 0;
	$: hasNext = index < items.length - 1;
	$: zoomed = scale > 1.01;
	$: backdropOpacity = Math.max(0.35, 1 - Math.abs(swipe.dy) / 400);

	let currentSrc = '';
	$: if (item && item.src !== currentSrc) {
		currentSrc = item.src;
		resetView(false);
		loaded = false;
		failed = false;
		livePlaying = false;
		liveFailed = false;
		preloadNeighbours();
	}

	// Live photos: the clip plays over the image, in the photo's mode.
	let liveEl: HTMLVideoElement | null = null;
	let liveBox = { left: 0, top: 0, width: 0, height: 0 };
	let livePlaying = false;
	let liveFailed = false;
	$: liveMode = item?.live ? effectiveLiveMode(readLiveMode(item.liveMode), item.live) : 'off';
	$: showLive = !!item?.live && loaded && !liveFailed;

	/** The clip covers exactly the box the image is laid out in. */
	function measureLive() {
		if (!imgEl) return;
		liveBox = { left: imgEl.offsetLeft, top: imgEl.offsetTop, width: imgEl.offsetWidth, height: imgEl.offsetHeight };
	}

	function liveVideo(node: HTMLVideoElement) {
		liveEl = node;
		measureLive();
		node.muted = true;
		if (liveMode !== 'off') {
			node.loop = liveMode === 'loop';
			node.play().catch(() => {});
		}
		const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measureLive);
		if (observer && stage) observer.observe(stage);
		return {
			destroy() {
				observer?.disconnect();
				node.pause();
				node.removeAttribute('src');
				node.load();
				if (liveEl === node) liveEl = null;
			}
		};
	}

	function playLive() {
		if (!liveEl) return;
		liveEl.loop = false;
		liveEl.currentTime = 0;
		liveEl.play().catch(() => {});
	}

	function preloadNeighbours() {
		for (const offset of [1, -1]) {
			const neighbour = items[index + offset];
			if (neighbour) new Image().src = neighbour.src;
		}
	}

	function resetView(animate = true) {
		animating = animate;
		scale = 1;
		tx = 0;
		ty = 0;
		swipe = { dx: 0, dy: 0 };
	}

	function go(delta: number) {
		const next = index + delta;
		if (next < 0 || next >= items.length) {
			// Rubber-band back instead of doing nothing.
			animating = true;
			swipe = { dx: 0, dy: 0 };
			return;
		}
		index = next;
	}

	/** Keeps the image covering the stage instead of drifting off-screen. */
	function clampPan(nextScale: number, x: number, y: number) {
		if (!imgEl || !stage) return { x, y };
		const maxX = Math.max(0, (imgEl.offsetWidth * nextScale - stage.clientWidth) / 2);
		const maxY = Math.max(0, (imgEl.offsetHeight * nextScale - stage.clientHeight) / 2);
		return { x: Math.min(maxX, Math.max(-maxX, x)), y: Math.min(maxY, Math.max(-maxY, y)) };
	}

	/** Zoom so the point under (clientX, clientY) stays put. */
	function zoomTo(nextScale: number, clientX?: number, clientY?: number, base = { scale, tx, ty }) {
		nextScale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, nextScale));
		const rect = stage.getBoundingClientRect();
		const px = (clientX ?? rect.left + rect.width / 2) - (rect.left + rect.width / 2);
		const py = (clientY ?? rect.top + rect.height / 2) - (rect.top + rect.height / 2);
		const ratio = nextScale / base.scale;
		const pan = clampPan(nextScale, px - (px - base.tx) * ratio, py - (py - base.ty) * ratio);
		scale = nextScale;
		tx = nextScale === 1 ? 0 : pan.x;
		ty = nextScale === 1 ? 0 : pan.y;
	}

	function zoomBy(factor: number) {
		animating = true;
		zoomTo(scale * factor);
	}

	function handleWheel(event: WheelEvent) {
		event.preventDefault();
		animating = false;
		// Trackpad pinches arrive as ctrl+wheel with small deltas.
		const sensitivity = event.ctrlKey ? 0.01 : 0.0022;
		zoomTo(scale * Math.exp(-event.deltaY * sensitivity), event.clientX, event.clientY);
	}

	function distance(a: { x: number; y: number }, b: { x: number; y: number }) {
		return Math.hypot(a.x - b.x, a.y - b.y);
	}

	function handlePointerDown(event: PointerEvent) {
		if (event.button !== 0 && event.pointerType === 'mouse') return;
		startedOnBackground = event.target === stage;
		stage.setPointerCapture(event.pointerId);
		pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
		animating = false;
		moved = false;
		if (pointers.size === 2) {
			const [a, b] = [...pointers.values()];
			pinchStart = { dist: distance(a, b), scale, cx: (a.x + b.x) / 2, cy: (a.y + b.y) / 2, tx, ty };
			dragStart = null;
			swipe = { dx: 0, dy: 0 };
		} else if (pointers.size === 1) {
			dragStart = { x: event.clientX, y: event.clientY, tx, ty };
		}
	}

	function handlePointerMove(event: PointerEvent) {
		if (!pointers.has(event.pointerId)) return;
		pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });

		if (pinchStart && pointers.size >= 2) {
			const [a, b] = [...pointers.values()];
			moved = true;
			zoomTo(pinchStart.scale * (distance(a, b) / pinchStart.dist), pinchStart.cx, pinchStart.cy, {
				scale: pinchStart.scale,
				tx: pinchStart.tx,
				ty: pinchStart.ty
			});
			return;
		}
		if (!dragStart) return;
		const dx = event.clientX - dragStart.x;
		const dy = event.clientY - dragStart.y;
		if (Math.hypot(dx, dy) > 6) moved = true;
		if (zoomed) {
			const pan = clampPan(scale, dragStart.tx + dx, dragStart.ty + dy);
			tx = pan.x;
			ty = pan.y;
		} else if (moved) {
			// Lock to the dominant axis: horizontal navigates, vertical dismisses.
			swipe = Math.abs(dx) > Math.abs(dy) ? { dx, dy: 0 } : { dx: 0, dy };
		}
	}

	function handlePointerUp(event: PointerEvent) {
		if (!pointers.has(event.pointerId)) return;
		pointers.delete(event.pointerId);
		if (pointers.size > 0) {
			// Lifting one finger of a pinch continues as a drag with the other.
			pinchStart = null;
			const [rest] = [...pointers.values()];
			dragStart = { x: rest.x, y: rest.y, tx, ty };
			return;
		}
		pinchStart = null;
		dragStart = null;
		animating = true;

		if (!moved && event.type === 'pointerup') {
			handleTap(event);
			return;
		}
		if (!zoomed) {
			if (swipe.dx <= -SWIPE_DISTANCE) go(1);
			else if (swipe.dx >= SWIPE_DISTANCE) go(-1);
			else if (Math.abs(swipe.dy) >= DISMISS_DISTANCE) {
				onClose();
				return;
			}
			swipe = { dx: 0, dy: 0 };
		}
	}

	function handleTap(event: PointerEvent) {
		const now = Date.now();
		const isDoubleTap = now - lastTap.time < 300 && Math.hypot(event.clientX - lastTap.x, event.clientY - lastTap.y) < 30;
		lastTap = { time: isDoubleTap ? 0 : now, x: event.clientX, y: event.clientY };
		if (isDoubleTap) {
			zoomTo(zoomed ? 1 : DOUBLE_TAP_SCALE, event.clientX, event.clientY);
			return;
		}
		// A single tap on the dark area around the image closes, like most viewers.
		if (startedOnBackground && !zoomed) {
			setTimeout(() => {
				if (lastTap.time === now) onClose();
			}, 300);
		}
	}

	function handleKeydown(event: KeyboardEvent) {
		switch (event.key) {
			case 'Escape':
				event.preventDefault();
				if (zoomed) resetView();
				else onClose();
				break;
			case 'ArrowLeft':
				if (!zoomed) go(-1);
				break;
			case 'ArrowRight':
				if (!zoomed) go(1);
				break;
			case '+':
			case '=':
				zoomBy(1.5);
				break;
			case '-':
				zoomBy(1 / 1.5);
				break;
			case '0':
				resetView();
				break;
			case 'i':
				if (hasInfo) showInfo = !showInfo;
				break;
		}
	}

	onMount(() => {
		previousFocus = document.activeElement;
		const { overflow } = document.body.style;
		document.body.style.overflow = 'hidden';
		tick().then(() => closeButton?.focus({ preventScroll: true }));
		return () => {
			document.body.style.overflow = overflow;
		};
	});

	onDestroy(() => {
		if (previousFocus instanceof HTMLElement) previousFocus.focus({ preventScroll: true });
	});
</script>

<svelte:window on:keydown={handleKeydown} />

{#if item}
	<div class="lightbox" role="dialog" aria-modal="true" aria-label={item.title || item.alt || 'Image viewer'} data-lightbox-ignore>
		<div class="lightbox-backdrop" style="opacity: {backdropOpacity}"></div>

		<!-- Top bar -->
		<div class="lightbox-bar lightbox-top">
			<div class="lightbox-counter">
				{#if items.length > 1}{index + 1} / {items.length}{/if}
			</div>
			<div class="lightbox-tools">
				<button type="button" class="lb-btn" on:click={() => zoomBy(1 / 1.5)} disabled={!zoomed} title="Zoom out (-)" aria-label="Zoom out">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="7" /><path stroke-linecap="round" d="M8 11h6M20 20l-3.5-3.5" /></svg>
				</button>
				<button type="button" class="lb-scale" on:click={() => resetView()} title="Reset zoom (0)">
					{Math.round(scale * 100)}%
				</button>
				<button type="button" class="lb-btn" on:click={() => zoomBy(1.5)} disabled={scale >= MAX_SCALE} title="Zoom in (+)" aria-label="Zoom in">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="7" /><path stroke-linecap="round" d="M8 11h6M11 8v6M20 20l-3.5-3.5" /></svg>
				</button>
				{#if item.live}
					<button
						type="button"
						class="lb-btn lb-live"
						class:active={livePlaying}
						on:click={playLive}
						disabled={!showLive}
						title={liveFailed ? $t('live.unsupported') : `${$t('live.playNow')} · ${modeLabel(liveMode)}`}
						aria-label={$t('live.playNow')}
					>
						<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="2.6" fill="currentColor" stroke="none" /><circle cx="12" cy="12" r="5.6" /><circle cx="12" cy="12" r="9.2" stroke-dasharray="1.6 2.4" /></svg>
						<span>{$t('live.badge')}</span>
					</button>
				{/if}
				<span class="lb-divider"></span>
				<a class="lb-btn" href={item.src} target="_blank" rel="noopener noreferrer" title="Open original" aria-label="Open original">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 01-1 1H5a1 1 0 01-1-1V7a1 1 0 011-1h5" /></svg>
				</a>
				{#if hasInfo}
					<button type="button" class="lb-btn" class:active={showInfo} on:click={() => (showInfo = !showInfo)} title="Details (i)" aria-label="Toggle details">
						<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9" /><path stroke-linecap="round" d="M12 11v5M12 8h.01" /></svg>
					</button>
				{/if}
				<button type="button" class="lb-btn" bind:this={closeButton} on:click={onClose} title="Close (Esc)" aria-label="Close">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" d="M6 6l12 12M18 6L6 18" /></svg>
				</button>
			</div>
		</div>

		<!-- Stage -->
		<!-- svelte-ignore a11y-no-static-element-interactions -->
		<div
			class="lightbox-stage"
			class:zoomed
			class:grabbing={dragStart && zoomed}
			bind:this={stage}
			on:wheel|nonpassive={handleWheel}
			on:pointerdown={handlePointerDown}
			on:pointermove={handlePointerMove}
			on:pointerup={handlePointerUp}
			on:pointercancel={handlePointerUp}
			on:dragstart|preventDefault
		>
			{#if !loaded && !failed}
				{#if item.thumb}
					<img class="lightbox-thumb" src={item.thumb} alt="" aria-hidden="true" />
				{:else}
					<div class="lightbox-spinner" aria-label="Loading"></div>
				{/if}
			{/if}
			{#if failed}
				<div class="lightbox-error">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M4 16l4.6-4.6a2 2 0 012.8 0L16 16m-2-2l1.6-1.6a2 2 0 012.8 0L20 14M4 4l16 16" /></svg>
					<span>Image failed to load</span>
				</div>
			{/if}
			{#key item.src}
				<img
					bind:this={imgEl}
					src={item.src}
					alt={item.alt || item.title || ''}
					class="lightbox-image"
					class:loaded
					class:over-thumb={!!item.thumb}
					class:animating
					style="transform: translate3d({tx + swipe.dx}px, {ty + swipe.dy}px, 0) scale({scale});"
					draggable="false"
					on:load={() => (loaded = true)}
					on:error={() => (failed = true)}
				/>
				{#if showLive && item.live}
					<video
						use:liveVideo
						src={item.live}
						class="lightbox-live"
						class:playing={livePlaying}
						class:animating
						style="left: {liveBox.left}px; top: {liveBox.top}px; width: {liveBox.width}px; height: {liveBox.height}px; transform: translate3d({tx + swipe.dx}px, {ty + swipe.dy}px, 0) scale({scale});"
						muted
						playsinline
						disablepictureinpicture
						aria-hidden="true"
						tabindex="-1"
						on:playing={() => (livePlaying = true)}
						on:ended={() => (livePlaying = false)}
						on:error={() => {
							livePlaying = false;
							liveFailed = true;
						}}
					></video>
				{/if}
			{/key}
		</div>

		{#if hasPrev}
			<button type="button" class="lb-nav lb-prev" on:click={() => go(-1)} aria-label="Previous image">
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15 5l-7 7 7 7" /></svg>
			</button>
		{/if}
		{#if hasNext}
			<button type="button" class="lb-nav lb-next" on:click={() => go(1)} aria-label="Next image">
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" /></svg>
			</button>
		{/if}

		<!-- Bottom: caption or info panel -->
		{#if hasInfo && showInfo}
			<div class="lightbox-info">
				<slot name="info" {item} {index} />
			</div>
		{:else if !hasInfo && (item.title || item.alt)}
			<div class="lightbox-bar lightbox-caption">{item.title || item.alt}</div>
		{/if}
	</div>
{/if}

<style>
	.lightbox {
		position: fixed;
		inset: 0;
		z-index: 1000;
		display: flex;
		flex-direction: column;
		color: #fff;
		animation: lb-fade 0.18s ease-out;
		touch-action: none;
		user-select: none;
	}

	.lightbox-backdrop {
		position: absolute;
		inset: 0;
		background: rgb(8 8 10 / 0.94);
		backdrop-filter: blur(6px);
		transition: opacity 0.2s ease;
	}

	.lightbox-bar {
		position: relative;
		z-index: 2;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 10px 12px;
		padding-top: max(10px, env(safe-area-inset-top));
	}

	.lightbox-counter {
		font-size: 13px;
		font-variant-numeric: tabular-nums;
		color: rgb(255 255 255 / 0.7);
		padding-left: 6px;
	}

	.lightbox-tools {
		display: flex;
		align-items: center;
		gap: 2px;
		padding: 3px;
		border-radius: 999px;
		background: rgb(255 255 255 / 0.08);
		backdrop-filter: blur(8px);
	}

	.lb-btn,
	.lb-scale {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		height: 34px;
		min-width: 34px;
		border: 0;
		border-radius: 999px;
		background: transparent;
		color: rgb(255 255 255 / 0.85);
		cursor: pointer;
		transition: background 0.15s ease, color 0.15s ease, opacity 0.15s ease;
	}

	.lb-btn svg {
		width: 18px;
		height: 18px;
	}

	.lb-scale {
		min-width: 52px;
		font-size: 12px;
		font-variant-numeric: tabular-nums;
	}

	.lb-btn:hover:not(:disabled),
	.lb-scale:hover,
	.lb-btn.active {
		background: rgb(255 255 255 / 0.14);
		color: #fff;
	}

	.lb-btn:disabled {
		opacity: 0.35;
		cursor: default;
	}

	.lb-btn:focus-visible,
	.lb-scale:focus-visible,
	.lb-nav:focus-visible {
		outline: 2px solid rgb(255 255 255 / 0.7);
		outline-offset: 1px;
	}

	.lb-divider {
		width: 1px;
		height: 18px;
		margin: 0 4px;
		background: rgb(255 255 255 / 0.18);
	}

	.lightbox-stage {
		position: relative;
		z-index: 1;
		flex: 1;
		min-height: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		overflow: hidden;
		padding: 8px 16px 16px;
		cursor: zoom-in;
	}

	.lightbox-stage.zoomed {
		cursor: grab;
	}

	.lightbox-stage.grabbing {
		cursor: grabbing;
	}

	.lightbox-image {
		max-width: 100%;
		max-height: 100%;
		object-fit: contain;
		border-radius: 4px;
		opacity: 0;
		transform-origin: center center;
		will-change: transform;
		box-shadow: 0 20px 60px rgb(0 0 0 / 0.5);
		transition: opacity 0.25s ease;
	}

	.lightbox-image.loaded {
		opacity: 1;
		animation: lb-pop 0.22s cubic-bezier(0.2, 0.9, 0.3, 1.1);
	}

	/* Over a same-sized placeholder a plain cross-fade reads as a sharpen. */
	.lightbox-image.loaded.over-thumb {
		animation: none;
	}

	.lightbox-image.animating {
		transition: opacity 0.25s ease, transform 0.25s cubic-bezier(0.2, 0.8, 0.2, 1);
	}

	.lightbox-live {
		position: absolute;
		object-fit: cover;
		border-radius: 4px;
		opacity: 0;
		pointer-events: none;
		transform-origin: center center;
		will-change: transform;
		transition: opacity 0.3s ease;
	}

	.lightbox-live.playing {
		opacity: 1;
	}

	.lightbox-live.animating {
		transition: opacity 0.3s ease, transform 0.25s cubic-bezier(0.2, 0.8, 0.2, 1);
	}

	.lb-live {
		gap: 4px;
		padding: 0 10px 0 8px;
		font-size: 11px;
		font-weight: 600;
		letter-spacing: 0.06em;
	}

	.lb-live svg {
		width: 16px;
		height: 16px;
	}

	/* The smaller image already on screen stands in until the original loads,
	   filling the same box so the swap is seamless. */
	.lightbox-thumb {
		position: absolute;
		inset: 8px 16px 16px;
		width: calc(100% - 32px);
		height: calc(100% - 24px);
		object-fit: contain;
	}

	.lightbox-spinner {
		position: absolute;
		width: 34px;
		height: 34px;
		border-radius: 50%;
		border: 3px solid rgb(255 255 255 / 0.18);
		border-top-color: #fff;
		animation: lb-spin 0.8s linear infinite;
	}

	.lightbox-error {
		position: absolute;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 10px;
		font-size: 14px;
		color: rgb(255 255 255 / 0.6);
	}

	.lightbox-error svg {
		width: 44px;
		height: 44px;
	}

	.lb-nav {
		position: absolute;
		top: 50%;
		z-index: 2;
		display: flex;
		align-items: center;
		justify-content: center;
		width: 44px;
		height: 44px;
		margin-top: -22px;
		border: 0;
		border-radius: 999px;
		background: rgb(255 255 255 / 0.1);
		color: #fff;
		cursor: pointer;
		backdrop-filter: blur(8px);
		transition: background 0.15s ease, transform 0.15s ease;
	}

	.lb-nav:hover {
		background: rgb(255 255 255 / 0.2);
	}

	.lb-nav:active {
		transform: scale(0.94);
	}

	.lb-nav svg {
		width: 22px;
		height: 22px;
	}

	.lb-prev {
		left: 16px;
	}

	.lb-next {
		right: 16px;
	}

	.lightbox-caption {
		justify-content: center;
		padding-bottom: max(14px, env(safe-area-inset-bottom));
		font-size: 13px;
		color: rgb(255 255 255 / 0.75);
		text-align: center;
		overflow-wrap: anywhere;
	}

	.lightbox-info {
		position: relative;
		z-index: 2;
		padding: 0 12px max(12px, env(safe-area-inset-bottom));
		animation: lb-rise 0.2s ease-out;
		user-select: text;
		touch-action: auto;
	}

	@media (max-width: 640px) {
		.lb-nav {
			display: none;
		}
		.lb-scale {
			min-width: 44px;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.lightbox,
		.lightbox-image.loaded,
		.lightbox-info {
			animation: none;
		}
		/* Over a same-sized placeholder a plain cross-fade reads as a sharpen. */
	.lightbox-image.loaded.over-thumb {
		animation: none;
	}

	.lightbox-image.animating {
			transition: opacity 0.15s ease;
		}
	}

	@keyframes lb-fade {
		from {
			opacity: 0;
		}
	}

	@keyframes lb-pop {
		from {
			opacity: 0;
			scale: 0.96;
		}
	}

	@keyframes lb-rise {
		from {
			opacity: 0;
			transform: translateY(8px);
		}
	}

	@keyframes lb-spin {
		to {
			transform: rotate(360deg);
		}
	}
</style>
