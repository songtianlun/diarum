<script lang="ts">
	import '../app.css';
	import { onMount, type Snippet } from 'svelte';
	import { goto } from '$app/navigation';
	import { installUnauthorizedApiHandler } from '$lib/api/client';
	import { initTheme } from '$lib/stores/theme';

	let { children }: { children: Snippet } = $props();

	onMount(() => {
		initTheme();
		return installUnauthorizedApiHandler(() => {
			if (window.location.pathname !== '/login') {
				goto('/login');
			}
		});
	});
</script>

{@render children()}
