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

	let deleteConfirmId: string | null = null;

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
		const title = conv.title || 'New conversation';
		// Limit title length
		return title.length > 28 ? title.slice(0, 28) + '...' : title;
	}

	function handleDelete(e: Event, convId: string) {
		e.stopPropagation();
		if (deleteConfirmId === convId) {
			dispatch('delete', convId);
			deleteConfirmId = null;
		} else {
			deleteConfirmId = convId;
			// Auto reset after 3 seconds
			setTimeout(() => {
				if (deleteConfirmId === convId) {
					deleteConfirmId = null;
				}
			}, 3000);
		}
	}

	function cancelDelete(e: Event) {
		e.stopPropagation();
		deleteConfirmId = null;
	}
</script>

<div class="flex flex-col h-full">
	<!-- Header with title and New Chat button -->
	<div class="p-4 border-b border-border/50 flex-shrink-0">
		<div class="flex items-center justify-between mb-3">
			<h2 class="text-sm font-semibold text-foreground">Conversations</h2>
			<span class="text-xs text-muted-foreground bg-muted/50 px-2 py-0.5 rounded-full">
				{conversations.length}
			</span>
		</div>
		<button
			on:click={() => dispatch('create')}
			class="w-full flex items-center justify-center gap-2 px-4 py-2.5
				bg-primary text-primary-foreground rounded-xl
				hover:opacity-90 transition-all text-sm font-medium shadow-sm"
		>
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
			</svg>
			New Chat
		</button>
	</div>

	<!-- Conversation List -->
	<div class="flex-1 overflow-y-auto">
		{#if loading}
			<div class="flex items-center justify-center py-12">
				<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
				</svg>
			</div>
		{:else if conversations.length === 0}
			<div class="text-center py-12 px-4">
				<div class="w-12 h-12 mx-auto mb-3 rounded-xl bg-muted/50 flex items-center justify-center">
					<svg class="w-6 h-6 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
							d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
					</svg>
				</div>
				<p class="text-muted-foreground text-sm">No conversations yet</p>
			</div>
		{:else}
			<div class="p-2 space-y-1">
				{#each conversations as conv (conv.id)}
					<div
						role="button"
						tabindex="0"
						on:click={() => dispatch('select', conv.id)}
						on:keydown={(e) => e.key === 'Enter' && dispatch('select', conv.id)}
						class="w-full text-left p-3 rounded-xl transition-all duration-200 group cursor-pointer relative
							{selectedId === conv.id
								? 'bg-primary/10 border border-primary/20'
								: 'hover:bg-muted/50 border border-transparent'}"
					>
						<div class="flex items-start justify-between gap-2">
							<div class="flex-1 min-w-0">
								<div class="text-sm font-medium truncate max-w-[180px]">{getTitle(conv)}</div>
								<div class="text-xs text-muted-foreground mt-1 flex items-center gap-2">
									<span class="flex items-center gap-1">
										<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
												d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
										</svg>
										{formatDate(conv.updated)}
									</span>
									<span class="flex items-center gap-1">
										<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
												d="M8 10h.01M12 10h.01M16 10h.01M9 16H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-5l-5 5v-5z" />
										</svg>
										{conv.message_count ?? 0}
									</span>
								</div>
							</div>

							<!-- Delete Button -->
							{#if deleteConfirmId === conv.id}
								<div class="flex items-center gap-1">
									<button
										on:click={(e) => handleDelete(e, conv.id)}
										class="p-1.5 rounded-lg bg-destructive text-destructive-foreground text-xs font-medium hover:opacity-90 transition-all"
										title="Confirm delete"
									>
										<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
										</svg>
									</button>
									<button
										on:click={cancelDelete}
										class="p-1.5 rounded-lg bg-muted hover:bg-muted/80 transition-all"
										title="Cancel"
									>
										<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
										</svg>
									</button>
								</div>
							{:else}
								<button
									on:click={(e) => handleDelete(e, conv.id)}
									class="p-1.5 rounded-lg opacity-0 group-hover:opacity-100 hover:bg-destructive/10 transition-all"
									title="Delete"
								>
									<svg class="w-4 h-4 text-destructive" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
											d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
									</svg>
								</button>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>
