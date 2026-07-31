<script lang="ts">
	/**
	 * Everything that lives in the top-right corner of the other themes —
	 * assistant, share, sync state — relocated to the foot of the left rail as
	 * a Win95 toolbar plus a sunken status field.
	 *
	 * The status field is clickable and triggers the same manual sync as the
	 * header button elsewhere.
	 */
	import Win95Icon from './Win95Icon.svelte';
	import { t } from '$lib/i18n';

	export let isOnline: boolean;
	export let isSyncing: boolean;
	export let isDirty: boolean;
	export let onSync: () => void;
	export let onShareMouseDown: () => void;
	export let onShare: () => void;
	export let onNavigate: (href: string) => void;
	export let outlineOpen = true;
	export let onToggleOutline: (() => void) | null = null;

	let statusIcon: 'offline' | 'syncing' | 'dirty' | 'saved' = 'saved';
	$: statusIcon = !isOnline ? 'offline' : isSyncing ? 'syncing' : isDirty ? 'dirty' : 'saved';
	$: statusText = !isOnline
		? $t('win95.statusOffline')
		: isSyncing
			? $t('win95.statusSyncing')
			: isDirty
				? $t('win95.statusDirty')
				: $t('win95.statusSaved');
</script>

<div class="status-pane">
	<div class="toolbar w95-out">
		<button
			type="button"
			class="w95-btn w95-iconbtn"
			title={$t('entryNav.assistant')}
			on:click={() => onNavigate('/assistant')}
		>
			<Win95Icon name="robot" size={16} />
		</button>
		<button
			type="button"
			class="w95-btn w95-iconbtn"
			title={$t('entryNav.shareAsImage')}
			on:mousedown={onShareMouseDown}
			on:click={onShare}
		>
			<Win95Icon name="share" size={16} />
		</button>

		<span class="w95-vsep"></span>

		<button
			type="button"
			class="w95-btn w95-iconbtn"
			title={$t('nav.search')}
			on:click={() => onNavigate('/search')}
		>
			<Win95Icon name="search" size={16} />
		</button>
		<button
			type="button"
			class="w95-btn w95-iconbtn"
			title={$t('nav.mediaLibrary')}
			on:click={() => onNavigate('/media')}
		>
			<Win95Icon name="media" size={16} />
		</button>
		<button
			type="button"
			class="w95-btn w95-iconbtn"
			title={$t('nav.settings')}
			on:click={() => onNavigate('/settings')}
		>
			<Win95Icon name="settings" size={16} />
		</button>

		{#if onToggleOutline}
			<span class="w95-vsep"></span>
			<button
				type="button"
				class="w95-btn w95-iconbtn"
				class:pressed={outlineOpen}
				title={$t('win95.viewOutline')}
				on:click={onToggleOutline}
			>
				<Win95Icon name="outline" size={16} />
			</button>
		{/if}
	</div>

	<div class="w95-statusbar">
		<button type="button" class="w95-status-cell clickable grow-cell" title={statusText} on:click={onSync}>
			<span class="ico" class:spin={isSyncing}>
				<Win95Icon name={statusIcon} size={13} />
			</span>
			<span class="txt">{statusText}</span>
		</button>
		<span class="w95-status-cell" title={isOnline ? 'Online' : 'Offline'}>
			<Win95Icon name={isOnline ? 'disk' : 'offline'} size={13} />
		</span>
	</div>
</div>

<style>
	.status-pane {
		flex-shrink: 0;
	}

	.toolbar {
		display: flex;
		align-items: center;
		gap: 2px;
		padding: 2px;
	}

	.grow-cell {
		flex: 1;
		min-width: 0;
		text-align: left;
	}

	.txt {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.ico {
		display: block;
		flex-shrink: 0;
	}

	.ico.spin {
		animation: w95-spin 1.2s steps(8, end) infinite;
	}

	@keyframes w95-spin {
		to {
			transform: rotate(360deg);
		}
	}
</style>
