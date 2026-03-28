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
	import ThemeToggle from '$lib/components/ui/ThemeToggle.svelte';

	let conversations: Conversation[] = [];
	let isCreating = false;
	let loading = true;
	let aiEnabled = false;
	let sidebarOpen = false;
	let chatError = '';
	let version = '';

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

	async function fetchVersion() {
		try {
			const res = await fetch('/api/version');
			if (res.ok) {
				const data = await res.json();
				version = data.version;
			}
		} catch (e) {
			// Silently fail
		}
	}

	async function handleSendMessage(content: string) {
		if (isCreating) return;

		isCreating = true;
		chatError = '';
		try {
			const conv = await createConversation();
			conversations = [conv, ...conversations];
			goto(`/assistant/${conv.id}?message=${encodeURIComponent(content)}`);
		} catch (e) {
			console.error('Failed to create conversation:', e);
			chatError = e instanceof Error ? e.message : 'Failed to create conversation';
		}
		isCreating = false;
	}

	onMount(async () => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}

		fetchVersion();

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
	<title>AI Assistant - Diarum</title>
</svelte:head>

<div class="h-screen bg-background flex flex-col overflow-hidden">
	<PageHeader title="AI Assistant" sticky={false}>
		<div slot="actions" class="flex items-center gap-2">
			<a href="/settings" class="hidden lg:block p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200" title="Settings">
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
						d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
				</svg>
			</a>
			<button
				on:click={() => sidebarOpen = !sidebarOpen}
				class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 lg:hidden"
				aria-label="Toggle sidebar"
			>
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
				</svg>
			</button>
		</div>
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
					class="fixed inset-0 bg-black/40 backdrop-blur-sm z-30 lg:hidden"
					on:click={() => sidebarOpen = false}
					aria-label="Close sidebar"
				></button>
			{/if}

			<!-- Sidebar - Desktop -->
			<aside class="hidden lg:block lg:relative w-72
				bg-card/50 border border-border rounded-2xl flex-shrink-0 overflow-hidden">
				<ConversationList
					{conversations}
					selectedId={null}
					{loading}
					on:select={(e) => handleSelectConversation(e.detail)}
					on:create={handleStartNewChat}
					on:delete={(e) => handleDeleteConversation(e.detail)}
				/>
			</aside>

			<!-- Mobile Drawer -->
			{#if sidebarOpen}
				<div class="fixed inset-y-0 left-0 w-72 bg-card border-r border-border shadow-2xl z-40 lg:hidden animate-slide-in-left">
					<!-- Drawer Header -->
					<div class="flex items-center justify-between px-5 py-4 border-b border-border/50">
						<div class="flex items-center gap-2">
							<img src="/logo.png" alt="Diarum" class="w-6 h-6" />
							<span class="font-semibold text-foreground">AI Assistant</span>
						</div>
						<button
							on:click={() => sidebarOpen = false}
							class="p-2 hover:bg-muted rounded-lg transition-colors"
							aria-label="Close"
						>
							<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
							</svg>
						</button>
					</div>

					<!-- Drawer Content -->
					<div class="flex flex-col h-[calc(100%-57px)]">
						<!-- Actions Section -->
						<div class="px-3 py-3">
							<div class="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-2 px-1">
								Quick Actions
							</div>
							<div class="space-y-0.5">
								<a
									href="/diary"
									class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
									on:click={() => sidebarOpen = false}
								>
									<div class="p-1.5 rounded-md bg-green-500/10 text-green-500 group-hover:bg-green-500/20 transition-colors">
										<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
												d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
										</svg>
									</div>
									<div class="min-w-0">
										<div class="text-xs font-medium text-foreground">Calendar</div>
										<div class="text-[10px] text-muted-foreground truncate">View all diary entries</div>
									</div>
								</a>

								<a
									href="/search"
									class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
									on:click={() => sidebarOpen = false}
								>
									<div class="p-1.5 rounded-md bg-blue-500/10 text-blue-500 group-hover:bg-blue-500/20 transition-colors">
										<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
										</svg>
									</div>
									<div class="min-w-0">
										<div class="text-xs font-medium text-foreground">Search</div>
										<div class="text-[10px] text-muted-foreground truncate">Find diary entries</div>
									</div>
								</a>

								<a
									href="/media"
									class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
									on:click={() => sidebarOpen = false}
								>
									<div class="p-1.5 rounded-md bg-purple-500/10 text-purple-500 group-hover:bg-purple-500/20 transition-colors">
										<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
										</svg>
									</div>
									<div class="min-w-0">
										<div class="text-xs font-medium text-foreground">Media</div>
										<div class="text-[10px] text-muted-foreground truncate">Browse photos & files</div>
									</div>
								</a>

								<a
									href="/settings"
									class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
									on:click={() => sidebarOpen = false}
								>
									<div class="p-1.5 rounded-md bg-gray-500/10 text-gray-500 group-hover:bg-gray-500/20 transition-colors">
										<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
										</svg>
									</div>
									<div class="min-w-0">
										<div class="text-xs font-medium text-foreground">Settings</div>
										<div class="text-[10px] text-muted-foreground truncate">Preferences & AI config</div>
									</div>
								</a>
							</div>
						</div>

						<!-- Divider -->
						<div class="mx-3 border-t border-border/50"></div>

						<!-- Conversations Section -->
						<div class="flex-1 overflow-hidden">
							<ConversationList
								{conversations}
								selectedId={null}
								{loading}
								on:select={(e) => { sidebarOpen = false; handleSelectConversation(e.detail); }}
								on:create={() => { sidebarOpen = false; handleStartNewChat(); }}
								on:delete={(e) => handleDeleteConversation(e.detail)}
							/>
						</div>
					</div>
				</div>
			{/if}

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
						{#if chatError}
							<div class="mb-3 p-3 bg-destructive/10 text-destructive rounded-lg text-sm flex items-start gap-2">
								<svg class="w-4 h-4 mt-0.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
								</svg>
								<span>{chatError}</span>
								<button on:click={() => chatError = ''} class="ml-auto p-0.5 hover:bg-destructive/20 rounded" aria-label="Dismiss error">
									<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
									</svg>
								</button>
							</div>
						{/if}
						<ChatInput
							disabled={isCreating}
							placeholder="Ask about your diary..."
							on:send={(e) => handleSendMessage(e.detail)}
						/>
						<div class="flex items-center justify-center gap-2 mt-3 text-xs text-muted-foreground/60">
							<span class="hidden sm:inline">Press Enter to send, Shift+Enter for new line ·</span>
							<span>Diarum</span>
							{#if version}
								<a href="https://github.com/songtianlun/diarum" target="_blank" rel="noopener noreferrer" class="font-mono text-[10px] hover:text-foreground transition-colors">{version}</a>
							{/if}
							<span class="hidden sm:inline">·</span>
							<span class="hidden sm:block"><ThemeToggle /></span>
						</div>
					</div>
				</div>
			</main>
		</div>
	{/if}
</div>
