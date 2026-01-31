<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
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
	import PageHeader from '$lib/components/ui/PageHeader.svelte';

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
			// If conversation not found, redirect to assistant main page
			goto('/assistant');
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
		goto('/assistant');
	}

	async function handleSelectConversation(convId: string) {
		if (convId !== selectedConversationId) {
			goto(`/assistant/${convId}`);
		}
		closeSidebarOnMobile();
	}

	async function handleDeleteConversation(convId: string) {
		try {
			await deleteConversation(convId);
			conversations = conversations.filter(c => c.id !== convId);
			if (selectedConversationId === convId) {
				goto('/assistant');
			}
		} catch (e) {
			console.error('Failed to delete conversation:', e);
		}
	}

	async function handleSendMessage(content: string) {
		if (isStreaming || !selectedConversationId) return;

		const convId = selectedConversationId;

		// Add user message with unique ID
		const userMsg: Message = {
			id: `temp-user-${Date.now()}`,
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
				if (chunk.title && convId) {
					conversations = conversations.map(c =>
						c.id === convId ? { ...c, title: chunk.title! } : c
					);
				}
				if (chunk.content) {
					streamingContent += chunk.content;
					scrollToBottom();
				}
				if (chunk.done) {
					const assistantMsg: Message = {
						id: `temp-assistant-${Date.now()}`,
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

		// Get conversation ID from URL
		const convId = $page.params.id;
		if (convId) {
			selectedConversationId = convId;
			await loadMessages(convId);
		}

		loading = false;

		// Check for message query parameter (from new chat redirect)
		const messageParam = $page.url.searchParams.get('message');
		if (messageParam && convId) {
			// Clear the URL parameter without triggering navigation
			window.history.replaceState({}, '', `/assistant/${convId}`);
			// Send the message
			await handleSendMessage(messageParam);
		}
	});
</script>

<svelte:head>
	<title>AI Assistant - Journitalia</title>
</svelte:head>

<div class="h-screen bg-background flex flex-col overflow-hidden">
	<PageHeader title="AI Assistant" sticky={false}>
		<button
			slot="actions"
			on:click={() => sidebarOpen = !sidebarOpen}
			class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 lg:hidden"
			aria-label="Toggle sidebar"
		>
			<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
			</svg>
		</button>
	</PageHeader>

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
				top-11 lg:top-0 h-[calc(100vh-2.75rem)] lg:h-full overflow-hidden">
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
							{#if isStreaming}
								<ChatMessage
									message={{ id: 'streaming', role: 'assistant', content: streamingContent, created: '' }}
									isStreaming={true}
								/>
							{/if}
						</div>
					{/if}
				</div>

				<!-- Input -->
				<div class="border-t border-border/50 p-4 lg:p-6 bg-gradient-to-t from-card/80 to-card/50 backdrop-blur-sm flex-shrink-0">
					<div class="max-w-3xl mx-auto">
						<ChatInput
							disabled={isStreaming}
							placeholder="Ask about your diary..."
							on:send={(e) => handleSendMessage(e.detail)}
						/>
						<p class="text-xs text-muted-foreground/60 text-center mt-3">
							Press Enter to send, Shift+Enter for new line
						</p>
					</div>
				</div>
			</main>
		</div>
	{/if}
</div>
