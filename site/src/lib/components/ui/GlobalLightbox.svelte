<script lang="ts">
	import Lightbox from './Lightbox.svelte';
	import { lightbox, closeLightbox } from '$lib/stores/lightbox';
</script>

{#if $lightbox}
	<Lightbox items={$lightbox.items} index={$lightbox.index} onClose={closeLightbox} />
{/if}
