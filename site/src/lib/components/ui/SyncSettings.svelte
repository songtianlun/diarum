<script lang="ts">
	import { onMount } from 'svelte';
	import { initDiaryCache } from '$lib/stores/diaryCache';
	import { setAutoSaveInterval } from '$lib/stores/syncConfig';
	import { getSyncSettings, saveSyncSettings } from '$lib/api/settings';

	let saving = false;
	let autoSaveSeconds = 3;
	let hasChanges = false;

	// Track original values to detect changes
	let originalAutoSaveSeconds = 3;

	function handleAutoSaveChange() {
		setAutoSaveInterval(autoSaveSeconds * 1000);
		hasChanges = autoSaveSeconds !== originalAutoSaveSeconds;
	}

	async function handleSaveSettings() {
		saving = true;
		const success = await saveSyncSettings({
			autoSaveInterval: autoSaveSeconds * 1000
		});
		if (success) {
			originalAutoSaveSeconds = autoSaveSeconds;
			hasChanges = false;
		}
		saving = false;
	}

	async function loadSettingsFromBackend() {
		const settings = await getSyncSettings();
		autoSaveSeconds = settings.autoSaveInterval / 1000;
		originalAutoSaveSeconds = autoSaveSeconds;
		// Also update local store
		setAutoSaveInterval(settings.autoSaveInterval);
	}

	onMount(async () => {
		// initDiaryCache is idempotent, safe to call multiple times
		initDiaryCache();

		// Load settings from backend
		await loadSettingsFromBackend();
	});
</script>

<div class="space-y-4">
	<!-- Auto-save Interval -->
	<div class="py-3">
		<div class="flex items-center justify-between mb-2">
			<div>
				<div class="font-medium text-foreground">Auto-save Interval</div>
				<div class="text-sm text-muted-foreground">How often unsaved edits are synced to server</div>
			</div>
			<div class="flex items-center gap-2">
				<input
					type="number"
					bind:value={autoSaveSeconds}
					on:change={handleAutoSaveChange}
					min="1"
					max="60"
					class="w-16 px-2 py-1 text-sm bg-muted rounded-lg text-foreground text-center focus:outline-none focus:ring-2 focus:ring-primary"
				/>
				<span class="text-sm text-muted-foreground">seconds</span>
			</div>
		</div>
	</div>

	<!-- Actions -->
	<div class="pt-2">
		<button
			on:click={handleSaveSettings}
			disabled={saving || !hasChanges}
			class="px-3 sm:px-4 py-2 text-sm bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors duration-200 disabled:opacity-50 flex items-center justify-center gap-2"
		>
			{#if saving}
				<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
				</svg>
			{:else}
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
				</svg>
			{/if}
			Save
		</button>
	</div>
</div>
