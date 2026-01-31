<script lang="ts">
	import type { Message } from '$lib/api/chat';
	import type { Diary } from '$lib/api/client';
	import { getDiariesByIds } from '$lib/api/diaries';
	import { goto } from '$app/navigation';

	export let message: Message;
	export let isStreaming = false;

	let expanded = false;
	let diaries: Diary[] = [];
	let loading = false;
	let loaded = false;

	function formatDate(dateStr: string): string {
		const date = new Date(dateStr);
		return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
	}

	function formatDiaryDate(dateStr: string): string {
		const date = new Date(dateStr);
		return date.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' });
	}

	function truncateContent(content: string, maxLength: number = 80): string {
		if (content.length <= maxLength) return content;
		return content.substring(0, maxLength) + '...';
	}

	async function toggleExpanded() {
		expanded = !expanded;
		if (expanded && !loaded && message.referenced_diaries) {
			loading = true;
			try {
				diaries = await getDiariesByIds(message.referenced_diaries);
				loaded = true;
			} catch (error) {
				console.error('Failed to load diaries:', error);
			} finally {
				loading = false;
			}
		}
	}

	function navigateToDiary(date: string) {
		goto(`/diary/${date}`);
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
				<button
					type="button"
					class="text-xs text-muted-foreground hover:text-foreground transition-colors flex items-center gap-1"
					on:click={toggleExpanded}
				>
					<svg
						class="w-3 h-3 transition-transform {expanded ? 'rotate-90' : ''}"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
					>
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
					</svg>
					Referenced {message.referenced_diaries.length} {message.referenced_diaries.length === 1 ? 'diary' : 'diaries'}
				</button>
			{/if}
		</div>

		{#if expanded && message.referenced_diaries && message.referenced_diaries.length > 0}
			<div class="mt-2 px-1 space-y-2">
				{#if loading}
					<div class="text-xs text-muted-foreground">Loading...</div>
				{:else}
					{#each diaries as diary}
						<button
							type="button"
							class="w-full text-left p-2 rounded-lg bg-background/50 border border-border/50 hover:border-border hover:bg-background transition-colors"
							on:click={() => navigateToDiary(diary.date)}
						>
							<div class="text-xs font-medium text-foreground">{formatDiaryDate(diary.date)}</div>
							<div class="text-xs text-muted-foreground mt-1 line-clamp-2">
								{truncateContent(diary.content)}
							</div>
						</button>
					{/each}
				{/if}
			</div>
		{/if}
	</div>
</div>
