<script lang="ts">
	import { untrack } from 'svelte';
	import Sheet from './Sheet.svelte';
	import { createDay, updateDay, createDayTag, type Day, type DayInput, type DayRepeat, type DayTag } from '$lib/api/days';
	import { TAG_COLORS, nextTagColor } from '$lib/utils/days';
	import { getToday } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	let {
		day = null,
		tags = $bindable([]),
		onclose,
		onsaved
	}: { day?: Day | null; tags?: DayTag[]; onclose: () => void; onsaved: (day: Day) => void } = $props();

	const emojis = ['📅', '🎂', '❤️', '💍', '🎓', '✈️', '🏠', '👶', '💼', '🎉', '🌱', '⭐'];
	const repeats: DayRepeat[] = ['', 'week', 'month', 'year'];

	// The form edits a copy; the day passed in only seeds it.
	const seed = untrack(() => day);
	let form: DayInput = $state({
		name: seed?.name ?? '',
		date: seed?.date ?? getToday(),
		emoji: seed?.emoji || '📅',
		note: seed?.note ?? '',
		repeat: seed?.repeat ?? '',
		include_start: seed?.include_start ?? false,
		show_on_home: seed?.show_on_home ?? true,
		tags: [...(seed?.tags ?? [])]
	});
	// Inline "new tag" field; a tag made here is picked right away.
	let newTag = $state('');
	let addingTag = $state(false);

	function toggleTag(id: string) {
		form.tags = form.tags.includes(id) ? form.tags.filter((x) => x !== id) : [...form.tags, id];
	}

	async function addTag() {
		const name = newTag.trim();
		addingTag = false;
		newTag = '';
		if (!name) return;
		const existing = tags.find((x) => x.name === name);
		if (existing) {
			if (!form.tags.includes(existing.id)) toggleTag(existing.id);
			return;
		}
		try {
			const tag = await createDayTag({ name, color: nextTagColor(tags.map((x) => x.color)) });
			tags = [...tags, tag];
			toggleTag(tag.id);
		} catch (e) {
			error = (e as Error).message;
		}
	}
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
			const saved = seed ? await updateDay(seed.id, form) : await createDay(form);
			onsaved(saved);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			saving = false;
		}
	}
</script>

<Sheet title={seed ? $t('days.form.editTitle') : $t('days.form.newTitle')} {onclose}>
	<form id="day-form" class="space-y-4" onsubmit={save}>
		<div>
			<span class="block text-xs font-medium text-muted-foreground mb-1.5">{$t('days.form.emoji')}</span>
			<div class="flex flex-wrap gap-1.5">
				{#each emojis as emoji}
					<button
						type="button"
						onclick={() => (form.emoji = emoji)}
						aria-pressed={form.emoji === emoji}
						class="w-9 h-9 rounded-lg text-lg flex items-center justify-center transition-all {form.emoji === emoji ? 'bg-primary/10 ring-2 ring-primary/50' : 'bg-muted/40 hover:bg-muted'}"
					>{emoji}</button>
				{/each}
				<!-- Any other emoji; stays empty while a preset is picked. -->
				<input
					class="w-14 h-9 rounded-lg text-center text-lg border focus:outline-none {emojis.includes(form.emoji) ? 'bg-muted/40 border-transparent' : 'bg-primary/10 border-primary/50'} focus:border-ring"
					maxlength="8"
					placeholder="✎"
					value={emojis.includes(form.emoji) ? '' : form.emoji}
					oninput={(e) => (form.emoji = e.currentTarget.value.trim() || '📅')}
					aria-label={$t('days.form.emoji')}
				/>
			</div>
		</div>

		<label class="block">
			<span class="block text-xs font-medium text-muted-foreground mb-1.5">{$t('days.form.name')}</span>
			<!-- svelte-ignore a11y_autofocus -->
			<input
				class="w-full px-3 py-2 rounded-lg bg-background border border-input text-sm focus:outline-none focus:ring-2 focus:ring-ring/40"
				bind:value={form.name}
				maxlength="100"
				placeholder={$t('days.form.namePlaceholder')}
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

		<div>
			<span class="block text-xs font-medium text-muted-foreground mb-1.5">{$t('days.form.repeat')}</span>
			<div class="grid grid-cols-4 gap-1 p-1 rounded-lg bg-muted/50">
				{#each repeats as repeat}
					<button
						type="button"
						onclick={() => (form.repeat = repeat)}
						aria-pressed={form.repeat === repeat}
						class="py-1.5 rounded-md text-xs transition-colors {form.repeat === repeat ? 'bg-card text-foreground shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
					>{$t(`days.repeats.${repeat || 'none'}`)}</button>
				{/each}
			</div>
		</div>

		<div>
			<span class="block text-xs font-medium text-muted-foreground mb-1.5">{$t('days.tags.field')}</span>
			<div class="flex flex-wrap gap-1.5">
				{#each tags as tag (tag.id)}
					{@const on = form.tags.includes(tag.id)}
					<button
						type="button"
						onclick={() => toggleTag(tag.id)}
						aria-pressed={on}
						class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs border transition-colors {on ? 'bg-primary/10 border-primary/40 text-foreground' : 'border-border/70 text-muted-foreground hover:text-foreground hover:bg-muted/50'}"
					>
						<span class="w-1.5 h-1.5 rounded-full" style="background: {tag.color || TAG_COLORS[6]}"></span>
						{tag.name}
						{#if on}<svg class="w-3 h-3 -mr-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" /></svg>{/if}
					</button>
				{/each}
				{#if addingTag}
					<!-- svelte-ignore a11y_autofocus -->
					<input
						class="w-28 px-2.5 py-1 rounded-full text-xs bg-background border border-ring focus:outline-none"
						bind:value={newTag}
						maxlength="30"
						placeholder={$t('days.tags.newPlaceholder')}
						autofocus
						onblur={addTag}
						onkeydown={(e) => {
							if (e.key === 'Enter') {
								e.preventDefault();
								e.currentTarget.blur();
							} else if (e.key === 'Escape') {
								e.stopPropagation();
								newTag = '';
								e.currentTarget.blur();
							}
						}}
					/>
				{:else}
					<button
						type="button"
						onclick={() => (addingTag = true)}
						class="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs border border-dashed border-border text-muted-foreground hover:text-foreground hover:border-ring/50"
					>+ {$t('days.tags.create')}</button>
				{/if}
			</div>
		</div>

		<div class="rounded-lg border border-border/60 divide-y divide-border/60">
			{#each [['include_start', 'includeStart'], ['show_on_home', 'showOnHome']] as [field, key]}
				{@const k = field as 'include_start' | 'show_on_home'}
				<label class="flex items-center justify-between gap-3 px-3 py-2.5 cursor-pointer">
					<span class="min-w-0">
						<span class="block text-sm text-foreground">{$t(`days.form.${key}`)}</span>
						<span class="block text-[11px] text-muted-foreground">{$t(`days.form.${key}Hint`)}</span>
					</span>
					<input type="checkbox" class="sr-only peer" bind:checked={form[k]} />
					<span class="relative w-9 h-5 flex-shrink-0 rounded-full bg-muted-foreground/30 transition-colors peer-checked:bg-primary after:absolute after:top-0.5 after:left-0.5 after:w-4 after:h-4 after:rounded-full after:bg-card after:shadow after:transition-transform peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring/50"></span>
				</label>
			{/each}
		</div>

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
		<button type="submit" form="day-form" disabled={saving} class="flex-1 py-2 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:opacity-90 disabled:opacity-60">
			{saving ? $t('days.form.saving') : $t('days.form.save')}
		</button>
	{/snippet}
</Sheet>
