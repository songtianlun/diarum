<script lang="ts">
	/**
	 * The entry-nav date switcher, rebuilt as a Win95 DateTimePicker:
	 * a sunken edit field with a drop-down button that reveals a month grid,
	 * flanked by prev/next spin arrows.
	 *
	 * Same three actions as the other themes' top nav — previous day, next day
	 * (never past today), open the calendar.
	 */
	import Win95Icon from './Win95Icon.svelte';
	import Win95Calendar from './Win95Calendar.svelte';
	import type { CalendarDiaryMeta } from '$lib/api/diaries';
	import { getDayOfWeek, isToday } from '$lib/utils/date';
	import { t } from '$lib/i18n';

	export let date: string;
	export let entries: CalendarDiaryMeta[] = [];
	export let busy = false;
	export let onPrev: () => void;
	export let onNext: () => void;
	export let onPick: (date: string) => void;
	/** Drops the pane caption (Windows CE layout). */
	export let compact = false;
	/** Anchors the drop-down above the field instead of below it. */
	export let dropUp = false;

	let dropped = false;

	$: atToday = isToday(date);

	function choose(next: string) {
		dropped = false;
		if (next !== date) onPick(next);
	}

	function onWindowKey(event: KeyboardEvent) {
		if (event.key === 'Escape' && dropped) {
			dropped = false;
		}
	}
</script>

<svelte:window on:keydown={onWindowKey} />

<div class="nav-pane" class:compact>
	{#if !compact}
		<div class="w95-pane-caption">
			<Win95Icon name="calendar" size={13} />
			<span>{$t('win95.calendar')}</span>
		</div>
	{/if}

	<div class="nav-row">
		<button
			type="button"
			class="w95-btn w95-iconbtn"
			title={$t('win95.prevDay')}
			disabled={busy}
			on:click={onPrev}
		>
			<Win95Icon name="left" size={16} />
		</button>

		<div class="picker">
			<button
				type="button"
				class="w95-field date-field"
				aria-expanded={dropped}
				on:click={() => (dropped = !dropped)}
			>
				<span class="d">{date}</span>
				<span class="wd">{getDayOfWeek(date)}</span>
				{#if atToday}<span class="tag">{$t('common.today')}</span>{/if}
			</button>
			<button
				type="button"
				class="w95-btn drop"
				class:pressed={dropped}
				aria-label={$t('win95.calendarDialogTitle')}
				on:click={() => (dropped = !dropped)}
			>
				<svg width="7" height="4" viewBox="0 0 7 4" shape-rendering="crispEdges">
					<path d="M0 0h7v1H6v1H5v1H4v1H3V3H2V2H1V1H0z" fill="currentColor" />
				</svg>
			</button>

			{#if dropped}
				<button class="scrim" tabindex="-1" aria-label={$t('win95.close')} on:click={() => (dropped = false)}
				></button>
				<div class="drop-panel" class:up={dropUp}>
					<Win95Calendar value={date} {entries} onSelect={choose} />
				</div>
			{/if}
		</div>

		<button
			type="button"
			class="w95-btn w95-iconbtn"
			title={atToday ? $t('win95.notToday') : $t('win95.nextDay')}
			disabled={busy || atToday}
			on:click={onNext}
		>
			<Win95Icon name="right" size={16} />
		</button>
	</div>
</div>

<style>
	.nav-pane {
		position: relative;
		flex-shrink: 0;
		padding-bottom: 2px;
	}

	.nav-row {
		display: flex;
		align-items: stretch;
		gap: 3px;
	}

	.picker {
		position: relative;
		display: flex;
		flex: 1;
		min-width: 0;
	}

	.date-field {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 5px;
		padding: 3px 5px;
		font-family: inherit;
		font-size: 12px;
		color: #000;
		text-align: left;
		overflow: hidden;
	}

	.date-field .d {
		font-variant-numeric: tabular-nums;
		letter-spacing: 0.02em;
	}

	.date-field .wd {
		color: #404040;
		font-size: 11px;
	}

	.date-field .tag {
		margin-left: auto;
		flex-shrink: 0;
		padding: 0 4px;
		background: #000080;
		color: #fff;
		font-size: 10px;
		line-height: 14px;
	}

	.drop {
		width: 17px;
		min-height: 0;
		padding: 0;
		margin-left: 2px;
		flex-shrink: 0;
		color: #000;
	}
	/* Out-specifies the shared `.w95-btn.pressed` text padding. */
	.picker .drop:active,
	.picker .drop.pressed {
		padding: 1px 0 0 1px;
	}

	/* Click-away layer; the dropdown itself sits above it. */
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 49;
		background: transparent;
		cursor: default;
	}

	.drop-panel {
		position: absolute;
		z-index: 50;
		top: calc(100% + 2px);
		left: 0;
		box-shadow: 2px 2px 0 0 rgba(0, 0, 0, 0.35);
	}

	.drop-panel.up {
		top: auto;
		bottom: calc(100% + 2px);
	}

	.nav-pane.compact {
		padding-bottom: 0;
		flex: 1;
		min-width: 0;
	}
</style>
