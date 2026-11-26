<script lang="ts">
	import { createEventDispatcher, tick } from 'svelte';
	import { t, getIntlLocale } from '$lib/i18n';
	import type { DiaryReference, Message, ToolActivity } from '$lib/api/chat';
	import { getDiariesByIds } from '$lib/api/diaries';
	import { copyText } from '$lib/utils/clipboard';
	import {
		renderMarkdownBlock,
		splitMarkdownBlocks,
		stripHtmlPreview
	} from '$lib/utils/chatMarkdown';

	export let message: Message;
	export let isStreaming = false;
	export let canRegenerate = false;
	export let canEdit = false;

	const dispatch = createEventDispatcher<{
		regenerate: string;
		edit: { id: string; content: string };
		previewDiary: string;
	}>();

	let copied = false;
	let thinkingExpanded = false;
	let refsExpanded = false;
	let refsLoading = false;
	let loadedRefs: DiaryReference[] = [];
	let refsLoadedForKey = '';

	let isEditing = false;
	let editDraft = '';
	let editEl: HTMLTextAreaElement;

	// Cache rendered HTML for completed blocks so streaming only parses the active last block
	let renderedBlocks: string[] = [];
	let cachedBlockSources: string[] = [];

	$: blocks = message.role === 'assistant' ? splitMarkdownBlocks(message.content) : [];
	$: {
		if (message.role === 'assistant') {
			const nextRendered: string[] = [];
			const nextSources: string[] = [];
			for (let i = 0; i < blocks.length; i++) {
				const src = blocks[i];
				const isLastActive = isStreaming && i === blocks.length - 1;
				if (!isLastActive && cachedBlockSources[i] === src && renderedBlocks[i] !== undefined) {
					nextRendered.push(renderedBlocks[i]);
					nextSources.push(src);
				} else {
					nextRendered.push(renderMarkdownBlock(src, isLastActive));
					nextSources.push(isLastActive ? '' : src);
				}
			}
			renderedBlocks = nextRendered;
			cachedBlockSources = nextSources;
		}
	}

	// Auto-expand thinking while actively thinking with no answer text yet
	$: isActivelyThinking = isStreaming && Boolean(message.thinking) && !message.content;

	$: displayRefs = message.references && message.references.length > 0 ? message.references : loadedRefs;
	$: refCount =
		message.references && message.references.length > 0
			? message.references.length
			: (message.referenced_diaries?.length ?? 0);

	function formatTime(dateStr: string): string {
		if (!dateStr) return '';
		const date = new Date(dateStr);
		if (Number.isNaN(date.getTime())) return '';
		return date.toLocaleTimeString(getIntlLocale(), { hour: '2-digit', minute: '2-digit' });
	}

	function formatDiaryDate(dateStr: string): string {
		const parts = dateStr.slice(0, 10).split('-').map(Number);
		if (parts.length !== 3 || parts.some(Number.isNaN)) return dateStr;
		const date = new Date(parts[0], parts[1] - 1, parts[2]);
		return date.toLocaleDateString(getIntlLocale(), { year: 'numeric', month: 'short', day: 'numeric' });
	}

	function toolLabel(tool: ToolActivity): string {
		switch (tool.name) {
			case 'search_diaries':
				return $t('assistant.toolSearchDiaries');
			case 'get_diary':
				return $t('assistant.toolGetDiary');
			case 'list_diaries':
				return $t('assistant.toolListDiaries');
			case 'search_diaries_keyword':
				return $t('assistant.toolKeywordSearch');
			case 'get_days':
				return $t('assistant.toolGetDays');
			default:
				return tool.name;
		}
	}

	function toolArgHint(tool: ToolActivity): string {
		if (!tool.args) return '';
		const parts: string[] = [];
		const q = tool.args.query;
		if (typeof q === 'string' && q.trim()) parts.push(`"${q.trim()}"`);
		const d = tool.args.date;
		if (typeof d === 'string' && d.trim()) parts.push(d.trim());
		const start = tool.args.start_date;
		const end = tool.args.end_date;
		if (typeof start === 'string' && start && typeof end === 'string' && end) {
			parts.push(`${start} ~ ${end}`);
		} else if (typeof start === 'string' && start) {
			parts.push(`${start} →`);
		} else if (typeof end === 'string' && end) {
			parts.push(`→ ${end}`);
		}
		return parts.join(' · ');
	}

	async function handleCopyMessage() {
		const ok = await copyText(message.content);
		if (ok) {
			copied = true;
			setTimeout(() => (copied = false), 1800);
		}
	}

	async function handleMarkdownClick(e: MouseEvent) {
		const target = e.target as HTMLElement | null;
		if (!target) return;

		const copyBtn = target.closest<HTMLButtonElement>('[data-copy-code]');
		if (copyBtn) {
			const encoded = copyBtn.dataset.copyCode || '';
			try {
				const ok = await copyText(decodeURIComponent(encoded));
				if (ok) {
					const prev = copyBtn.textContent;
					copyBtn.textContent = $t('assistant.copied');
					copyBtn.classList.add('is-copied');
					setTimeout(() => {
						copyBtn.textContent = prev || $t('assistant.copy');
						copyBtn.classList.remove('is-copied');
					}, 1600);
				}
			} catch {
				// Ignore decode error
			}
			return;
		}

		const dateChip = target.closest<HTMLButtonElement>('[data-diary-date]');
		if (dateChip?.dataset.diaryDate) {
			dispatch('previewDiary', dateChip.dataset.diaryDate);
		}
	}

	async function toggleRefs() {
		refsExpanded = !refsExpanded;
		const ids = message.referenced_diaries;
		if (refsExpanded && ids && ids.length > 0 && (!message.references || message.references.length === 0)) {
			const key = ids.join(',');
			if (refsLoadedForKey === key) return;
			refsLoading = true;
			try {
				const diaries = await getDiariesByIds(ids);
				loadedRefs = diaries.map((d) => ({
					id: d.id ?? d.date,
					date: d.date.slice(0, 10),
					mood: d.mood,
					weather: d.weather,
					preview: stripHtmlPreview(d.content, 110)
				}));
				refsLoadedForKey = key;
			} catch (err) {
				console.error('Failed to load referenced diaries:', err);
			} finally {
				refsLoading = false;
			}
		}
	}

	async function startEdit() {
		editDraft = message.content;
		isEditing = true;
		await tick();
		editEl?.focus();
	}

	function cancelEdit() {
		isEditing = false;
		editDraft = '';
	}

	function submitEdit() {
		const trimmed = editDraft.trim();
		if (!trimmed) return;
		isEditing = false;
		dispatch('edit', { id: message.id, content: trimmed });
	}
