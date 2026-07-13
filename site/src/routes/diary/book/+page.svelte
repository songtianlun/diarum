<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { getToday } from '$lib/utils/date';

	onMount(() => {
		goto(`/diary/book/${getToday()}`, { replaceState: true });
	});
</script>
