<script lang="ts">
	export let src: string;
	/** Tried when `src` fails, e.g. the original of a missing thumbnail. */
	export let fallback = '';
	export let alt = '';
	export let eager = false;
	/** Marks a live photo. */
	export let live = false;
	/**
	 * Marks a video: `src` is its poster ('' without one) and `fallback` the
	 * video, whose first frame is shown when there is no poster.
	 */
	export let video = false;

	let loaded = false;
	let failed = false;
	let requested: string | null = null;
	let current = '';

	$: if (src !== requested) {
		requested = src;
		current = src || (video ? fallback : '');
		loaded = false;
		failed = !current;
	}
	$: frame = video && !!current && current === fallback;

	function handleError() {
		if (fallback && current !== fallback) current = fallback;
		else failed = true;
	}

	// Cached images can finish before the load listener is attached.
	function checkComplete(img: HTMLImageElement) {
		if (img.complete && img.naturalWidth > 0) loaded = true;
	}
</script>

<div class="thumb" class:loaded class:failed>
	{#if failed}
		<svg class="thumb-broken" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
			<path stroke-linecap="round" stroke-linejoin="round" d="M4 16l4.6-4.6a2 2 0 012.8 0L16 16m-2-2l1.6-1.6a2 2 0 012.8 0L20 14M4 4l16 16" />
		</svg>
	{:else}
		{#key current}
			{#if frame}
				<!-- #t= makes browsers (Safari too) paint a frame instead of nothing. -->
				<video
					src="{current}#t=0.1"
					muted
					playsinline
					preload="metadata"
					disablepictureinpicture
					tabindex="-1"
					aria-hidden="true"
					on:loadeddata={() => (loaded = true)}
					on:error={() => (failed = true)}
				></video>
			{:else}
				<img
					src={current}
					{alt}
					loading={eager ? 'eager' : 'lazy'}
					decoding="async"
					draggable="false"
					use:checkComplete
					on:load={() => (loaded = true)}
					on:error={handleError}
				/>
			{/if}
		{/key}
	{/if}
	{#if video && !failed}
		<span class="thumb-play" aria-hidden="true">
			<svg viewBox="0 0 24 24" fill="currentColor"><path d="M8 5.5v13a1 1 0 001.5.87l11-6.5a1 1 0 000-1.74l-11-6.5A1 1 0 008 5.5z" /></svg>
		</span>
	{/if}
	{#if live}
		<span class="thumb-live" title="Live">
			<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="2.6" fill="currentColor" stroke="none" /><circle cx="12" cy="12" r="5.6" /><circle cx="12" cy="12" r="9.2" stroke-dasharray="1.6 2.4" /></svg>
			LIVE
		</span>
	{/if}
</div>

<style>
	.thumb {
		position: absolute;
		inset: 0;
		overflow: hidden;
		background: linear-gradient(100deg, hsl(var(--muted)) 30%, hsl(var(--muted-foreground) / 0.1) 50%, hsl(var(--muted)) 70%);
		background-size: 220% 100%;
		animation: thumb-shimmer 1.4s ease-in-out infinite;
	}

	.thumb.loaded,
	.thumb.failed {
		animation: none;
		background: hsl(var(--muted) / 0.5);
	}

	img,
	video {
		width: 100%;
		height: 100%;
		object-fit: cover;
		opacity: 0;
		transform: scale(1.03);
		transition: opacity 0.3s ease, transform 0.4s ease;
	}

	.loaded img,
	.loaded video {
		opacity: 1;
		transform: none;
	}

	.thumb-live {
		position: absolute;
		top: 6px;
		left: 6px;
		display: inline-flex;
		align-items: center;
		gap: 3px;
		height: 18px;
		padding: 0 6px 0 4px;
		border-radius: 999px;
		background: rgb(0 0 0 / 0.42);
		color: #fff;
		font-size: 9px;
		font-weight: 600;
		letter-spacing: 0.06em;
		pointer-events: none;
		backdrop-filter: blur(4px);
	}

	.thumb-live svg {
		width: 12px;
		height: 12px;
	}

	.thumb-play {
		position: absolute;
		top: 50%;
		left: 50%;
		display: flex;
		align-items: center;
		justify-content: center;
		width: 36px;
		height: 36px;
		margin: -18px 0 0 -18px;
		border-radius: 999px;
		background: rgb(0 0 0 / 0.45);
		color: #fff;
		pointer-events: none;
		backdrop-filter: blur(4px);
	}

	.thumb-play svg {
		width: 16px;
		height: 16px;
		margin-left: 2px;
	}

	.thumb-broken {
		position: absolute;
		top: 50%;
		left: 50%;
		width: 28px;
		height: 28px;
		margin: -14px 0 0 -14px;
		color: hsl(var(--muted-foreground) / 0.6);
	}

	@keyframes thumb-shimmer {
		0% {
			background-position: 110% 0;
		}
		100% {
			background-position: -110% 0;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.thumb {
			animation: none;
		}
	}
</style>
