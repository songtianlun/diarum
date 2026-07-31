<script lang="ts">
	/** Mood / weather picker, styled as a Win95 palette dialog. */
	import Win95Dialog from './Win95Dialog.svelte';
	import { t } from '$lib/i18n';

	export let title: string;
	export let options: string[] = [];
	export let value = '';
	export let onPick: (emoji: string) => void;
	export let onClose: () => void;

	function choose(emoji: string) {
		onPick(emoji === value ? '' : emoji);
		onClose();
	}
</script>

<Win95Dialog {title} icon="star" {onClose}>
	<div class="w95-groupbox">
		<span class="legend">{title}</span>
		<div class="w95-emoji-grid">
			{#each options as option}
				<button
					type="button"
					class="w95-emoji-cell"
					class:on={option === value}
					title={option}
					on:click={() => choose(option)}
				>
					{option}
				</button>
			{/each}
		</div>
	</div>

	<svelte:fragment slot="actions">
		<button
			type="button"
			class="w95-btn"
			disabled={!value}
			on:click={() => {
				onPick('');
				onClose();
			}}>{$t('win95.clear')}</button
		>
		<button type="button" class="w95-btn" on:click={onClose}>{$t('win95.close')}</button>
	</svelte:fragment>
</Win95Dialog>
