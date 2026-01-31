<script lang="ts">
	import { createEventDispatcher } from 'svelte';
	import type { Conversation } from '$lib/api/chat';

	export let conversations: Conversation[] = [];
	export let selectedId: string | null = null;
	export let loading = false;

	const dispatch = createEventDispatcher<{
		select: string;
		create: void;
		delete: string;
	}>();

	function formatDate(dateStr: string): string {
		const date = new Date(dateStr);
		const now = new Date();
		const diff = now.getTime() - date.getTime();
		const days = Math.floor(diff / (1000 * 60 * 60 * 24));

		if (days === 0) return 'Today';
		if (days === 1) return 'Yesterday';
		if (days < 7) return `${days} days ago`;
		return date.toLocaleDateString();
	}

	function getTitle(conv: Conversation): string {
		return conv.title || 'New conversation';
	}
</script>

<div class="flex flex-col h-full">
	<div class="p-3 border-b border-border">
		<button
			on:click={() => dispatch('create')}
			class="w-full flex items-center justify-center gap-2 px-4 py-2
				bg-primary text-primary-foreground rounded-lg
				hover:opacity-90 transition-opacity text-sm font-medium"
		>
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
			</svg>
			New Chat
		</button>
	</div>

	<div class="flex-1 overflow-y-auto">
		{#if loading}
			<div class="flex items-center justify-center py-8">
				<svg class="w-5 h-5 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
				</svg>
			</div>
		{:else if conversations.length === 0}
			<div class="text-center py-8 px-4 text-muted-foreground text-sm">
				No conversations yet
			</div>
		{:else}
			<div class="p-2 space-y-1">
				{#each conversations as conv (conv.id)}
					<div
						role="button"
						tabindex="0"
						on:click={() => dispatch('select', conv.id)}
						on:keydown={(e) => e.key === 'Enter' && dispatch('select', conv.id)}
						class="w-full text-left p-3 rounded-lg transition-colors group cursor-pointer
							{selectedId === conv.id ? 'bg-muted' : 'hover:bg-muted/50'}"
					>
						<div class="flex items-start justify-between gap-2">
							<div class="flex-1 min-w-0">
								<div class="text-sm font-medium truncate">{getTitle(conv)}</div>
								<div class="text-xs text-muted-foreground mt-0.5">
									{formatDate(conv.updated)}
								</div>
							</div>
							<button
								on:click|stopPropagation={() => dispatch('delete', conv.id)}
								class="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-destructive/10 transition-all"
								title="Delete"
							>
								<svg class="w-4 h-4 text-destructive" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
										d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
								</svg>
							</button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>
