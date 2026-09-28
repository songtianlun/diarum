<script lang="ts">
	/**
	 * Ranked list with proportional bars, e.g. "top users". Rows are buttons
	 * when `onpick` is given so a click can apply the row as a filter.
	 */
	import { getIntlLocale } from '$lib/i18n';

	interface Row {
		key: string;
		label: string;
		count: number;
		hint?: string;
		tone?: string;
		mono?: boolean;
	}

	let {
		rows = [],
		emptyLabel = '—',
		barClass = 'bg-primary/15',
		onpick
	}: {
		rows?: Row[];
		emptyLabel?: string;
		barClass?: string;
		onpick?: (key: string) => void;
	} = $props();

	let max = $derived(Math.max(1, ...rows.map((r) => r.count)));
</script>

{#snippet content(row: Row)}
	<span class="absolute inset-y-0 left-0 rounded-md {barClass}" style="width: {(row.count / max) * 100}%"></span>
	<span class="relative min-w-0 flex-1 truncate text-foreground {row.mono ? 'font-mono text-xs' : ''}" title={row.label}>{row.label}</span>
	{#if row.hint}<span class="relative flex-shrink-0 text-[11px] {row.tone || 'text-muted-foreground'}">{row.hint}</span>{/if}
	<span class="relative flex-shrink-0 tabular-nums text-xs text-muted-foreground">{row.count.toLocaleString(getIntlLocale())}</span>
{/snippet}

{#if rows.length === 0}
	<div class="py-6 text-center text-xs text-muted-foreground">{emptyLabel}</div>
{:else}
	<ul class="space-y-1">
		{#each rows as row (row.key)}
			<li>
				{#if onpick}
					<button type="button" onclick={() => onpick(row.key)} class="relative flex w-full items-center gap-2 overflow-hidden rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted/60 transition-colors">
						{@render content(row)}
					</button>
				{:else}
					<div class="relative flex w-full items-center gap-2 overflow-hidden rounded-md px-2 py-1.5 text-sm">
						{@render content(row)}
					</div>
				{/if}
			</li>
		{/each}
	</ul>
{/if}
