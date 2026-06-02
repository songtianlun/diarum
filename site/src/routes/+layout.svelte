<script lang="ts">
	import '../app.css';
	import { onMount, type Snippet } from 'svelte';
	import { initTheme } from '$lib/stores/theme';

	let { children }: { children: Snippet } = $props();

	onMount(() => {
		initTheme();
	});
</script>

{@render children()}
