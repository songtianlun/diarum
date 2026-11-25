<script lang="ts">
	import { createEventDispatcher, tick } from 'svelte';
	import { t } from '$lib/i18n';
	import type { Conversation } from '$lib/api/chat';

	export let conversations: Conversation[] = [];
	export let selectedId: string | null = null;
	export let loading = false;

	const dispatch = createEventDispatcher<{
		select: string;
		create: void;
		delete: string;
		rename: { id: string; title: string };
	}>();

	let searchQuery = '';
	let deleteConfirmId: string | null = null;
	let editingId: string | null = null;
	let editingTitle = '';
	let editInputEl: HTMLInputElement;

	type GroupKey = 'today' | 'yesterday' | 'pastWeek' | 'earlier';
	interface ConversationGroup {
		key: GroupKey;
		labelKey: string;
		items: Conversation[];
	}

	function getBucket(dateStr: string): GroupKey {
		const date = new Date(dateStr);
		if (Number.isNaN(date.getTime())) return 'earlier';
		const now = new Date();
		const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
		const startOfItem = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
		const diffDays = Math.floor((startOfToday - startOfItem) / 86400000);
		if (diffDays <= 0) return 'today';
		if (diffDays === 1) return 'yesterday';
		if (diffDays < 7) return 'pastWeek';
		return 'earlier';
	}

	$: filtered = searchQuery.trim()
		? conversations.filter((c) =>
				(c.title || $t('assistant.untitledConversation'))
					.toLowerCase()
					.includes(searchQuery.trim().toLowerCase())
			)
		: conversations;

	$: groups = (() => {
		const map: Record<GroupKey, Conversation[]> = {
			today: [],
			yesterday: [],
			pastWeek: [],
			earlier: []
		};
		for (const conv of filtered) {
			map[getBucket(conv.updated || conv.created)].push(conv);
		}
		const defs: Array<{ key: GroupKey; labelKey: string }> = [
			{ key: 'today', labelKey: 'assistant.today' },
			{ key: 'yesterday', labelKey: 'assistant.yesterday' },
			{ key: 'pastWeek', labelKey: 'assistant.pastWeek' },
			{ key: 'earlier', labelKey: 'assistant.earlier' }
		];
		return defs
			.filter((d) => map[d.key].length > 0)
			.map((d): ConversationGroup => ({ key: d.key, labelKey: d.labelKey, items: map[d.key] }));
	})();

	async function startRename(e: Event, conv: Conversation) {
		e.stopPropagation();
		deleteConfirmId = null;
		editingId = conv.id;
		editingTitle = conv.title || '';
		await tick();
		editInputEl?.focus();
		editInputEl?.select();
	}

	function commitRename() {
		if (!editingId) return;
		const id = editingId;
		const trimmed = editingTitle.trim();
		editingId = null;
		if (trimmed) {
			dispatch('rename', { id, title: trimmed });
		}
	}

	function cancelRename() {
		editingId = null;
		editingTitle = '';
	}

	function handleRenameKeydown(e: KeyboardEvent) {
		if (e.isComposing || e.keyCode === 229) return;
		if (e.key === 'Enter') {
			e.preventDefault();
			commitRename();
		} else if (e.key === 'Escape') {
			e.preventDefault();
			cancelRename();
		}
	}

	function handleDelete(e: Event, convId: string) {
		e.stopPropagation();
		if (deleteConfirmId === convId) {
			dispatch('delete', convId);
			deleteConfirmId = null;
		} else {
			deleteConfirmId = convId;
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
	<!-- Header -->
	<div class="p-3 border-b border-border/50 flex-shrink-0 space-y-2.5">
		<div class="flex items-center justify-between px-1">
			<div class="flex items-center gap-2">
				<h2 class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
					{$t('assistant.conversations')}
				</h2>
				<span class="text-[11px] text-muted-foreground bg-muted px-1.5 py-0.5 rounded-md tabular-nums">
					{conversations.length}
				</span>
			</div>
			<button
				type="button"
				on:click={() => dispatch('create')}
				class="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-primary text-primary-foreground
					hover:opacity-90 active:scale-95 transition-all text-xs font-medium shadow-sm"
				title={$t('assistant.newChat')}
			>
				<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
				</svg>
				<span>{$t('assistant.newChat')}</span>
			</button>
		</div>

		{#if conversations.length > 0}
			<div class="relative">
				<svg
					class="w-3.5 h-3.5 text-muted-foreground/70 absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none"
					fill="none"
					stroke="currentColor"
					viewBox="0 0 24 24"
				>
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
				</svg>
				<input
					type="text"
					bind:value={searchQuery}
					placeholder={$t('assistant.searchConversations')}
					class="w-full pl-8 pr-7 py-1.5 text-xs rounded-lg bg-muted/50 border border-transparent
						focus:border-border focus:bg-background focus:outline-none text-foreground placeholder:text-muted-foreground/70"
				/>
				{#if searchQuery}
					<button
						type="button"
						on:click={() => (searchQuery = '')}
						class="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
						aria-label={$t('assistant.dismiss')}
					>
						<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
						</svg>
					</button>
				{/if}
			</div>
		{/if}
	</div>

	<!-- List -->
	<div class="flex-1 overflow-y-auto px-2 py-2">
		{#if loading}
			<div class="flex items-center justify-center py-12">
				<svg class="w-5 h-5 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
				</svg>
			</div>
		{:else if conversations.length === 0}
			<div class="text-center py-12 px-4">
				<div class="w-10 h-10 mx-auto mb-2.5 rounded-xl bg-muted/60 flex items-center justify-center">
					<svg class="w-5 h-5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
							d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
					</svg>
				</div>
				<p class="text-muted-foreground text-xs">{$t('assistant.noConversations')}</p>
			</div>
		{:else if groups.length === 0}
			<div class="text-center py-10 px-4 text-xs text-muted-foreground">
				{$t('assistant.noMatchingConversations')}
			</div>
		{:else}
			<div class="space-y-4">
				{#each groups as group (group.key)}
					<div>
						<div class="px-2 pb-1 text-[11px] font-medium text-muted-foreground/80">
							{$t(group.labelKey)}
						</div>
						<div class="space-y-0.5">
							{#each group.items as conv (conv.id)}
								<div
									role="button"
									tabindex="0"
									on:click={() => editingId !== conv.id && dispatch('select', conv.id)}
									on:keydown={(e) => e.key === 'Enter' && editingId !== conv.id && dispatch('select', conv.id)}
									class="w-full text-left px-2.5 py-2 rounded-xl transition-colors group cursor-pointer relative
										{selectedId === conv.id
											? 'bg-primary/10 text-foreground font-medium'
											: 'hover:bg-muted/60 text-foreground/90'}"
								>
									{#if editingId === conv.id}
										<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
										<div on:click|stopPropagation class="flex items-center gap-1">
											<input
												bind:this={editInputEl}
												bind:value={editingTitle}
												on:keydown={handleRenameKeydown}
												on:blur={commitRename}
												class="w-full text-xs px-2 py-1 rounded-md bg-background border border-primary/50 focus:outline-none text-foreground"
											/>
										</div>
									{:else}
										<div class="flex items-center justify-between gap-1.5">
											<div class="min-w-0 flex-1">
												<div class="text-xs truncate leading-5">
													{conv.title || $t('assistant.untitledConversation')}
												</div>
											</div>

											{#if deleteConfirmId === conv.id}
												<div class="flex items-center gap-1 flex-shrink-0">
													<button
														type="button"
														on:click={(e) => handleDelete(e, conv.id)}
														class="p-1 rounded-md bg-red-600 text-white hover:opacity-90 transition-opacity"
														title={$t('assistant.confirmDelete')}
													>
														<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
															<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
														</svg>
													</button>
													<button
														type="button"
														on:click={cancelDelete}
														class="p-1 rounded-md bg-muted hover:bg-muted/80"
														title={$t('assistant.cancel')}
													>
														<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
															<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
														</svg>
													</button>
												</div>
											{:else}
												<div class="flex items-center gap-0.5 flex-shrink-0 opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity">
													<button
														type="button"
														on:click={(e) => startRename(e, conv)}
														class="p-1 rounded-md hover:bg-muted text-muted-foreground hover:text-foreground"
														title={$t('assistant.rename')}
													>
														<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
															<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
																d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
														</svg>
													</button>
													<button
														type="button"
														on:click={(e) => handleDelete(e, conv.id)}
														class="p-1 rounded-md hover:bg-red-500/10 text-muted-foreground hover:text-red-500"
														title={$t('assistant.delete')}
													>
														<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
															<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
																d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
														</svg>
													</button>
												</div>
											{/if}
										</div>
									{/if}
								</div>
							{/each}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>
