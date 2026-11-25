<script lang="ts">
	import { page } from '$app/stores';
	import AssistantWorkspace from '$lib/components/chat/AssistantWorkspace.svelte';
</script>

<AssistantWorkspace routeConversationId={$page.params.id ?? null} />
