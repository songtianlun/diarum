<script lang="ts">
	/**
	 * Pixel-art icon set for the Windows 95 skin.
	 *
	 * Every glyph is drawn on a 16×16 grid with integer coordinates and
	 * `shape-rendering: crispEdges`, so it stays hard-edged at 1× the way a
	 * real .ico would — no antialiased curves, no gradients.
	 *
	 * `diarum` is the exception: it renders the actual project logo as a
	 * raster, since that mark identifies the app and a hand-drawn stand-in
	 * would misrepresent it. It deliberately opts out of `crispEdges` —
	 * nearest-neighbour scaling of a photographic PNG produces exactly the
	 * jagged fringe we want to avoid.
	 */
	import type { Win95IconName } from './types';

	export let name: Win95IconName;
	export let size = 16;
	/** When set, the glyph is exposed to assistive tech with this label. */
	export let title: string | undefined = undefined;
</script>

{#if name === 'diarum'}
	<!-- Pre-scaled to 64px so the export path inlines ~6KB, not the 490KB
	     master. No border/background: the PNG's own soft alpha does the
	     blending against whatever surface it sits on. -->
	<img
		class="w95-logo"
		src="/logo-64.png"
		width={size}
		height={size}
		alt={title ?? ''}
		aria-hidden={title ? undefined : 'true'}
	/>
{:else}
<svg
	width={size}
	height={size}
	viewBox="0 0 16 16"
	xmlns="http://www.w3.org/2000/svg"
	shape-rendering="crispEdges"
	aria-hidden={title ? undefined : 'true'}
	role={title ? 'img' : undefined}
>
	{#if title}<title>{title}</title>{/if}

	{#if name === 'book'}
		<!-- closed book — used for year nodes -->
		<path d="M3 2h9l2 2v10H3z" fill="#800000" />
		<path d="M3 2h9l2 2v1H3z" fill="#c04040" />
		<path d="M3 2h2v12H3z" fill="#500000" />
		<path d="M6 5h6v1H6zM6 7h6v1H6zM6 9h4v1H6z" fill="#ffd0d0" />
		<path d="M3 2h9l2 2v10H3z" fill="none" stroke="#000" stroke-width="1" />
	{:else if name === 'folder'}
		<path d="M1 4h5l1 1h8v9H1z" fill="#ffd75e" />
		<path d="M1 4h5l1 1h8v1H1z" fill="#fff0b0" />
		<path d="M1 4h5l1 1h8v9H1z" fill="none" stroke="#000" />
		<path d="M2 13h12" stroke="#c09000" />
	{:else if name === 'folder-open'}
		<path d="M1 4h5l1 1h6v2H4L2 13H1z" fill="#ffd75e" />
		<path d="M4 7h11l-2 7H2z" fill="#ffe9a0" />
		<path d="M1 4h5l1 1h6v2M4 7h11l-2 7H1V4" fill="none" stroke="#000" />
	{:else if name === 'page'}
		<!-- written page — a day that has an entry -->
		<path d="M3 1h7l3 3v11H3z" fill="#fff" />
		<path d="M10 1l3 3h-3z" fill="#c0c0c0" />
		<path d="M3 1h7l3 3v11H3z" fill="none" stroke="#000" />
		<path d="M10 1v3h3" fill="none" stroke="#000" />
		<path d="M5 6h6v1H5zM5 8h6v1H5zM5 10h6v1H5zM5 12h4v1H5z" fill="#000080" />
	{:else if name === 'page-blank'}
		<path d="M3 1h7l3 3v11H3z" fill="#fff" />
		<path d="M10 1l3 3h-3z" fill="#c0c0c0" />
		<path d="M3 1h7l3 3v11H3z" fill="none" stroke="#808080" />
		<path d="M10 1v3h3" fill="none" stroke="#808080" />
	{:else if name === 'notepad'}
		<path d="M2 1h12v14H2z" fill="#fff" stroke="#000" />
		<path d="M2 1h12v2H2z" fill="#000080" />
		<path d="M4 5h8v1H4zM4 7h8v1H4zM4 9h8v1H4zM4 11h5v1H4z" fill="#0000a0" />
	{:else if name === 'contents'}
		<path d="M1 2h14v12H1z" fill="#fff" stroke="#000" />
		<path d="M3 4h3v3H3z" fill="#ffd75e" stroke="#000" />
		<path d="M8 5h5v1H8z" fill="#000" />
		<path d="M3 9h3v3H3z" fill="#ffd75e" stroke="#000" />
		<path d="M8 10h5v1H8z" fill="#000" />
	{:else if name === 'calendar'}
		<path d="M1 2h14v13H1z" fill="#fff" stroke="#000" />
		<path d="M1 2h14v4H1z" fill="#c00000" />
		<path d="M4 1h2v3H4zM10 1h2v3h-2z" fill="#e0e0e0" stroke="#000" />
		<path d="M3 8h2v2H3zM7 8h2v2H7zM11 8h2v2h-2zM3 11h2v2H3zM7 11h2v2H7z" fill="#000080" />
	{:else if name === 'left'}
		<path d="M9 3h2v2H9zM7 5h2v2H7zM5 7h2v2H5zM7 9h2v2H7zM9 11h2v2H9z" fill="currentColor" />
	{:else if name === 'right'}
		<path d="M5 3h2v2H5zM7 5h2v2H7zM9 7h2v2H9zM7 9h2v2H7zM5 11h2v2H5z" fill="currentColor" />
	{:else if name === 'robot'}
		<path d="M7 1h2v2H7z" fill="#808080" />
		<path d="M3 3h10v9H3z" fill="#c0c0c0" stroke="#000" />
		<path d="M5 5h2v2H5zM9 5h2v2H9z" fill="#00c0c0" stroke="#000" />
		<path d="M6 9h4v1H6z" fill="#000" />
		<path d="M1 5h2v4H1zM13 5h2v4h-2z" fill="#808080" stroke="#000" />
	{:else if name === 'share'}
		<path d="M10 1h4v4h-4zM10 11h4v4h-4zM1 6h4v4H1z" fill="#c0c0c0" stroke="#000" />
		<path d="M5 7l5-3M5 9l5 3" stroke="#000080" fill="none" />
	{:else if name === 'search'}
		<circle cx="7" cy="7" r="4.5" fill="#a0e0ff" stroke="#000" />
		<path d="M10 10l4 4" stroke="#000" stroke-width="2" fill="none" />
	{:else if name === 'settings'}
		<path d="M6 1h4v2h2l1 1v2h2v4h-2v2l-1 1h-2v2H6v-2H4l-1-1v-2H1V6h2V4l1-1h2z" fill="#c0c0c0" stroke="#000" />
		<circle cx="8" cy="8" r="2.5" fill="#fff" stroke="#000" />
	{:else if name === 'media'}
		<path d="M1 3h14v10H1z" fill="#fff" stroke="#000" />
		<path d="M2 10l3-4 3 3 3-4 3 5z" fill="#00a000" />
		<circle cx="5" cy="5.5" r="1.5" fill="#ffd75e" stroke="#000" />
	{:else if name === 'disk'}
		<path d="M1 1h12l2 2v12H1z" fill="#404040" stroke="#000" />
		<path d="M4 1h7v5H4z" fill="#c0c0c0" stroke="#000" />
		<path d="M8 2h2v3H8z" fill="#404040" />
		<path d="M3 9h10v6H3z" fill="#e0e0e0" stroke="#000" />
	{:else if name === 'saved'}
		<path d="M2 8h2v2H2zM4 10h2v2H4zM6 8h2v2H6zM8 6h2v2H8zM10 4h2v2h-2zM12 2h2v2h-2z" fill="#008000" />
	{:else if name === 'dirty'}
		<path d="M8 1l7 13H1z" fill="#ffd75e" stroke="#000" />
		<path d="M7 6h2v4H7zM7 11h2v2H7z" fill="#000" />
	{:else if name === 'offline'}
		<circle cx="8" cy="8" r="6.5" fill="#c0c0c0" stroke="#000" />
		<path d="M4 4l8 8" stroke="#c00000" stroke-width="2" fill="none" />
	{:else if name === 'syncing'}
		<path d="M8 2a6 6 0 016 6h-2a4 4 0 00-4-4z" fill="#000080" />
		<path d="M8 14a6 6 0 01-6-6h2a4 4 0 004 4z" fill="#000080" />
	{:else if name === 'info'}
		<circle cx="8" cy="8" r="7" fill="#000080" stroke="#000" />
		<path d="M7 3h2v2H7zM7 6h2v7H7z" fill="#fff" />
	{:else if name === 'h1' || name === 'h2' || name === 'h3'}
		<!-- "H" plus a subscript level digit, so the outline shows depth at a glance -->
		<path d="M2 3h2v9H2zM6 3h2v9H6zM4 6h2v2H4z" fill="#000080" />
		{#if name === 'h1'}
			<path d="M10 7h2v1h-2zM11 8h1v3h-1zM10 11h3v1h-3z" fill="#000080" />
		{:else if name === 'h2'}
			<path d="M9 7h3v1H9zM11 8h1v1h-1zM9 9h3v1H9zM9 10h1v1H9zM9 11h3v1H9z" fill="#000080" />
		{:else}
			<path d="M9 7h3v1H9zM11 8h1v1h-1zM10 9h2v1h-2zM11 10h1v1h-1zM9 11h3v1H9z" fill="#000080" />
		{/if}
	{:else if name === 'tree-expand'}
		<!-- depicts the resulting state: an open node with its children showing -->
		<rect x="0.5" y="1.5" width="8" height="8" fill="#fff" stroke="#000" />
		<path d="M2 5h5v1H2z" fill="#000" />
		<path d="M10 5h6v1h-6z" fill="#000080" />
		<path d="M4 10h1v6H4z" fill="#808080" />
		<path d="M5 12h4v1H5zM5 15h4v1H5z" fill="#808080" />
		<path d="M10 12h6v1h-6zM10 15h6v1h-6z" fill="#000080" />
	{:else if name === 'tree-collapse'}
		<!-- the closed counterpart: a single node, nothing beneath it -->
		<rect x="0.5" y="1.5" width="8" height="8" fill="#fff" stroke="#000" />
		<path d="M2 5h5v1H2zM4 3h1v5H4z" fill="#000" />
		<path d="M10 5h6v1h-6z" fill="#000080" />
	{:else if name === 'outline'}
		<path d="M2 3h3v2H2zM7 3h7v2H7zM4 7h3v2H4zM9 7h5v2H9zM6 11h3v2H6zM11 11h3v2h-3z" fill="#000080" />
	{:else if name === 'star'}
		<path d="M8 1l2 5h5l-4 3 1.5 5L8 11l-4.5 3L5 9 1 6h5z" fill="#ffd75e" stroke="#000" />
	{:else if name === 'home'}
		<path d="M8 1l7 6h-2v8H3V7H1z" fill="#c04040" stroke="#000" />
		<path d="M5 9h3v6H5z" fill="#804000" stroke="#000" />
		<path d="M10 9h3v3h-3z" fill="#a0e0ff" stroke="#000" />
	{:else if name === 'diarum-pixel'}
		<!-- Hand-drawn stand-in for the app, kept for the places where the
		     photographic logo reads as out of place against flat chrome. -->
		<path d="M2 1h10l2 2v12H2z" fill="#f0e0c0" stroke="#000" />
		<path d="M2 1h3v14H2z" fill="#804000" stroke="#000" />
		<path d="M7 5h5v1H7zM7 7h5v1H7zM7 9h4v1H7z" fill="#805020" />
	{/if}
</svg>
{/if}

<style>
	svg {
		display: block;
		flex-shrink: 0;
	}

	/* Matches the svg box exactly so the logo drops into the same slot, with
	   nothing painted around it — no border, no background, no radius. */
	.w95-logo {
		display: block;
		flex-shrink: 0;
		object-fit: contain;
		background: none;
		border: 0;
		border-radius: 0;
		/* The surrounding skin sets `image-rendering: pixelated` on some
		   surfaces; this mark needs smooth downscaling instead. */
		image-rendering: auto;
	}
</style>
