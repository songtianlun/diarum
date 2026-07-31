<script lang="ts">
	/**
	 * Notepad's menu bar. Behaves like the real thing: click to open, then
	 * hovering another title switches menus without a second click; Escape or
	 * any outside click dismisses.
	 *
	 * These menus expose only actions that already exist elsewhere in the app —
	 * nothing here changes behaviour, it just gives the shell a real place to
	 * put them.
	 */
	import type { Win95Menu, Win95MenuItem } from './types';

	export let menus: Win95Menu[] = [];

	let openIndex = -1;

	function toggle(i: number) {
		openIndex = openIndex === i ? -1 : i;
	}

	function hover(i: number) {
		if (openIndex !== -1) openIndex = i;
	}

	function run(item: Win95MenuItem) {
		if (item.disabled || item.sep) return;
		openIndex = -1;
		item.action?.();
	}

	function onKey(event: KeyboardEvent) {
		if (event.key === 'Escape' && openIndex !== -1) {
			event.stopPropagation();
			openIndex = -1;
		}
	}
</script>

<svelte:window on:keydown={onKey} />

<div class="w95-menubar" role="menubar">
	{#each menus as menu, i}
		<div class="holder">
			<button
				type="button"
				class="w95-menubar-item"
				class:open={openIndex === i}
				aria-haspopup="true"
				aria-expanded={openIndex === i}
				on:click={() => toggle(i)}
				on:mouseenter={() => hover(i)}
			>
				{#if menu.mnemonic !== undefined}
					<span>{menu.label.slice(0, menu.mnemonic)}</span><u
						>{menu.label.slice(menu.mnemonic, menu.mnemonic + 1)}</u
					><span>{menu.label.slice(menu.mnemonic + 1)}</span>
				{:else}
					{menu.label}
				{/if}
			</button>

			{#if openIndex === i}
				<div class="w95-menu" role="menu">
					{#each menu.items as item}
						{#if item.sep}
							<div class="w95-sep"></div>
						{:else}
							<button
								type="button"
								class="w95-menu-item"
								role="menuitem"
								disabled={item.disabled}
								on:click={() => run(item)}
							>
								{#if item.checked}<span class="checkmark">✓</span>{/if}
								<span>{item.label}</span>
								{#if item.shortcut}<span class="shortcut">{item.shortcut}</span>{/if}
							</button>
						{/if}
					{/each}
				</div>
			{/if}
		</div>
	{/each}
</div>

{#if openIndex !== -1}
	<button class="scrim" tabindex="-1" aria-label="close menu" on:click={() => (openIndex = -1)}></button>
{/if}

<style>
	.holder {
		position: relative;
	}

	.w95-menu {
		top: 100%;
		left: 0;
	}

	.scrim {
		position: fixed;
		inset: 0;
		z-index: 55;
		background: transparent;
		cursor: default;
	}
</style>
