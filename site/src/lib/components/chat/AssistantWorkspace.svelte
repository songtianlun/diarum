<script lang="ts">
	import { onDestroy, onMount, tick } from 'svelte';
	import { goto, replaceState } from '$app/navigation';
	import { page } from '$app/stores';
	import { t } from '$lib/i18n';
	import { isAuthenticated } from '$lib/api/client';
	import { getAISettings } from '$lib/api/ai';
	import {
		createConversation,
		deleteConversation,
		deleteMessagesFrom,
		generateConversationTitle,
		getConversation,
		getConversations,
		streamChat,
		updateConversationTitle,
		type ChatScope,
		type Conversation,
		type DiaryReference,
		type Message,
		type ToolActivity
	} from '$lib/api/chat';
	import ChatInput from '$lib/components/chat/ChatInput.svelte';
	import ChatMessage from '$lib/components/chat/ChatMessage.svelte';
	import ConversationList from '$lib/components/chat/ConversationList.svelte';
	import DiaryPreviewModal from '$lib/components/chat/DiaryPreviewModal.svelte';
	import EmptyState from '$lib/components/chat/EmptyState.svelte';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import ThemeToggle from '$lib/components/ui/ThemeToggle.svelte';

	export let routeConversationId: string | null = null;

	let conversations: Conversation[] = [];
	let activeConversationId: string | null = routeConversationId;
	let messages: Message[] = [];

	let loading = true;
	let messagesLoading = false;
	let aiEnabled = false;
	let sidebarOpen = false;
	let desktopSidebarCollapsed = false;
	let version = '';

	let scope: ChatScope = 'all';
	let isStreaming = false;
	let streamMessage: Message | null = null;
	let activeAbort: AbortController | null = null;
	let activeStreamToken = 0;

	let chatError = '';
	let lastFailedPrompt: string | null = null;
	let previewDiaryDate: string | null = null;

	let messagesContainer: HTMLDivElement;
	let chatInputRef: ChatInput;
	let autoStickToBottom = true;
	let showScrollToBottom = false;

	const STICK_THRESHOLD_PX = 80;

	$: activeConversation = conversations.find((c) => c.id === activeConversationId) ?? null;
	$: headerTitle = activeConversation?.title || $t('assistant.title');

	function isNearBottom(): boolean {
		if (!messagesContainer) return true;
		const { scrollTop, scrollHeight, clientHeight } = messagesContainer;
		return scrollHeight - scrollTop - clientHeight <= STICK_THRESHOLD_PX;
	}

	function handleScroll() {
		if (!messagesContainer) return;
		const near = isNearBottom();
		autoStickToBottom = near;
		showScrollToBottom = !near && (messages.length > 0 || isStreaming);
	}

	async function scrollToBottom(force = false) {
		await tick();
		if (!messagesContainer) return;
		if (force || autoStickToBottom) {
			messagesContainer.scrollTop = messagesContainer.scrollHeight;
			autoStickToBottom = true;
			showScrollToBottom = false;
		}
	}

	function closeSidebarOnMobile() {
		if (typeof window !== 'undefined' && window.innerWidth < 1024) {
			sidebarOpen = false;
		}
	}

	function cancelActiveStream() {
		activeStreamToken++;
		if (activeAbort) {
			activeAbort.abort();
			activeAbort = null;
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
		chatError = '';
		lastFailedPrompt = null;
		try {
			const detail = await getConversation(convId);
			if (activeConversationId !== convId) return;
			messages = detail.messages || [];
			await scrollToBottom(true);
		} catch (e) {
			console.error('Failed to load messages:', e);
			activeConversationId = null;
			messages = [];
			goto('/assistant');
		} finally {
			messagesLoading = false;
		}
	}

	async function fetchVersion() {
		try {
			const res = await fetch('/api/version');
			if (res.ok) {
				const data = await res.json();
				version = data.version;
			}
		} catch {
			// Ignore version lookup failure
		}
	}

	function handleStartNewChat() {
		cancelActiveStream();
		isStreaming = false;
		streamMessage = null;
		activeConversationId = null;
		messages = [];
		chatError = '';
		lastFailedPrompt = null;
		closeSidebarOnMobile();
		if ($page.url.pathname !== '/assistant') {
			goto('/assistant');
		}
		tick().then(() => chatInputRef?.focus());
	}

	async function handleSelectConversation(convId: string) {
		closeSidebarOnMobile();
		if (convId === activeConversationId) return;
		cancelActiveStream();
		isStreaming = false;
		streamMessage = null;
		activeConversationId = convId;
		if ($page.url.pathname !== `/assistant/${convId}`) {
			goto(`/assistant/${convId}`);
		}
		await loadMessages(convId);
	}

	async function handleDeleteConversation(convId: string) {
		try {
			await deleteConversation(convId);
			conversations = conversations.filter((c) => c.id !== convId);
			if (activeConversationId === convId) {
				handleStartNewChat();
			}
		} catch (e) {
			console.error('Failed to delete conversation:', e);
		}
	}

	async function handleRenameConversation(id: string, title: string) {
		const prev = conversations.find((c) => c.id === id)?.title ?? '';
		conversations = conversations.map((c) => (c.id === id ? { ...c, title } : c));
		try {
			const updated = await updateConversationTitle(id, title);
			conversations = conversations.map((c) =>
				c.id === id ? { ...c, title: updated.title, updated: updated.updated } : c
			);
		} catch (e) {
			console.error('Failed to rename conversation:', e);
			conversations = conversations.map((c) => (c.id === id ? { ...c, title: prev } : c));
		}
	}

	async function runStream(
		convId: string,
		content: string,
		opts: { regenerate?: boolean; isFirstTurn?: boolean; tempUserMsgId?: string } = {}
	) {
		cancelActiveStream();
		const token = activeStreamToken;
		const controller = new AbortController();
		activeAbort = controller;

		isStreaming = true;
		chatError = '';
		lastFailedPrompt = null;
		autoStickToBottom = true;

		let thinkingStartedAt: number | null = null;
		let thinkingEnded = false;
		let thinkingText = '';
		let thinkingDurationSec: number | undefined = undefined;
		let contentText = '';
		let tools: ToolActivity[] = [];
		let references: DiaryReference[] = [];
		let referencedIds: string[] = [];

		streamMessage = {
			id: 'streaming',
			role: 'assistant',
			content: '',
			thinking: '',
			tools: [],
			references: [],
			created: ''
		};

		await scrollToBottom(true);

		let completedCleanly = false;
		let finalAssistantId = `temp-assistant-${Date.now()}`;

		try {
			for await (const chunk of streamChat(convId, content, {
				signal: controller.signal,
				scope,
				regenerate: opts.regenerate
			})) {
				if (token !== activeStreamToken) return;

				if (chunk.error) {
					chatError = chunk.error;
					lastFailedPrompt = content;
					break;
				}

				if (chunk.title) {
					conversations = conversations.map((c) =>
						c.id === convId ? { ...c, title: chunk.title! } : c
					);
				}

				if (chunk.thinking) {
					if (thinkingStartedAt === null) {
						thinkingStartedAt = Date.now();
					}
					thinkingText += chunk.thinking;
					streamMessage = {
						...streamMessage!,
						thinking: thinkingText
					};
					scrollToBottom();
				}

				if (chunk.type === 'tool_start' && chunk.tool) {
					tools = [
						...tools,
						{
							id: chunk.tool.id,
							name: chunk.tool.name,
							args: chunk.tool.args,
							status: 'running'
						}
					];
					streamMessage = { ...streamMessage!, tools };
					scrollToBottom();
				}

				if (chunk.type === 'tool_end' && chunk.tool) {
					tools = tools.map((item) =>
						item.id === chunk.tool!.id
							? {
									...item,
									status: chunk.tool!.error ? 'error' : 'done',
									count: chunk.tool!.count,
									dates: chunk.tool!.dates,
									error: chunk.tool!.error
								}
							: item
					);
					streamMessage = { ...streamMessage!, tools };
					scrollToBottom();
				}

				if (chunk.references && chunk.references.length > 0) {
					references = chunk.references;
					streamMessage = { ...streamMessage!, references };
				}

				if (chunk.content) {
					if (thinkingStartedAt !== null && !thinkingEnded) {
						thinkingEnded = true;
						thinkingDurationSec = Math.max(1, Math.round((Date.now() - thinkingStartedAt) / 1000));
					}
					contentText += chunk.content;
					streamMessage = {
						...streamMessage!,
						content: contentText,
						thinkingDurationSec
					};
					scrollToBottom();
				}

				if (chunk.done) {
					completedCleanly = true;
					if (chunk.message_id) finalAssistantId = chunk.message_id;
					if (chunk.referenced_diaries) referencedIds = chunk.referenced_diaries;
					if (chunk.references) references = chunk.references;
					if (chunk.user_message_id && opts.tempUserMsgId) {
						messages = messages.map((m) =>
							m.id === opts.tempUserMsgId ? { ...m, id: chunk.user_message_id! } : m
						);
					}
				}
			}
		} catch (e) {
			if (token !== activeStreamToken) return;
			const isAbort = e instanceof DOMException && e.name === 'AbortError';
			if (!isAbort) {
				console.error('Failed to stream chat:', e);
				chatError = e instanceof Error ? e.message : 'Failed to send message';
				lastFailedPrompt = content;
			}
		}

		if (token !== activeStreamToken) return;

		if (thinkingStartedAt !== null && !thinkingEnded) {
			thinkingDurationSec = Math.max(1, Math.round((Date.now() - thinkingStartedAt) / 1000));
		}

		if (contentText || thinkingText) {
			const finalized: Message = {
				id: finalAssistantId,
				role: 'assistant',
				content: contentText,
				thinking: thinkingText || undefined,
				thinkingDurationSec,
				tools: tools.length > 0 ? tools : undefined,
				referenced_diaries: referencedIds.length > 0 ? referencedIds : undefined,
				references: references.length > 0 ? references : undefined,
				created: new Date().toISOString()
			};
			messages = [...messages, finalized];
		}

		streamMessage = null;
		isStreaming = false;
		activeAbort = null;

		// Refresh conversation ordering and real IDs if stopped mid-stream
		await loadConversations();

		if (completedCleanly && opts.isFirstTurn && contentText) {
			// Upgrade extracted title asynchronously with a concise LLM-generated title
			generateConversationTitle(convId).then((updated) => {
				if (updated?.title) {
					conversations = conversations.map((c) =>
						c.id === convId ? { ...c, title: updated.title, updated: updated.updated } : c
					);
				}
			});
		} else if (!completedCleanly && activeConversationId === convId && contentText) {
			// Sync server-assigned IDs after a manual Stop so follow-up edit/regenerate work
			try {
				const detail = await getConversation(convId);
				if (activeConversationId === convId && detail.messages.length >= messages.length) {
					messages = detail.messages;
				}
			} catch {
				// Ignore sync failure
			}
		}
	}

	async function handleSendMessage(content: string) {
		if (isStreaming) return;
		chatError = '';

		let convId = activeConversationId;
		const isFirstTurn = !convId || messages.length === 0;

		if (!convId) {
			try {
				const conv = await createConversation();
				convId = conv.id;
				activeConversationId = convId;
				conversations = [conv, ...conversations];
				replaceState(`/assistant/${convId}`, {});
			} catch (e) {
				console.error('Failed to create conversation:', e);
				chatError = e instanceof Error ? e.message : 'Failed to create conversation';
				return;
			}
		}

		const tempUserId = `temp-user-${Date.now()}`;
		const userMsg: Message = {
			id: tempUserId,
			role: 'user',
			content,
			created: new Date().toISOString()
		};
		messages = [...messages, userMsg];
		await scrollToBottom(true);

		await runStream(convId, content, { isFirstTurn, tempUserMsgId: tempUserId });
	}

	function handleStopStreaming() {
		if (!isStreaming) return;
		if (activeAbort) {
			activeAbort.abort();
		}
	}

	async function handleRegenerate(assistantMsgId: string) {
		if (isStreaming || !activeConversationId) return;
		const idx = messages.findIndex((m) => m.id === assistantMsgId);
		if (idx <= 0) return;

		const prevUserMsg = messages[idx - 1];
		if (!prevUserMsg || prevUserMsg.role !== 'user') return;

		try {
			await deleteMessagesFrom(activeConversationId, assistantMsgId);
			messages = messages.slice(0, idx);
			await runStream(activeConversationId, prevUserMsg.content, { regenerate: true });
		} catch (e) {
			console.error('Failed to regenerate:', e);
			chatError = e instanceof Error ? e.message : 'Failed to regenerate';
		}
	}

	async function handleEditMessage(userMsgId: string, newContent: string) {
		if (isStreaming || !activeConversationId) return;
		const idx = messages.findIndex((m) => m.id === userMsgId);
		if (idx < 0) return;

		try {
			await deleteMessagesFrom(activeConversationId, userMsgId);
			messages = messages.slice(0, idx);
			await handleSendMessage(newContent);
		} catch (e) {
			console.error('Failed to edit message:', e);
			chatError = e instanceof Error ? e.message : 'Failed to edit message';
		}
	}

	async function handleRetry() {
		if (isStreaming || !activeConversationId) return;
		const lastMsg = messages[messages.length - 1];
		if (lastMsg && lastMsg.role === 'user') {
			await runStream(activeConversationId, lastMsg.content, { regenerate: true });
		} else if (lastFailedPrompt) {
			const prompt = lastFailedPrompt;
			lastFailedPrompt = null;
			await handleSendMessage(prompt);
		}
	}

	// React when the route param changes (e.g., browser back/forward)
	$: if (!loading && aiEnabled && routeConversationId !== activeConversationId) {
		if (routeConversationId) {
			cancelActiveStream();
			isStreaming = false;
			streamMessage = null;
			activeConversationId = routeConversationId;
			loadMessages(routeConversationId);
		}
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

		if (routeConversationId) {
			activeConversationId = routeConversationId;
			await loadMessages(routeConversationId);
		}

		loading = false;

		// Backwards-compatible support for ?message= deep links
		const messageParam = $page.url.searchParams.get('message');
		if (messageParam) {
			const targetPath = activeConversationId ? `/assistant/${activeConversationId}` : '/assistant';
			replaceState(targetPath, {});
			await handleSendMessage(messageParam);
		} else {
			await tick();
			chatInputRef?.focus();
		}
	});

	onDestroy(() => {
		cancelActiveStream();
	});
