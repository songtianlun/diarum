<script lang="ts">
	import MiniDatePicker from '$lib/components/ui/MiniDatePicker.svelte';
	import EntryNav from '$lib/components/ui/EntryNav.svelte';

	let date = '2026-09-21';
	let showDatePicker = true;

	function handleDatePick(picked: string) {
		date = picked;
	}
</script>

<div style="padding-top: 4rem; max-width: 40rem; margin: 0 auto;">
	<EntryNav
		{date}
		busy={false}
		onPrevDay={() => {}}
		onNextDay={() => {}}
		onDateTextClick={() => (showDatePicker = !showDatePicker)}
		dateTextActive={showDatePicker}
		onShareMouseDown={() => {}}
		onShareClick={() => {}}
		tocActive={false}
		onTocClick={() => {}}
		isOnline={true}
		isSyncing={false}
		isDirty={false}
		onSyncClick={() => {}}
	>
		<svelte:fragment slot="datePicker">
			{#if showDatePicker}
				<MiniDatePicker value={date} onSelect={handleDatePick} onClose={() => (showDatePicker = false)} />
			{/if}
		</svelte:fragment>
	</EntryNav>
	<p style="padding: 2rem; text-align: center;">Selected: {date}</p>
</div>
