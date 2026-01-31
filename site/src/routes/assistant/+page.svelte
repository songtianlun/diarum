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
	let sidebarOpen = false;
	let messagesContainer: HTMLDivElement;
	// New state: true when user wants to start a new chat but hasn't sent a message yet
	let isNewChatMode = false;

	function closeSidebarOnMobile() {
		if (window.innerWidth < 1024) {
			sidebarOpen = false;
		}
	}

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

	function handleStartNewChat() {
		// Enter new chat mode without creating a conversation yet
		selectedConversationId = null;
		isNewChatMode = true;
		messages = [];
		closeSidebarOnMobile();
	}

	async function handleSelectConversation(convId: string) {
		isNewChatMode = false;
		selectedConversationId = convId;
		closeSidebarOnMobile();
		await loadMessages(convId);
	}

	async function handleDeleteConversation(convId: string) {
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
		if (isStreaming) return;

		let convId = selectedConversationId;

		// If in new chat mode, create conversation first
		if (isNewChatMode || !convId) {
			try {
				const conv = await createConversation();
				conversations = [conv, ...conversations];
				convId = conv.id;
				selectedConversationId = conv.id;
				isNewChatMode = false;
			} catch (e) {
				console.error('Failed to create conversation:', e);
				return;
			}
		}

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
			for await (const chunk of streamChat(convId, content)) {
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

					// Update conversation title if returned
					if (chunk.title && convId) {
						conversations = conversations.map(c =>
							c.id === convId
								? { ...c, title: chunk.title! }
								: c
						);
					}
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

		// Always start in new chat mode, ready to send messages
		isNewChatMode = true;

		loading = false;
	});
</script>

<svelte:head>
	<title>AI Assistant - Journitalia</title>
</svelte:head>

<div class="h-screen bg-background flex flex-col overflow-hidden">
	<!-- Header -->
	<header class="glass border-b border-border/50 flex-shrink-0 z-30">
		<div class="max-w-7xl mx-auto px-4 lg:px-6 h-14">
			<div class="flex items-center justify-between h-full">
				<div class="flex items-center gap-3">
					<button
						on:click={() => sidebarOpen = !sidebarOpen}
						class="p-2 hover:bg-muted/50 rounded-lg transition-all duration-200 lg:hidden"
						aria-label="Toggle sidebar"
					>
						<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
						</svg>
					</button>
					<a href="/diary" class="text-lg font-semibold text-foreground hover:text-primary transition-colors">Journitalia</a>
					<span class="text-muted-foreground/50">/</span>
					<span class="text-sm font-medium text-muted-foreground">AI Assistant</span>
				</div>
				<a href="/diary" class="p-2 hover:bg-muted/50 rounded-lg transition-all duration-200" title="Back to Diary">
					<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
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
		<div class="flex-1 flex overflow-hidden relative lg:p-4 lg:gap-4">
			<!-- Mobile Overlay -->
			{#if sidebarOpen}
				<button
					class="fixed inset-0 bg-black/50 z-30 lg:hidden"
					on:click={() => sidebarOpen = false}
					aria-label="Close sidebar"
				></button>
			{/if}

			<!-- Sidebar -->
			<aside class="fixed lg:relative inset-y-0 left-0 z-40 w-72 lg:w-72
				bg-card lg:bg-card/50 border-r lg:border border-border lg:rounded-2xl flex-shrink-0
				transform transition-transform duration-300 ease-in-out
				{sidebarOpen ? 'translate-x-0' : '-translate-x-full'} lg:translate-x-0
				top-14 lg:top-0 h-[calc(100vh-3.5rem)] lg:h-full overflow-hidden">
				<ConversationList
					{conversations}
					selectedId={selectedConversationId}
					{loading}
					on:select={(e) => handleSelectConversation(e.detail)}
					on:create={handleStartNewChat}
					on:delete={(e) => handleDeleteConversation(e.detail)}
				/>
			</aside>

			<!-- Chat Area -->
			<main class="flex-1 flex flex-col min-w-0 lg:bg-card/50 lg:border lg:border-border lg:rounded-2xl overflow-hidden">
				{#if !selectedConversationId && !isNewChatMode}
					<div class="flex-1 flex items-center justify-center p-6">
						<div class="text-center max-w-sm">
							<div class="w-20 h-20 mx-auto mb-6 rounded-2xl bg-gradient-to-br from-primary/20 to-primary/5 flex items-center justify-center">
								<svg class="w-10 h-10 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
										d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
								</svg>
							</div>
							<h2 class="text-xl font-semibold mb-2">Start a Conversation</h2>
							<p class="text-muted-foreground mb-6 text-sm leading-relaxed">
								Chat with your diary using AI. Ask questions about your past entries or get insights.
							</p>
							<button
								on:click={handleStartNewChat}
								class="inline-flex items-center gap-2 px-5 py-2.5 bg-primary text-primary-foreground rounded-xl hover:opacity-90 transition-all shadow-sm font-medium"
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
					<div bind:this={messagesContainer} class="flex-1 overflow-y-auto p-4 lg:p-6">
						{#if messagesLoading}
							<div class="flex justify-center py-12">
								<div class="flex flex-col items-center gap-3">
									<svg class="w-8 h-8 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
										<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
										<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
									</svg>
									<span class="text-sm text-muted-foreground">Loading messages...</span>
								</div>
							</div>
						{:else if messages.length === 0 && !streamingContent}
							<div class="flex flex-col items-center justify-center h-full text-center py-12">
								<div class="w-16 h-16 mb-4 rounded-xl bg-muted/50 flex items-center justify-center">
									<svg class="w-8 h-8 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
											d="M7 8h10M7 12h4m1 8l-4-4H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-3l-4 4z" />
									</svg>
								</div>
								<p class="text-muted-foreground text-sm">
									Start the conversation by sending a message below.
								</p>
							</div>
						{:else}
							<div class="max-w-3xl mx-auto space-y-4">
								{#each messages as message (message.id)}
									<ChatMessage {message} />
								{/each}
								{#if streamingContent}
									<ChatMessage
										message={{ id: 'streaming', role: 'assistant', content: streamingContent, created: '' }}
										isStreaming={true}
									/>
								{/if}
							</div>
						{/if}
					</div>

					<!-- Input -->
					<div class="border-t border-border/50 p-4 lg:p-5 bg-card/50 backdrop-blur-sm flex-shrink-0">
						<div class="max-w-3xl mx-auto">
							<ChatInput
								disabled={isStreaming}
								placeholder="Ask about your diary..."
								on:send={(e) => handleSendMessage(e.detail)}
							/>
						</div>
					</div>
				{/if}
			</main>
		</div>
	{/if}
</div>
