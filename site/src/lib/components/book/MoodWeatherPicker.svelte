<script lang="ts">
	/**
	 * A small icon button that shows the selected mood/weather emoji (or an
	 * empty placeholder glyph) and opens a popover grid of presets on click.
	 */
	export let label: 'Mood' | 'Weather' = 'Mood';
	export let value = '';
	export let options: string[] = [];
	export let disabled = false;
	export let align: 'left' | 'right' = 'left';
	export let onSelect: (emoji: string) => void = () => {};

	let open = false;
	let rootEl: HTMLDivElement | undefined;

	function toggle() {
		if (disabled) return;
		open = !open;
	}

	function choose(option: string) {
		onSelect(value === option ? '' : option);
		open = false;
	}

	function clear() {
		onSelect('');
		open = false;
	}

	function handleWindowClick(e: MouseEvent) {
		if (open && rootEl && !rootEl.contains(e.target as Node)) {
			open = false;
		}
	}

	function handleWindowKey(e: KeyboardEvent) {
		if (open && e.key === 'Escape') {
			open = false;
		}
	}

	$: if (disabled) open = false;
</script>

<svelte:window on:click={handleWindowClick} on:keydown={handleWindowKey} />

<div class="picker" bind:this={rootEl}>
	<button
		type="button"
		class="picker-trigger"
		class:has-value={!!value}
		class:is-open={open}
		{disabled}
		title={value ? `${label}: ${value}` : `Set ${label.toLowerCase()}`}
		aria-label={value ? `${label}: ${value}` : `Set ${label.toLowerCase()}`}
		on:click|stopPropagation={toggle}
	>
		{#if value}
			<span class="emoji">{value}</span>
		{:else if label === 'Mood'}
			<svg class="empty-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
				<circle cx="12" cy="12" r="8.5" stroke-width="1.6" stroke-dasharray="2.2 2.6" />
				<path d="M9 10.2h.01M15 10.2h.01" stroke-width="1.8" stroke-linecap="round" />
				<path d="M9 15c1 .8 2 1.2 3 1.2s2-.4 3-1.2" stroke-width="1.6" stroke-linecap="round" fill="none" />
			</svg>
		{:else}
			<svg class="empty-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
				<path
					d="M7.5 17.5h9a3.5 3.5 0 000-7 5 5 0 00-9.6-1.6A4 4 0 007.5 17.5z"
					stroke-width="1.6"
					stroke-dasharray="2.2 2.4"
					stroke-linejoin="round"
				/>
			</svg>
		{/if}
	</button>

	{#if open}
		<div class="picker-pop align-{align}" role="dialog" aria-label="{label} picker">
			<div class="picker-pop-header">
				<span>{label}</span>
				{#if value}
					<button type="button" class="picker-clear" on:click|stopPropagation={clear}>Clear</button>
				{/if}
			</div>
			<div class="picker-grid">
				{#each options as option}
					<button
						type="button"
						class="picker-option"
						class:active={value === option}
						title={option}
						aria-label={`${label} ${option}`}
						on:click|stopPropagation={() => choose(option)}
					>
						<span>{option}</span>
					</button>
				{/each}
			</div>
		</div>
	{/if}
</div>

<style>
	.picker {
		position: relative;
		display: inline-flex;
	}

	.picker-trigger {
		display: flex;
		align-items: center;
		justify-content: center;
		width: clamp(1.6rem, 4.2vw, 1.9rem);
		height: clamp(1.6rem, 4.2vw, 1.9rem);
		border-radius: 0.6rem;
		transition: background-color 0.2s ease, opacity 0.2s ease, transform 0.15s ease;
		flex-shrink: 0;
	}
	.picker-trigger:hover:not(:disabled) {
		background: hsl(var(--muted) / 0.5);
		transform: translateY(-1px);
	}
	.picker-trigger:disabled {
		opacity: 0.4;
	}
	.picker-trigger.is-open {
		background: hsl(var(--muted) / 0.65);
	}
	.picker-trigger.has-value {
		background: hsl(var(--primary) / 0.1);
	}

	.emoji {
		font-size: 1.05rem;
		line-height: 1;
	}
	.empty-icon {
		width: 1.05rem;
		height: 1.05rem;
		color: hsl(var(--muted-foreground) / 0.55);
	}

	.picker-pop {
		position: absolute;
		top: calc(100% + 0.4rem);
		z-index: 40;
		width: 13.5rem;
		padding: 0.65rem;
		border-radius: 0.9rem;
		background: hsl(var(--popover, var(--card)));
		border: 1px solid hsl(var(--border) / 0.7);
		box-shadow: 0 12px 30px hsl(0 0% 0% / 0.16), 0 2px 8px hsl(0 0% 0% / 0.08);
		animation: pop-in 0.15s ease;
	}
	.picker-pop.align-left {
		left: 0;
	}
	.picker-pop.align-right {
		right: 0;
	}

	@keyframes pop-in {
		from {
			opacity: 0;
			transform: translateY(-4px) scale(0.98);
		}
		to {
			opacity: 1;
			transform: translateY(0) scale(1);
		}
	}

	.picker-pop-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		font-size: 0.68rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: hsl(var(--muted-foreground));
		margin-bottom: 0.5rem;
		padding: 0 0.1rem;
	}

	.picker-clear {
		font-size: 0.65rem;
		font-weight: 500;
		text-transform: none;
		letter-spacing: normal;
		padding: 0.15rem 0.5rem;
		border-radius: 999px;
		background: hsl(var(--background) / 0.7);
		border: 1px solid hsl(var(--border) / 0.7);
		transition: background-color 0.15s ease;
	}
	.picker-clear:hover {
		background: hsl(var(--muted) / 0.6);
	}

	.picker-grid {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: 0.35rem;
	}

	.picker-option {
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 0.4rem 0;
		border-radius: 0.6rem;
		border: 1px solid hsl(var(--border) / 0.5);
		background: hsl(var(--background) / 0.6);
		font-size: 1.15rem;
		line-height: 1;
		transition: transform 0.15s ease, border-color 0.15s ease, background-color 0.15s ease, box-shadow 0.15s ease;
	}
	.picker-option:hover {
		transform: translateY(-1px);
		background: hsl(var(--muted) / 0.6);
		border-color: hsl(var(--primary) / 0.3);
	}
	.picker-option.active {
		border-color: hsl(var(--primary) / 0.65);
		background: hsl(var(--primary) / 0.12);
		box-shadow: 0 6px 14px hsl(var(--primary) / 0.12);
	}
</style>
