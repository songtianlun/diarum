<script lang="ts">
	/**
	 * The Start menu, complete with the vertical branding strip Win95 painted
	 * down the left edge (bottom-to-top, bold, over a blue ramp).
	 *
	 * It exists so the taskbar button is a real menu rather than a one-way trip
	 * home — every top-level destination in the app is listed here, with Home
	 * as an explicit entry instead of a hidden side effect of the click.
	 */
	import Win95Icon from './Win95Icon.svelte';
	import type { Win95StartItem } from './types';
	import { t } from '$lib/i18n';

	export let items: Win95StartItem[] = [];
	export let banner = 'Diarum 95';
	export let onClose: () => void;

	function run(item: Win95StartItem) {
		if (item.sep) return;
		onClose();
		item.action?.();
	}

	function onKey(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			event.stopPropagation();
			onClose();
		}
	}
</script>

<svelte:window on:keydown={onKey} />

<button class="scrim" tabindex="-1" aria-label={$t('win95.close')} on:click={onClose}></button>

<div class="start-menu" role="menu">
	<div class="banner" aria-hidden="true"><span>{banner}</span></div>

	<div class="items">
		{#each items as item}
			{#if item.sep}
				<div class="w95-sep"></div>
			{:else}
				<button type="button" class="item" role="menuitem" on:click={() => run(item)}>
					<span class="ico">
						{#if item.icon}<Win95Icon name={item.icon} size={20} />{/if}
					</span>
					<span class="label">{item.label}</span>
				</button>
			{/if}
		{/each}
	</div>
</div>

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 64;
		background: transparent;
		cursor: default;
	}

	.start-menu {
		position: absolute;
		bottom: calc(100% + 3px);
		left: 0;
		z-index: 65;
		display: flex;
		min-width: 196px;
		padding: 3px;
		background: var(--w95-face);
		box-shadow:
			inset -1px -1px 0 0 var(--w95-dark),
			inset 1px 1px 0 0 var(--w95-light),
			inset -2px -2px 0 0 var(--w95-shadow),
			inset 2px 2px 0 0 var(--w95-hilite),
			2px 2px 0 0 rgba(0, 0, 0, 0.35);
	}

	.banner {
		flex-shrink: 0;
		width: 22px;
		display: flex;
		align-items: flex-end;
		justify-content: center;
		padding-bottom: 6px;
		background: linear-gradient(to top, #000080 0%, #000080 35%, #1084d0 85%, #58b0e8 100%);
	}

	/* vertical-rl + 180° gives bottom-to-top text, the way the real strip read */
	.banner span {
		writing-mode: vertical-rl;
		transform: rotate(180deg);
		color: #fff;
		font-size: 15px;
		font-weight: 700;
		line-height: 1;
		white-space: nowrap;
		user-select: none;
	}

	.items {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		padding: 2px 0 2px 2px;
	}

	.item {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		padding: 4px 22px 4px 5px;
		font-family: inherit;
		font-size: 12px;
		color: var(--w95-text);
		background: transparent;
		text-align: left;
		white-space: nowrap;
	}

	.item:hover {
		background: var(--w95-select);
		color: var(--w95-select-text);
	}

	.ico {
		flex-shrink: 0;
		width: 20px;
		height: 20px;
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.label {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
	}
</style>
