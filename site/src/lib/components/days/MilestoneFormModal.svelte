<script lang="ts">
	import { untrack } from 'svelte';
	import Sheet from './Sheet.svelte';
	import { createMilestone, updateMilestone, type Milestone, type MilestoneInput } from '$lib/api/days';
	import { getToday } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	let {
		dayId,
		milestone = null,
		endTaken = null,
		onclose,
		onsaved
	}: {
		dayId: string;
		milestone?: Milestone | null;
		/** Name of another milestone that is already the day's end, if any. */
		endTaken?: string | null;
		onclose: () => void;
		onsaved: (m: Milestone) => void;
	} = $props();

	const seed = untrack(() => milestone);
	let form: MilestoneInput = $state({
		name: seed?.name ?? '',
		date: seed?.date ?? getToday(),
		note: seed?.note ?? '',
		end: seed?.end ?? false
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
			error = (e as { status?: number }).status === 409 ? $t('days.milestones.endExists') : (e as Error).message;
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
		<label class="flex items-center justify-between gap-3 px-3 py-2.5 rounded-lg border border-border/60 {endTaken ? 'opacity-60 cursor-not-allowed' : 'cursor-pointer'}">
			<span class="min-w-0">
				<span class="flex items-center gap-1.5 text-sm text-foreground">
					<span class="w-2.5 h-2.5 rounded-[3px] bg-foreground/70"></span>
					{$t('days.milestones.markEnd')}
				</span>
				<span class="block text-[11px] text-muted-foreground mt-0.5">
					{endTaken ? $t('days.milestones.endTaken', { name: endTaken }) : $t('days.milestones.markEndHint')}
				</span>
			</span>
			<input type="checkbox" class="sr-only peer" bind:checked={form.end} disabled={!!endTaken} />
			<span class="relative w-9 h-5 flex-shrink-0 rounded-full bg-muted-foreground/30 transition-colors peer-checked:bg-primary after:absolute after:top-0.5 after:left-0.5 after:w-4 after:h-4 after:rounded-full after:bg-card after:shadow after:transition-transform peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring/50"></span>
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
