<script lang="ts">
	import '../app.css';
	import { onMount, type Snippet } from 'svelte';
	import { goto } from '$app/navigation';
	import { installUnauthorizedApiHandler } from '$lib/api/client';
	import { initTheme } from '$lib/stores/theme';
	import { initLocale } from '$lib/i18n';

	let { children }: { children: Snippet } = $props();

	onMount(() => {
		initTheme();
		initLocale();
		return installUnauthorizedApiHandler(() => {
			if (window.location.pathname !== '/login') {
				goto('/login');
			}
		});
	});
</script>

{@render children()}
