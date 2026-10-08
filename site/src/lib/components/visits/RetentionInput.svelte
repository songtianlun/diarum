<script lang="ts">
	/**
	 * Retention period: common presets in years, "forever" (0) or a custom
	 * number of days.
	 */
	import { t } from '$lib/i18n';

	let { value = $bindable(0), presets = [365, 1095, 1825, 3650], max = 36500, id }: { value: number; presets?: number[]; max?: number; id?: string } = $props();

	let custom = $state(false);
	let choice = $derived(custom ? 'custom' : value === 0 ? '0' : presets.includes(value) ? String(value) : 'custom');

	function pick(next: string) {
		if (next === 'custom') {
			custom = true;
			if (value === 0) value = 365;
			return;
		}
		custom = false;
		value = Number(next);
	}

	function label(days: number): string {
		return days % 365 === 0 ? `${days / 365} ${$t('admin.visits.years')}` : `${days} ${$t('admin.visits.days')}`;
	}
</script>

<div class="flex flex-wrap items-center gap-2">
	<select {id} value={choice} onchange={(e) => pick((e.currentTarget as HTMLSelectElement).value)} class="px-3 py-2 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary/40">
		{#each presets as days}<option value={String(days)}>{label(days)}</option>{/each}
		<option value="0">{$t('admin.visits.forever')}</option>
		<option value="custom">{$t('admin.visits.custom')}</option>
	</select>
	{#if choice === 'custom'}
		<input type="number" min="1" {max} bind:value aria-label={$t('admin.visits.days')} class="w-28 px-3 py-2 bg-background border border-border rounded-lg text-sm tabular-nums focus:outline-none focus:ring-2 focus:ring-primary/40" />
		<span class="text-sm text-muted-foreground">{$t('admin.visits.days')}</span>
	{/if}
</div>
