<script lang="ts">
	import type { CommandItem } from './commands';

	export let items: CommandItem[] = [];
	export let selectedIndex = 0;
	export let onSelect: (item: CommandItem) => void;

	let container: HTMLDivElement;

	// Group items by their group property
	$: groupedItems = items.reduce((acc, item) => {
		const group = item.group || 'OTHER';
		if (!acc[group]) acc[group] = [];
		acc[group].push(item);
		return acc;
	}, {} as Record<string, CommandItem[]>);

	// Get flat index for an item
	function getItemIndex(item: CommandItem): number {
		return items.indexOf(item);
	}

	function scrollToSelected() {
		const selected = container?.querySelector('.selected');
		if (selected) {
			selected.scrollIntoView({ block: 'nearest' });
		}
	}

	$: if (selectedIndex !== undefined) {
		scrollToSelected();
	}

	const iconMap: Record<string, string> = {
		text: `<path d="M4 7V4h16v3M9 20h6M12 4v16"/>`,
		h1: `<path d="M4 12h8M4 18V6M12 18V6"/><path d="M17 10v8M21 10v8M17 14h4"/>`,
		h2: `<path d="M4 12h8M4 18V6M12 18V6"/><path d="M21 18h-4c0-4 4-3 4-6 0-1.5-2-2.5-4-1"/>`,
		h3: `<path d="M4 12h8M4 18V6M12 18V6"/><path d="M17.5 10.5c1.7-1 3.5 0 3.5 1.5a2 2 0 0 1-2 2M17.5 17.5c1.7 1 3.5 0 3.5-1.5a2 2 0 0 0-2-2"/>`,
		list: `<line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/>`,
		'list-ordered': `<line x1="10" y1="6" x2="21" y2="6"/><line x1="10" y1="12" x2="21" y2="12"/><line x1="10" y1="18" x2="21" y2="18"/><path d="M4 6h1v4"/><path d="M4 10h2"/><path d="M6 18H4c0-1 2-2 2-3s-1-1.5-2-1"/>`,
		'check-square': `<polyline points="9 11 12 14 22 4"/><path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/>`,
		quote: `<path d="M3 21c3 0 7-1 7-8V5c0-1.25-.756-2.017-2-2H4c-1.25 0-2 .75-2 1.972V11c0 1.25.75 2 2 2 1 0 1 0 1 1v1c0 1-1 2-2 2s-1 .008-1 1.031V21z"/><path d="M15 21c3 0 7-1 7-8V5c0-1.25-.757-2.017-2-2h-4c-1.25 0-2 .75-2 1.972V11c0 1.25.75 2 2 2h.75c0 2.25.25 4-2.75 4v3z"/>`,
		code: `<polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>`,
		minus: `<line x1="5" y1="12" x2="19" y2="12"/>`,
		image: `<rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/>`,
	};
</script>

<div class="command-menu" bind:this={container}>
	{#if items.length === 0}
		<div class="no-results">No results</div>
	{:else}
		{#each Object.entries(groupedItems) as [group, groupItems]}
			<div class="group">
				<div class="group-label">{group}</div>
				{#each groupItems as item}
					<button
						class="command-item"
						class:selected={getItemIndex(item) === selectedIndex}
						on:click={() => onSelect(item)}
						on:mouseenter={() => (selectedIndex = getItemIndex(item))}
					>
						<div class="icon">
							<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
								{@html iconMap[item.icon] || ''}
							</svg>
						</div>
						<span class="title">{item.title}</span>
					</button>
				{/each}
			</div>
		{/each}
	{/if}
</div>

<style>
	.command-menu {
		background: hsl(var(--background));
		border: 1px solid hsl(var(--border));
		border-radius: 10px;
		box-shadow: 0 4px 16px hsl(var(--foreground) / 0.08);
		padding: 8px;
		min-width: 200px;
		max-height: 340px;
		overflow-y: auto;
	}

	.no-results {
		padding: 16px;
		color: hsl(var(--muted-foreground));
		font-size: 13px;
		text-align: center;
	}

	.group {
		margin-bottom: 4px;
	}

	.group:last-child {
		margin-bottom: 0;
	}

	.group-label {
		padding: 6px 8px 4px;
		font-size: 11px;
		font-weight: 600;
		color: hsl(var(--muted-foreground));
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.command-item {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 8px;
		border: none;
		border-radius: 6px;
		background: transparent;
		cursor: pointer;
		text-align: left;
		transition: background 0.1s ease;
	}

	.command-item:hover,
	.command-item.selected {
		background: hsl(var(--accent));
	}

	.icon {
		flex-shrink: 0;
		width: 28px;
		height: 28px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: hsl(var(--muted));
		border-radius: 6px;
	}

	.icon svg {
		width: 16px;
		height: 16px;
		color: hsl(var(--muted-foreground));
	}

	.title {
		font-size: 13px;
		font-weight: 500;
		color: hsl(var(--foreground));
	}
</style>
