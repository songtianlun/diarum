<script lang="ts">
	/**
	 * Modal dialog frame: raised window, inactive-free title bar with a single
	 * close box, body slot and a right-aligned action row.
	 *
	 * Rendered inside the desktop (not the document body) so it is clipped to
	 * the shell, the way a real child window would be.
	 */
	import Win95Icon from './Win95Icon.svelte';
	import { t } from '$lib/i18n';

	export let title: string;
	export let icon: 'info' | 'calendar' | 'star' | 'contents' | 'outline' | 'notepad' = 'info';
	export let onClose: () => void;
	/** Fills the shell instead of hugging its content — used by the CE sheets. */
	export let full = false;

	function onKey(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			event.stopPropagation();
			onClose();
		}
	}
</script>

<svelte:window on:keydown={onKey} />

<div class="w95-modal-veil">
	<button class="veil-hit" tabindex="-1" aria-label={$t('win95.close')} on:click={onClose}></button>

	<div class="w95-window w95-dialog" class:full role="dialog" aria-modal="true" aria-label={title}>
		<div class="w95-titlebar">
			<Win95Icon name={icon} size={14} />
			<span class="w95-title-text">{title}</span>
			<div class="w95-title-buttons">
				<button type="button" class="w95-title-btn" aria-label={$t('win95.close')} on:click={onClose}>
					<svg width="8" height="7" viewBox="0 0 8 7" shape-rendering="crispEdges">
						<path
							d="M0 0h2v1h1v1h2V1h1V0h2v1H6v1H5v1h1v1h1v1h1v1H6V5H5V4H3v1H2v1H0V5h1V4h1V3h1V2H1V1H0z"
							fill="currentColor"
						/>
					</svg>
				</button>
			</div>
		</div>

		<div class="w95-dialog-body" class:grow={full}>
			<slot />
		</div>

		{#if $$slots.actions}
			<div class="w95-dialog-actions">
				<slot name="actions" />
			</div>
		{/if}
	</div>
</div>

<style>
	.veil-hit {
		position: absolute;
		inset: 0;
		background: transparent;
		cursor: default;
	}

	.w95-dialog {
		position: relative;
		z-index: 1;
	}

	.w95-dialog.full {
		width: 100%;
		height: 100%;
		max-width: none;
	}

	.w95-dialog-body.grow {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
		padding: 6px;
	}
</style>