</script>

{#if message.role === 'user'}
	<div class="group flex flex-col items-end">
		{#if isEditing}
			<div class="w-full max-w-2xl rounded-2xl border border-primary/50 bg-background p-3 shadow-sm">
				<textarea
					bind:this={editEl}
					bind:value={editDraft}
					rows="3"
					class="w-full resize-none bg-transparent text-sm leading-6 text-foreground focus:outline-none"
				></textarea>
				<div class="flex items-center justify-end gap-2 mt-2">
					<button
						type="button"
						on:click={cancelEdit}
						class="px-3 py-1.5 rounded-lg text-xs text-muted-foreground hover:bg-muted transition-colors"
					>
						{$t('assistant.cancel')}
					</button>
					<button
						type="button"
						on:click={submitEdit}
						disabled={!editDraft.trim()}
						class="px-3 py-1.5 rounded-lg text-xs font-medium bg-primary text-primary-foreground hover:opacity-90 disabled:opacity-40"
					>
						{$t('assistant.saveAndSend')}
					</button>
				</div>
			</div>
		{:else}
			<div class="max-w-[85%] sm:max-w-[75%] rounded-2xl rounded-br-md px-4 py-2.5 bg-primary text-primary-foreground shadow-sm">
				<div class="whitespace-pre-wrap break-words text-sm leading-relaxed">
					{message.content}
				</div>
			</div>

			<div class="flex items-center gap-1.5 mt-1 px-1 opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity">
				{#if message.created}
					<span class="text-[11px] text-muted-foreground/70 mr-1">{formatTime(message.created)}</span>
				{/if}
				<button
					type="button"
					on:click={handleCopyMessage}
					class="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/70 transition-colors"
					title={copied ? $t('assistant.copied') : $t('assistant.copy')}
				>
					{#if copied}
						<svg class="w-3.5 h-3.5 text-emerald-600 dark:text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
						</svg>
					{:else}
						<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
						</svg>
					{/if}
				</button>
				{#if canEdit && !message.id.startsWith('temp-')}
					<button
						type="button"
						on:click={startEdit}
						class="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/70 transition-colors"
						title={$t('assistant.edit')}
					>
						<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
						</svg>
					</button>
				{/if}
			</div>
		{/if}
	</div>
{:else}
	<!-- Assistant message: clean editorial full-width prose -->
	<div class="group flex items-start gap-3">
		<div
			class="w-7 h-7 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-center flex-shrink-0 mt-0.5"
			aria-hidden="true"
		>
			<svg class="w-3.5 h-3.5 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M5 3v4M3 5h4M6 17v4m-2-2h4m5-16l2.286 6.857L21 12l-5.714 2.143L13 21l-2.286-6.857L5 12l5.714-2.143L13 3z"
				/>
			</svg>
		</div>

		<div class="flex-1 min-w-0 space-y-2.5">
			<!-- Tool activity pills -->
			{#if message.tools && message.tools.length > 0}
				<div class="flex flex-wrap gap-1.5">
					{#each message.tools as tool (tool.id)}
						<div
							class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs border
								{tool.status === 'error'
									? 'border-red-500/30 bg-red-500/5 text-red-600 dark:text-red-400'
									: 'border-border/70 bg-muted/40 text-muted-foreground'}"
						>
							{#if tool.status === 'running'}
								<svg class="w-3 h-3 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
								</svg>
							{:else if tool.status === 'error'}
								<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
								</svg>
							{:else}
								<svg class="w-3 h-3 text-emerald-600 dark:text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
								</svg>
							{/if}
							<span class="font-medium text-foreground/85">{toolLabel(tool)}</span>
							{#if toolArgHint(tool)}
								<span class="text-muted-foreground truncate max-w-[180px]">{toolArgHint(tool)}</span>
							{/if}
							{#if tool.status === 'done' && typeof tool.count === 'number' && tool.name !== 'get_days'}
								<span class="text-[11px] px-1.5 py-0.5 rounded bg-background/80 text-muted-foreground">
									{$t('assistant.toolFoundCount', { count: tool.count })}
								</span>
							{/if}
						</div>
					{/each}
				</div>
			{/if}

			<!-- Collapsible thinking process -->
			{#if message.thinking}
				<div class="rounded-xl border border-border/60 bg-muted/30 overflow-hidden">
					<button
						type="button"
						on:click={() => (thinkingExpanded = !thinkingExpanded)}
						class="w-full flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground hover:text-foreground transition-colors"
					>
						<svg
							class="w-3.5 h-3.5 transition-transform {(thinkingExpanded || isActivelyThinking) ? 'rotate-90' : ''}"
							fill="none"
							stroke="currentColor"
							viewBox="0 0 24 24"
						>
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
						</svg>
						{#if isActivelyThinking}
							<span class="animate-pulse font-medium">{$t('assistant.thinking')}</span>
						{:else if message.thinkingDurationSec}
							<span class="font-medium">{$t('assistant.thoughtDuration', { seconds: message.thinkingDurationSec })}</span>
						{:else}
							<span class="font-medium">{$t('assistant.thoughtProcess')}</span>
						{/if}
					</button>
					{#if thinkingExpanded || isActivelyThinking}
						<div class="px-3.5 pb-3 pt-1 text-xs text-muted-foreground/90 whitespace-pre-wrap leading-relaxed border-t border-border/40 max-h-64 overflow-y-auto font-mono">
							{message.thinking}
						</div>
					{/if}
				</div>
			{/if}

			<!-- Main markdown content -->
			{#if renderedBlocks.length > 0}
				<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
				<div
					on:click={handleMarkdownClick}
					class="chat-markdown text-sm text-foreground break-words"
				>
					{#each renderedBlocks as blockHtml, idx (idx)}
						{@html blockHtml}
					{/each}
					{#if isStreaming}
						<span class="inline-block w-1.5 h-4 ml-0.5 align-middle bg-primary/70 animate-pulse rounded-sm"></span>
					{/if}
				</div>
			{:else if isStreaming}
				<div class="flex items-center gap-2 py-1 text-xs text-muted-foreground">
					<span class="flex gap-1">
						<span class="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce [animation-delay:-0.2s]"></span>
						<span class="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce [animation-delay:-0.1s]"></span>
						<span class="w-1.5 h-1.5 rounded-full bg-primary/60 animate-bounce"></span>
					</span>
					<span>
						{message.tools?.some((tl) => tl.status === 'running')
							? $t('assistant.toolRunning')
							: $t('assistant.thinking')}
					</span>
				</div>
			{/if}

			<!-- Referenced diaries disclosure -->
			{#if refCount > 0}
				<div class="pt-0.5">
					<button
						type="button"
						on:click={toggleRefs}
						class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs text-muted-foreground
							bg-muted/45 hover:bg-muted/80 hover:text-foreground border border-border/50 transition-colors"
					>
						<svg class="w-3.5 h-3.5 text-primary/80" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
								d="M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253" />
						</svg>
						<span>
							{refCount === 1
								? $t('assistant.referencedCount', { count: refCount })
								: $t('assistant.referencedCountPlural', { count: refCount })}
						</span>
						<svg class="w-3 h-3 transition-transform {refsExpanded ? 'rotate-180' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
						</svg>
					</button>

					{#if refsExpanded}
						<div class="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-2">
							{#if refsLoading}
								<div class="text-xs text-muted-foreground py-1">{$t('common.loading')}</div>
							{:else}
								{#each displayRefs as ref (ref.id)}
									<button
										type="button"
										on:click={() => dispatch('previewDiary', ref.date)}
										class="text-left p-2.5 rounded-xl bg-background/70 border border-border/60
											hover:border-primary/40 hover:bg-background transition-all"
									>
										<div class="flex items-center justify-between gap-2">
											<span class="text-xs font-medium text-foreground">{formatDiaryDate(ref.date)}</span>
											{#if ref.mood || ref.weather}
												<span class="text-[11px] text-muted-foreground truncate">
													{[ref.mood, ref.weather].filter(Boolean).join(' · ')}
												</span>
											{/if}
										</div>
										{#if ref.preview}
											<p class="text-xs text-muted-foreground mt-1 line-clamp-2 leading-relaxed">
												{ref.preview}
											</p>
										{/if}
									</button>
								{/each}
							{/if}
						</div>
					{/if}
				</div>
			{/if}

			<!-- Assistant action bar -->
			{#if !isStreaming && message.content}
				<div class="flex items-center gap-1 pt-0.5 opacity-80 sm:opacity-0 sm:group-hover:opacity-100 focus-within:opacity-100 transition-opacity">
					<button
						type="button"
						on:click={handleCopyMessage}
						class="inline-flex items-center gap-1 px-2 py-1 rounded-md text-xs text-muted-foreground
							hover:text-foreground hover:bg-muted/70 transition-colors"
						title={copied ? $t('assistant.copied') : $t('assistant.copy')}
					>
						{#if copied}
							<svg class="w-3.5 h-3.5 text-emerald-600 dark:text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
							</svg>
							<span class="text-emerald-600 dark:text-emerald-400">{$t('assistant.copied')}</span>
						{:else}
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
									d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
							</svg>
							<span>{$t('assistant.copy')}</span>
						{/if}
					</button>

					{#if canRegenerate && !message.id.startsWith('temp-')}
						<button
							type="button"
							on:click={() => dispatch('regenerate', message.id)}
							class="inline-flex items-center gap-1 px-2 py-1 rounded-md text-xs text-muted-foreground
								hover:text-foreground hover:bg-muted/70 transition-colors"
							title={$t('assistant.regenerate')}
						>
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
									d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
							</svg>
							<span>{$t('assistant.regenerate')}</span>
						</button>
					{/if}

					{#if message.created}
						<span class="text-[11px] text-muted-foreground/60 ml-1">{formatTime(message.created)}</span>
					{/if}
				</div>
			{/if}
		</div>
	</div>
{/if}

<style>
	.chat-markdown :global(h1) {
		font-size: 1.25rem;
		font-weight: 700;
		margin-top: 1.15rem;
		margin-bottom: 0.5rem;
		line-height: 1.35;
	}
	.chat-markdown :global(h2) {
		font-size: 1.1rem;
		font-weight: 600;
		margin-top: 1rem;
		margin-bottom: 0.4rem;
		line-height: 1.4;
	}
	.chat-markdown :global(h3) {
		font-size: 0.975rem;
		font-weight: 600;
		margin-top: 0.85rem;
		margin-bottom: 0.3rem;
	}
	.chat-markdown :global(p) {
		margin-bottom: 0.65rem;
		line-height: 1.7;
	}
	.chat-markdown :global(p:last-child) {
		margin-bottom: 0;
	}
	.chat-markdown :global(ul),
	.chat-markdown :global(ol) {
		margin-left: 1.35rem;
		margin-bottom: 0.65rem;
	}
	.chat-markdown :global(ul) {
		list-style-type: disc;
	}
	.chat-markdown :global(ol) {
		list-style-type: decimal;
	}
	.chat-markdown :global(li) {
		margin-bottom: 0.3rem;
		line-height: 1.65;
	}
	.chat-markdown :global(strong) {
		font-weight: 600;
	}
	.chat-markdown :global(blockquote) {
		border-left: 3px solid hsl(var(--primary) / 0.4);
		padding: 0.15rem 0 0.15rem 0.85rem;
		margin: 0.75rem 0;
		color: hsl(var(--muted-foreground));
	}
	.chat-markdown :global(a) {
		color: hsl(var(--primary));
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	.chat-markdown :global(hr) {
		border: none;
		border-top: 1px solid hsl(var(--border));
		margin: 1rem 0;
	}
	.chat-markdown :global(:not(pre) > code) {
		background-color: hsl(var(--muted));
		padding: 0.12rem 0.38rem;
		border-radius: 0.35rem;
		font-size: 0.85em;
		font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
	}
	.chat-markdown :global(.chat-code-block) {
		margin: 0.75rem 0;
		border-radius: 0.75rem;
		border: 1px solid hsl(var(--border));
		overflow: hidden;
		background-color: hsl(25 25% 12%);
		color: hsl(45 25% 92%);
	}
	.chat-markdown :global(.chat-code-header) {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.4rem 0.75rem;
		background-color: hsl(25 22% 16%);
		border-bottom: 1px solid hsl(30 20% 22%);
		font-size: 0.72rem;
	}
	.chat-markdown :global(.chat-code-lang) {
		font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
		color: hsl(40 15% 72%);
		text-transform: lowercase;
	}
	.chat-markdown :global(.chat-code-copy) {
		padding: 0.15rem 0.5rem;
		border-radius: 0.375rem;
		color: hsl(40 20% 82%);
		background: transparent;
		cursor: pointer;
		transition: background-color 0.15s, color 0.15s;
	}
	.chat-markdown :global(.chat-code-copy:hover) {
		background-color: hsl(30 20% 24%);
		color: #fff;
	}
	.chat-markdown :global(.chat-code-copy.is-copied) {
		color: #6ee7b7;
	}
	.chat-markdown :global(.chat-code-block pre) {
		margin: 0;
		padding: 0.85rem 1rem;
		overflow-x: auto;
		font-size: 0.82rem;
		line-height: 1.6;
		font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
	}
	.chat-markdown :global(.chat-code-block pre code) {
		background: transparent;
		padding: 0;
	}
	/* Lowlight / highlight.js token colors suited for warm dark code block */
	.chat-markdown :global(.hljs-keyword),
	.chat-markdown :global(.hljs-selector-tag),
	.chat-markdown :global(.hljs-built_in) {
		color: #f5b971;
	}
	.chat-markdown :global(.hljs-string),
	.chat-markdown :global(.hljs-attr) {
		color: #a3d9a5;
	}
	.chat-markdown :global(.hljs-number),
	.chat-markdown :global(.hljs-literal) {
		color: #e8a2af;
	}
	.chat-markdown :global(.hljs-title),
	.chat-markdown :global(.hljs-function) {
		color: #8cc4ff;
	}
	.chat-markdown :global(.hljs-comment) {
		color: #9e9183;
		font-style: italic;
	}

	/* Tables */
	.chat-markdown :global(.chat-table-wrap) {
		overflow-x: auto;
		margin: 0.75rem 0;
		border-radius: 0.65rem;
		border: 1px solid hsl(var(--border));
	}
	.chat-markdown :global(table) {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.82rem;
	}
	.chat-markdown :global(th),
	.chat-markdown :global(td) {
		padding: 0.5rem 0.75rem;
		border-bottom: 1px solid hsl(var(--border) / 0.7);
		text-align: left;
	}
	.chat-markdown :global(th) {
		background-color: hsl(var(--muted) / 0.65);
		font-weight: 600;
	}
	.chat-markdown :global(tr:last-child td) {
		border-bottom: none;
	}

	/* Interactive inline diary date chips */
	.chat-markdown :global(.diary-date-chip) {
		display: inline-flex;
		align-items: center;
		padding: 0.05rem 0.45rem;
		margin: 0 0.1rem;
		border-radius: 0.4rem;
		font-size: 0.82em;
		font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
		font-weight: 500;
		background-color: hsl(var(--primary) / 0.1);
		color: hsl(var(--primary));
		border: 1px solid hsl(var(--primary) / 0.22);
		cursor: pointer;
		vertical-align: baseline;
		transition: background-color 0.15s, border-color 0.15s;
	}
	.chat-markdown :global(.diary-date-chip:hover) {
		background-color: hsl(var(--primary) / 0.18);
		border-color: hsl(var(--primary) / 0.45);
	}
</style>
