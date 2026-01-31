<script lang="ts">
	import type { Message } from '$lib/api/chat';

	export let message: Message;
	export let isStreaming = false;

	function formatDate(dateStr: string): string {
		const date = new Date(dateStr);
		return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
	}
</script>

<div class="flex {message.role === 'user' ? 'justify-end' : 'justify-start'} mb-4">
	<div class="max-w-[80%] {message.role === 'user' ? 'order-2' : 'order-1'}">
		<div
			class="rounded-2xl px-4 py-3 {message.role === 'user'
				? 'bg-primary text-primary-foreground rounded-br-md'
				: 'bg-muted text-foreground rounded-bl-md'}"
		>
			<div class="whitespace-pre-wrap break-words text-sm">
				{message.content}
				{#if isStreaming}
					<span class="inline-block w-2 h-4 bg-current animate-pulse ml-0.5"></span>
				{/if}
			</div>
		</div>

		<div class="flex items-center gap-2 mt-1 px-1 {message.role === 'user' ? 'justify-end' : 'justify-start'}">
			{#if message.created}
				<span class="text-xs text-muted-foreground">{formatDate(message.created)}</span>
			{/if}

			{#if message.referenced_diaries && message.referenced_diaries.length > 0}
				<span class="text-xs text-muted-foreground">
					Referenced {message.referenced_diaries.length} {message.referenced_diaries.length === 1 ? 'diary' : 'diaries'}
				</span>
			{/if}
		</div>
	</div>
</div>
