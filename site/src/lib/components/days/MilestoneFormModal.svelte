<script lang="ts">
	import { untrack } from 'svelte';
	import Sheet from './Sheet.svelte';
	import { createMilestone, updateMilestone, type Milestone, type MilestoneInput } from '$lib/api/days';
	import { getToday } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	let {
		dayId,
		milestone = null,
		onclose,
		onsaved
	}: { dayId: string; milestone?: Milestone | null; onclose: () => void; onsaved: (m: Milestone) => void } = $props();

	const seed = untrack(() => milestone);
	let form: MilestoneInput = $state({
		name: seed?.name ?? '',
		date: seed?.date ?? getToday(),
		note: seed?.note ?? ''
	});
	let saving = $state(false);
	let error = $state('');

	async function save(event?: SubmitEvent) {
		event?.preventDefault();
		if (!form.name.trim() || !form.date) {
			error = $t('days.form.required');
			return;
		}
		saving = true;
		error = '';
		try {
			onsaved(seed ? await updateMilestone(dayId, seed.id, form) : await createMilestone(dayId, form));
		} catch (e) {
			error = (e as Error).message;
		} finally {
			saving = false;
		}
	}
</script>

<Sheet title={seed ? $t('days.milestones.editTitle') : $t('days.milestones.newTitle')} {onclose}>
	<form id="milestone-form" class="space-y-4" onsubmit={save}>
		<label class="block">
			<span class="block text-xs font-medium text-muted-foreground mb-1.5">{$t('days.form.name')}</span>
			<!-- svelte-ignore a11y_autofocus -->
			<input
				class="w-full px-3 py-2 rounded-lg bg-background border border-input text-sm focus:outline-none focus:ring-2 focus:ring-ring/40"
				bind:value={form.name}
				maxlength="100"
				placeholder={$t('days.milestones.namePlaceholder')}
				autofocus={!seed}
			/>
		</label>
		<label class="block">
			<span class="block text-xs font-medium text-muted-foreground mb-1.5">{$t('days.form.date')}</span>
			<input
				type="date"
				class="w-full px-3 py-2 rounded-lg bg-background border border-input text-sm focus:outline-none focus:ring-2 focus:ring-ring/40"
				bind:value={form.date}
				required
			/>
		</label>
		<label class="block">
			<span class="block text-xs font-medium text-muted-foreground mb-1.5">{$t('days.form.note')}</span>
			<textarea
				class="w-full px-3 py-2 rounded-lg bg-background border border-input text-sm resize-none focus:outline-none focus:ring-2 focus:ring-ring/40"
				rows="3"
				maxlength="2000"
				bind:value={form.note}
				placeholder={$t('days.form.notePlaceholder')}
			></textarea>
		</label>
		{#if error}
			<p class="text-xs text-destructive">{error}</p>
		{/if}
	</form>

	{#snippet footer()}
		<button type="button" onclick={onclose} class="flex-1 py-2 rounded-lg border border-border/60 text-sm hover:bg-muted/50">{$t('days.form.cancel')}</button>
		<button type="submit" form="milestone-form" disabled={saving} class="flex-1 py-2 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:opacity-90 disabled:opacity-60">
			{saving ? $t('days.form.saving') : $t('days.form.save')}
		</button>
	{/snippet}
</Sheet>
