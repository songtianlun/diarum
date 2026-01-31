<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { isAuthenticated } from '$lib/api/client';
	import { getAISettings } from '$lib/api/ai';
	import {
		getConversations,
		createConversation,
		getConversation,
		deleteConversation,
		streamChat,
		type Conversation,
		type Message
	} from '$lib/api/chat';
	import ChatMessage from '$lib/components/chat/ChatMessage.svelte';
	import ChatInput from '$lib/components/chat/ChatInput.svelte';
	import ConversationList from '$lib/components/chat/ConversationList.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';

	let conversations: Conversation[] = [];
	let selectedConversationId: string | null = null;
	let messages: Message[] = [];
	let streamingContent = '';
	let isStreaming = false;
	let loading = true;
	let messagesLoading = false;
	let aiEnabled = false;
	let sidebarOpen = true;
	let messagesContainer: HTMLDivElement;

	async function loadConversations() {
		try {
			conversations = await getConversations();
		} catch (e) {
			console.error('Failed to load conversations:', e);
		}
	}

	async function loadMessages(convId: string) {
		messagesLoading = true;
		try {
			const detail = await getConversation(convId);
			messages = detail.messages;
			scrollToBottom();
		} catch (e) {
			console.error('Failed to load messages:', e);
		}
		messagesLoading = false;
	}

	function scrollToBottom() {
		setTimeout(() => {
			if (messagesContainer) {
				messagesContainer.scrollTop = messagesContainer.scrollHeight;
			}
		}, 50);
	}

	async function handleCreateConversation() {
		try {
			const conv = await createConversation();
			conversations = [conv, ...conversations];
			selectedConversationId = conv.id;
			messages = [];
		} catch (e) {
			console.error('Failed to create conversation:', e);
		}
	}

	async function handleSelectConversation(convId: string) {
		selectedConversationId = convId;
		await loadMessages(convId);
	}

	async function handleDeleteConversation(convId: string) {
		if (!confirm('Delete this conversation?')) return;
		try {
			await deleteConversation(convId);
			conversations = conversations.filter(c => c.id !== convId);
			if (selectedConversationId === convId) {
				selectedConversationId = null;
				messages = [];
			}
		} catch (e) {
			console.error('Failed to delete conversation:', e);
		}
	}

	async function handleSendMessage(content: string) {
		if (!selectedConversationId || isStreaming) return;

		// Add user message
		const userMsg: Message = {
			id: 'temp-user',
			role: 'user',
			content,
			created: new Date().toISOString()
		};
		messages = [...messages, userMsg];
		scrollToBottom();

		isStreaming = true;
		streamingContent = '';

		try {
			for await (const chunk of streamChat(selectedConversationId, content)) {
				if (chunk.error) {
					console.error('Stream error:', chunk.error);
					break;
				}
				if (chunk.content) {
					streamingContent += chunk.content;
					scrollToBottom();
				}
				if (chunk.done) {
					// Add assistant message
					const assistantMsg: Message = {
						id: 'temp-assistant',
						role: 'assistant',
						content: streamingContent,
						referenced_diaries: chunk.referenced_diaries,
						created: new Date().toISOString()
					};
					messages = [...messages, assistantMsg];
					streamingContent = '';
				}
			}
		} catch (e) {
			console.error('Failed to send message:', e);
		}

		isStreaming = false;
		await loadConversations();
	}

	onMount(async () => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}

		const settings = await getAISettings();
		aiEnabled = settings.enabled;

		if (!aiEnabled) {
			loading = false;
			return;
		}

		await loadConversations();
		loading = false;
	});
</script>

<svelte:head>
	<title>AI Assistant - Diaria</title>
</svelte:head>

<div class="min-h-screen bg-background flex flex-col">
	<!-- Header -->
	<header class="glass border-b border-border/50 sticky top-0 z-20">
		<div class="max-w-6xl mx-auto px-4 h-11">
			<div class="flex items-center justify-between h-full">
				<div class="flex items-center gap-3">
					<button
						on:click={() => sidebarOpen = !sidebarOpen}
						class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 lg:hidden"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
						</svg>
					</button>
					<a href="/diary" class="text-lg font-semibold text-foreground hover:text-primary transition-colors">Diaria</a>
					<span class="text-muted-foreground">/</span>
					<span class="text-sm text-muted-foreground">AI Assistant</span>
				</div>
				<a href="/diary" class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200" title="Back to Diary">
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
					</svg>
				</a>
			</div>
		</div>
	</header>

	<!-- Main Content -->
	{#if loading}
		<div class="flex-1 flex items-center justify-center">
			<svg class="w-8 h-8 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
				<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
				<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
			</svg>
		</div>
	{:else if !aiEnabled}
		<div class="flex-1 flex items-center justify-center p-4">
			<div class="text-center max-w-md">
				<svg class="w-16 h-16 mx-auto text-muted-foreground mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
						d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
				</svg>
				<h2 class="text-xl font-semibold mb-2">AI Features Not Enabled</h2>
				<p class="text-muted-foreground mb-4">
					Enable AI features in settings to use the AI assistant.
				</p>
				<a href="/settings" class="inline-flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:opacity-90">
					Go to Settings
				</a>
			</div>
		</div>
	{:else}
		<div class="flex-1 flex overflow-hidden">
			<!-- Sidebar -->
			<aside class="w-64 border-r border-border bg-card flex-shrink-0
				{sidebarOpen ? 'block' : 'hidden'} lg:block">
				<ConversationList
					{conversations}
					selectedId={selectedConversationId}
					{loading}
					on:select={(e) => handleSelectConversation(e.detail)}
					on:create={handleCreateConversation}
					on:delete={(e) => handleDeleteConversation(e.detail)}
				/>
			</aside>

			<!-- Chat Area -->
			<main class="flex-1 flex flex-col min-w-0">
				{#if !selectedConversationId}
					<div class="flex-1 flex items-center justify-center p-4">
						<div class="text-center">
							<svg class="w-16 h-16 mx-auto text-muted-foreground mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
									d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
							</svg>
							<h2 class="text-xl font-semibold mb-2">Start a Conversation</h2>
							<p class="text-muted-foreground mb-4">
								Select a conversation or create a new one to chat with your diary.
							</p>
							<button
								on:click={handleCreateConversation}
								class="inline-flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:opacity-90"
							>
								<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
								</svg>
								New Chat
							</button>
						</div>
					</div>
				{:else}
					<!-- Messages -->
					<div bind:this={messagesContainer} class="flex-1 overflow-y-auto p-4">
						{#if messagesLoading}
							<div class="flex justify-center py-8">
								<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
								</svg>
							</div>
						{:else if messages.length === 0 && !streamingContent}
							<div class="text-center text-muted-foreground py-8">
								Start the conversation by sending a message.
							</div>
						{:else}
							{#each messages as message (message.id)}
								<ChatMessage {message} />
							{/each}
							{#if streamingContent}
								<ChatMessage
									message={{ id: 'streaming', role: 'assistant', content: streamingContent, created: '' }}
									isStreaming={true}
								/>
							{/if}
						{/if}
					</div>

					<!-- Input -->
					<div class="border-t border-border p-4 bg-card">
						<ChatInput
							disabled={isStreaming}
							placeholder="Ask about your diary..."
							on:send={(e) => handleSendMessage(e.detail)}
						/>
					</div>
				{/if}
			</main>
		</div>
	{/if}
</div>