</script>

<svelte:head>
	<title>{$t('assistant.pageTitle')}</title>
</svelte:head>

<div class="h-[100dvh] bg-background flex flex-col overflow-hidden">
	<PageHeader title={headerTitle} sticky={false}>
		<div slot="actions" class="flex items-center gap-1.5">
			{#if aiEnabled}
				<button
					type="button"
					on:click={() => (desktopSidebarCollapsed = !desktopSidebarCollapsed)}
					class="hidden lg:flex p-1.5 hover:bg-muted/60 rounded-lg transition-colors text-muted-foreground hover:text-foreground"
					title={$t('assistant.toggleSidebar')}
					aria-label={$t('assistant.toggleSidebar')}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h10M4 18h16" />
					</svg>
				</button>
			{/if}
			<a
				href="/settings"
				class="p-1.5 hover:bg-muted/60 rounded-lg transition-colors text-muted-foreground hover:text-foreground"
				title={$t('nav.settings')}
			>
				<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"
					/>
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
				</svg>
			</a>
			{#if aiEnabled}
				<button
					type="button"
					on:click={() => (sidebarOpen = !sidebarOpen)}
					class="p-1.5 hover:bg-muted/60 rounded-lg transition-colors lg:hidden"
					aria-label={$t('assistant.toggleSidebar')}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
					</svg>
				</button>
			{/if}
		</div>
	</PageHeader>

	{#if loading}
		<div class="flex-1 flex items-center justify-center">
			<svg class="w-7 h-7 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
				<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
				<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
			</svg>
		</div>
	{:else if !aiEnabled}
		<div class="flex-1 flex items-center justify-center p-4">
			<div class="text-center max-w-md rounded-2xl border border-border bg-card/70 p-8 shadow-sm">
				<div class="w-14 h-14 mx-auto mb-4 rounded-2xl bg-muted flex items-center justify-center">
					<svg class="w-7 h-7 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
							d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
					</svg>
				</div>
				<h2 class="text-lg font-semibold mb-2">{$t('assistant.notEnabledTitle')}</h2>
				<p class="text-sm text-muted-foreground mb-5 leading-relaxed">
					{$t('assistant.notEnabledDesc')}
				</p>
				<a
					href="/settings"
					class="inline-flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-xl text-sm font-medium hover:opacity-90 transition-opacity"
				>
					{$t('assistant.goToSettings')}
				</a>
			</div>
		</div>
	{:else}
		<div class="flex-1 flex overflow-hidden relative lg:p-3 lg:gap-3">
			<!-- Mobile Overlay -->
			{#if sidebarOpen}
				<button
					type="button"
					class="fixed inset-0 bg-black/40 backdrop-blur-sm z-30 lg:hidden"
					on:click={() => (sidebarOpen = false)}
					aria-label={$t('assistant.closePreview')}
				></button>
			{/if}

			<!-- Desktop Sidebar -->
			{#if !desktopSidebarCollapsed}
				<aside class="hidden lg:flex lg:flex-col w-72 bg-card/70 border border-border rounded-2xl flex-shrink-0 overflow-hidden">
					<ConversationList
						{conversations}
						selectedId={activeConversationId}
						{loading}
						on:select={(e) => handleSelectConversation(e.detail)}
						on:create={handleStartNewChat}
						on:delete={(e) => handleDeleteConversation(e.detail)}
						on:rename={(e) => handleRenameConversation(e.detail.id, e.detail.title)}
					/>
				</aside>
			{/if}

			<!-- Mobile Drawer -->
			{#if sidebarOpen}
				<div class="fixed inset-y-0 left-0 w-72 max-w-[85vw] bg-card border-r border-border shadow-2xl z-40 lg:hidden flex flex-col animate-slide-in-left">
					<div class="flex items-center justify-between px-4 py-3 border-b border-border/50 flex-shrink-0">
						<div class="flex items-center gap-2">
							<img src="/logo.png" alt="Diarum" class="w-5 h-5" />
							<span class="text-sm font-semibold text-foreground">{$t('assistant.title')}</span>
						</div>
						<button
							type="button"
							on:click={() => (sidebarOpen = false)}
							class="p-1.5 hover:bg-muted rounded-lg transition-colors"
							aria-label={$t('assistant.closePreview')}
						>
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
							</svg>
						</button>
					</div>

					<div class="flex-1 overflow-hidden">
						<ConversationList
							{conversations}
							selectedId={activeConversationId}
							{loading}
							on:select={(e) => handleSelectConversation(e.detail)}
							on:create={handleStartNewChat}
							on:delete={(e) => handleDeleteConversation(e.detail)}
							on:rename={(e) => handleRenameConversation(e.detail.id, e.detail.title)}
						/>
					</div>

					<div class="border-t border-border/50 px-3 py-2.5 flex items-center justify-around text-xs text-muted-foreground flex-shrink-0">
						<a href="/diary" on:click={() => (sidebarOpen = false)} class="px-2 py-1 rounded-lg hover:bg-muted/70 hover:text-foreground">
							{$t('assistant.navCalendar')}
						</a>
						<a href="/search" on:click={() => (sidebarOpen = false)} class="px-2 py-1 rounded-lg hover:bg-muted/70 hover:text-foreground">
							{$t('assistant.navSearch')}
						</a>
						<a href="/media" on:click={() => (sidebarOpen = false)} class="px-2 py-1 rounded-lg hover:bg-muted/70 hover:text-foreground">
							{$t('assistant.navMedia')}
						</a>
						<ThemeToggle />
					</div>
				</div>
			{/if}

			<!-- Chat Main Area -->
			<main class="flex-1 flex flex-col min-w-0 relative lg:bg-card/55 lg:border lg:border-border lg:rounded-2xl overflow-hidden">
				<!-- Messages Scroll Area -->
				<div
					bind:this={messagesContainer}
					on:scroll={handleScroll}
					class="flex-1 overflow-y-auto px-4 py-5 sm:px-6"
				>
					{#if messagesLoading}
						<div class="flex flex-col items-center justify-center h-full gap-2.5">
							<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
							</svg>
							<span class="text-xs text-muted-foreground">{$t('assistant.loadingMessages')}</span>
						</div>
					{:else if messages.length === 0 && !streamMessage}
						<EmptyState on:prompt={(e) => handleSendMessage(e.detail)} />
					{:else}
						<div class="max-w-3xl mx-auto space-y-6 pb-4">
							{#each messages as message, i (message.id)}
								<ChatMessage
									{message}
									canEdit={!isStreaming && message.role === 'user'}
									canRegenerate={!isStreaming && message.role === 'assistant' && i > 0}
									on:regenerate={(e) => handleRegenerate(e.detail)}
									on:edit={(e) => handleEditMessage(e.detail.id, e.detail.content)}
									on:previewDiary={(e) => (previewDiaryDate = e.detail)}
								/>
							{/each}

							{#if streamMessage}
								<ChatMessage
									message={streamMessage}
									isStreaming={true}
									on:previewDiary={(e) => (previewDiaryDate = e.detail)}
								/>
							{/if}
						</div>
					{/if}
				</div>

				<!-- Floating Scroll to Bottom Button -->
				{#if showScrollToBottom}
					<div class="absolute bottom-36 left-1/2 -translate-x-1/2 z-20 pointer-events-none">
						<button
							type="button"
							on:click={() => scrollToBottom(true)}
							class="pointer-events-auto inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full
								bg-card/95 border border-border shadow-md text-xs text-foreground hover:bg-muted transition-all"
							title={$t('assistant.scrollToBottom')}
						>
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 14l-7 7m0 0l-7-7m7 7V3" />
							</svg>
							<span>{$t('assistant.scrollToBottom')}</span>
						</button>
					</div>
				{/if}

				<!-- Bottom Composer -->
				<div class="px-3 pb-3 pt-2 sm:px-6 sm:pb-4 bg-gradient-to-t from-background via-background/95 to-transparent flex-shrink-0">
					<div class="max-w-3xl mx-auto">
						{#if chatError}
							<div class="mb-2.5 px-3.5 py-2.5 rounded-xl bg-red-500/10 border border-red-500/25 text-red-600 dark:text-red-400 text-xs flex items-center gap-2.5">
								<svg class="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
								</svg>
								<span class="flex-1 break-words">{chatError}</span>
								<button
									type="button"
									on:click={handleRetry}
									class="px-2.5 py-1 rounded-lg bg-red-500/15 hover:bg-red-500/25 font-medium transition-colors flex-shrink-0"
								>
									{$t('assistant.retry')}
								</button>
								<button
									type="button"
									on:click={() => (chatError = '')}
									class="p-0.5 hover:bg-red-500/20 rounded"
									aria-label={$t('assistant.dismiss')}
								>
									<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
									</svg>
								</button>
							</div>
						{/if}

						<ChatInput
							bind:this={chatInputRef}
							disabled={isStreaming}
							streaming={isStreaming}
							{scope}
							on:send={(e) => handleSendMessage(e.detail)}
							on:stop={handleStopStreaming}
							on:scopeChange={(e) => (scope = e.detail)}
						/>

						<div class="flex items-center justify-center gap-2 mt-2 text-[11px] text-muted-foreground/65">
							<span class="hidden sm:inline">{$t('assistant.inputHint')} ·</span>
							<span>Diarum</span>
							{#if version}
								<a
									href="https://github.com/songtianlun/diarum"
									target="_blank"
									rel="noopener noreferrer"
									class="font-mono text-[10px] hover:text-foreground transition-colors"
								>
									{version}
								</a>
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

<DiaryPreviewModal date={previewDiaryDate} on:close={() => (previewDiaryDate = null)} />
