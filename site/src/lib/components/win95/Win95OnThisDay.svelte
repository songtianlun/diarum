<script lang="ts">
	/**
	 * "On this day" as an Explorer-style rail pane: a caption strip over a
	 * sunken list, matching the contents tree and outline it sits beside.
	 *
	 * On CE (narrow) the same list is reused inside a taskbar sheet, where the
	 * dialog's own title bar already names it — pass showCaption={false} there.
	 *
	 * Entries are fetched by the parent view (which owns the date) and handed
	 * down, so navigating between days never triggers a second request here.
	 */
	import Win95Icon from './Win95Icon.svelte';
	import type { OnThisDayEntry } from '$lib/api/diaries';
	import { formatDisplayDate } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	export let entries: OnThisDayEntry[] = [];
	export let onOpen: (date: string) => void;
	export let showCaption = true;

	function yearsAgoLabel(years: number): string {
		const key = years === 1 ? 'diaryOverview.yearsAgoSingular' : 'diaryOverview.yearsAgoPlural';
		return $t(key, { n: years });
	}
</script>

<div class="otd-pane">
	{#if showCaption}
		<div class="w95-pane-caption">
			<Win95Icon name="page" size={13} />
			<span>{$t('diaryOverview.onThisDay')}</span>
		</div>
	{/if}

	<div class="w95-field otd-body">
		{#if entries.length === 0}
			<div class="w95-tree-empty">{$t('diaryOverview.onThisDayEmptyWin95')}</div>
		{:else}
			<div class="otd-list">
				{#each entries as entry (entry.date)}
					<button
						type="button"
						class="otd-item"
						title={entry.preview}
						on:click={() => onOpen(entry.date)}
					>
						<span class="otd-head">
							<Win95Icon name="page" size={14} />
							<span class="otd-years">{yearsAgoLabel(entry.yearsAgo)}</span>
							{#if entry.weather}<span class="otd-emo">{entry.weather}</span>{/if}
							{#if entry.mood}<span class="otd-emo">{entry.mood}</span>{/if}
						</span>
						<span class="otd-date">{formatDisplayDate(entry.date)}</span>
						<span class="otd-preview">{entry.preview}</span>
					</button>
				{/each}
			</div>
		{/if}
	</div>
</div>

<style>
	.otd-pane {
		display: flex;
		flex-direction: column;
		min-height: 78px;
		flex: 2 1 0;
	}

	.otd-body {
		flex: 1 1 auto;
		min-height: 56px;
		overflow: auto;
	}

	.otd-list {
		padding: 2px;
	}

	.otd-item {
		display: block;
		width: 100%;
		padding: 3px 4px;
		text-align: left;
		background: transparent;
		border: none;
		font: inherit;
		color: inherit;
	}

	/* The era's list boxes highlighted the whole row on selection. */
	.otd-item:hover {
		background: var(--w95-select);
		color: var(--w95-select-text);
	}

	.otd-head {
		display: flex;
		align-items: center;
		gap: 4px;
	}

	.otd-years {
		font-weight: bold;
	}

	.otd-emo {
		margin-left: auto;
	}

	.otd-date {
		display: block;
		padding-left: 18px;
		opacity: 0.75;
	}

	.otd-preview {
		display: block;
		padding-left: 18px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		opacity: 0.75;
	}

	.otd-item:hover .otd-date,
	.otd-item:hover .otd-preview {
		opacity: 1;
	}
</style>
