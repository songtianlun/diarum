<script lang="ts">
	export let src: string;
	/** Tried when `src` fails, e.g. the original of a missing thumbnail. */
	export let fallback = '';
	export let alt = '';
	export let eager = false;

	let loaded = false;
	let failed = false;
	let requested = '';
	let current = '';

	$: if (src !== requested) {
		requested = src;
		current = src;
		loaded = false;
		failed = false;
	}

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
		{/key}
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

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		opacity: 0;
		transform: scale(1.03);
		transition: opacity 0.3s ease, transform 0.4s ease;
	}

	.loaded img {
		opacity: 1;
		transform: none;
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
