<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { isAuthenticated } from '$lib/api/client';
	import { getAISettings } from '$lib/api/ai';
	import {
		getConversations,
		createConversation,
		deleteConversation,
		type Conversation
	} from '$lib/api/chat';
	import ChatInput from '$lib/components/chat/ChatInput.svelte';
	import ConversationList from '$lib/components/chat/ConversationList.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';

	let conversations: Conversation[] = [];
	let isCreating = false;
	let loading = true;
	let aiEnabled = false;
	let sidebarOpen = false;

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

	function handleStartNewChat() {
		closeSidebarOnMobile();
	}

	async function handleSelectConversation(convId: string) {
		goto(`/assistant/${convId}`);
	}

	async function handleDeleteConversation(convId: string) {
		try {
			await deleteConversation(convId);
			conversations = conversations.filter(c => c.id !== convId);
		} catch (e) {
			console.error('Failed to delete conversation:', e);
		}
	}

	async function handleSendMessage(content: string) {
		if (isCreating) return;

		isCreating = true;
		try {
			const conv = await createConversation();
			conversations = [conv, ...conversations];
			goto(`/assistant/${conv.id}?message=${encodeURIComponent(content)}`);
		} catch (e) {
			console.error('Failed to create conversation:', e);
		}
		isCreating = false;
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
					selectedId={null}
					{loading}
					on:select={(e) => handleSelectConversation(e.detail)}
					on:create={handleStartNewChat}
					on:delete={(e) => handleDeleteConversation(e.detail)}
				/>
			</aside>

			<!-- Chat Area - New Chat Mode -->
			<main class="flex-1 flex flex-col min-w-0 lg:bg-card/50 lg:border lg:border-border lg:rounded-2xl overflow-hidden">
				<!-- Empty state with prompt -->
				<div class="flex-1 overflow-y-auto p-4 lg:p-6">
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
				</div>

				<!-- Input -->
				<div class="border-t border-border/50 p-4 lg:p-6 bg-gradient-to-t from-card/80 to-card/50 backdrop-blur-sm flex-shrink-0">
					<div class="max-w-3xl mx-auto">
						<ChatInput
							disabled={isCreating}
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
