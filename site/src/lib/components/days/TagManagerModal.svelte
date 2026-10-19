<script lang="ts">
	import Sheet from './Sheet.svelte';
	import { createDayTag, updateDayTag, deleteDayTag, type DayTag } from '$lib/api/days';
	import { TAG_COLORS, nextTagColor } from '$lib/utils/days';
	import { t } from '$lib/i18n';

	/** Add, rename, recolour and delete tags; every change saves at once. */
	let {
		tags = $bindable(),
		onclose,
		ondeleted
	}: { tags: DayTag[]; onclose: () => void; ondeleted?: (id: string) => void } = $props();

	let newName = $state('');
	let error = $state('');
	let busy = $state(false);
	// The tag whose colour picker is open.
	let picking = $state<string | null>(null);

	function message(e: unknown): string {
		const err = e as { status?: number; message?: string };
		return err.status === 409 ? $t('days.tags.exists') : (err.message ?? String(e));
	}

	async function add(event?: SubmitEvent) {
		event?.preventDefault();
		const name = newName.trim();
		if (!name || busy) return;
		busy = true;
		error = '';
		try {
			const tag = await createDayTag({ name, color: nextTagColor(tags.map((x) => x.color)) });
			tags = [...tags, tag];
			newName = '';
		} catch (e) {
			error = message(e);
		} finally {
			busy = false;
		}
	}

	async function save(tag: DayTag, patch: Partial<Pick<DayTag, 'name' | 'color'>>) {
		const next = { name: (patch.name ?? tag.name).trim(), color: patch.color ?? tag.color };
		if (!next.name || (next.name === tag.name && next.color === tag.color)) return false;
		error = '';
		try {
			const saved = await updateDayTag(tag.id, next);
			tags = tags.map((x) => (x.id === saved.id ? saved : x));
			return true;
		} catch (e) {
			error = message(e);
			return false;
		}
	}

	async function rename(tag: DayTag, input: HTMLInputElement) {
		if (!(await save(tag, { name: input.value }))) input.value = tag.name;
	}

	async function remove(tag: DayTag) {
		if (!confirm($t('days.tags.deleteConfirm', { name: tag.name }))) return;
		error = '';
		try {
			await deleteDayTag(tag.id);
			tags = tags.filter((x) => x.id !== tag.id);
			ondeleted?.(tag.id);
		} catch (e) {
			error = message(e);
		}
	}
</script>

<Sheet title={$t('days.tags.title')} {onclose}>
	<form class="flex gap-2" onsubmit={add}>
		<input
			class="flex-1 min-w-0 px-3 py-2 rounded-lg bg-background border border-input text-sm focus:outline-none focus:ring-2 focus:ring-ring/40"
			bind:value={newName}
			maxlength="30"
			placeholder={$t('days.tags.newPlaceholder')}
		/>
		<button type="submit" disabled={!newName.trim() || busy} class="px-3.5 rounded-lg bg-primary text-primary-foreground text-sm font-medium disabled:opacity-50">
			{$t('days.tags.add')}
		</button>
	</form>

	{#if error}
		<p class="mt-2 text-xs text-destructive">{error}</p>
	{/if}

	{#if tags.length === 0}
		<p class="py-8 text-center text-sm text-muted-foreground">{$t('days.tags.empty')}</p>
	{:else}
		<ul class="mt-4 divide-y divide-border/60 rounded-lg border border-border/60">
			{#each tags as tag (tag.id)}
				<li class="px-3 py-2">
					<div class="flex items-center gap-2.5">
						<button
							type="button"
							class="w-5 h-5 flex-shrink-0 rounded-full ring-offset-2 ring-offset-card transition-shadow hover:ring-2 hover:ring-border {picking === tag.id ? 'ring-2 ring-ring/50' : ''}"
							style="background: {tag.color || TAG_COLORS[6]}"
							title={$t('days.tags.color')}
							aria-label={$t('days.tags.color')}
							aria-expanded={picking === tag.id}
							onclick={() => (picking = picking === tag.id ? null : tag.id)}
						></button>
						<input
							class="flex-1 min-w-0 px-1.5 py-1 -mx-1.5 rounded bg-transparent text-sm text-foreground border border-transparent hover:border-border focus:border-ring focus:outline-none"
							value={tag.name}
							maxlength="30"
							onblur={(e) => rename(tag, e.currentTarget)}
							onkeydown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
							aria-label={$t('days.form.name')}
						/>
						<span class="text-[11px] text-muted-foreground tabular-nums flex-shrink-0">{$t('days.tags.dayCount', { n: tag.days })}</span>
						<button
							type="button"
							class="p-1.5 -mr-1.5 rounded-lg text-muted-foreground hover:text-destructive hover:bg-destructive/10 flex-shrink-0"
							title={$t('days.delete')}
							aria-label={$t('days.delete')}
							onclick={() => remove(tag)}
						>
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6M9 7V4a1 1 0 011-1h4a1 1 0 011 1v3M4 7h16" /></svg>
						</button>
					</div>
					{#if picking === tag.id}
						<div class="flex flex-wrap gap-2 pt-2.5 pb-1 pl-[1.875rem] animate-fade-in-only">
							{#each TAG_COLORS as color}
								<button
									type="button"
									class="w-6 h-6 rounded-full ring-offset-2 ring-offset-card transition-shadow {tag.color === color ? 'ring-2 ring-foreground/40' : 'hover:ring-2 hover:ring-border'}"
									style="background: {color}"
									aria-label={color}
									onclick={async () => {
										await save(tag, { color });
										picking = null;
									}}
								></button>
							{/each}
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}

	{#snippet footer()}
		<button type="button" onclick={onclose} class="flex-1 py-2 rounded-lg border border-border/60 text-sm hover:bg-muted/50">{$t('days.tags.done')}</button>
	{/snippet}
</Sheet>
