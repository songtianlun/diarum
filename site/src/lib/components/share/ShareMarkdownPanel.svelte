<script lang="ts">
	import { onMount } from 'svelte';
	import { marked } from 'marked';
	import { htmlToMarkdown } from '$lib/utils/htmlToMarkdown';
	import { copyText } from '$lib/utils/clipboard';
	import { formatDisplayDate, getDayOfWeek } from '$lib/utils/date';

	export let date: string;
	/** Entry HTML (or the selected part of it). */
	export let content: string;
	export let mood = '';
	export let weather = '';
	export let tags: string[] = [];
	export let isSelection = false;

	interface MarkdownShareOptions {
		date: boolean;
		meta: boolean;
		tags: boolean;
		images: boolean;
	}

	const OPTIONS_KEY = 'diarum.share.markdown';
	let options: MarkdownShareOptions = { date: true, meta: true, tags: true, images: true };
	let view: 'source' | 'preview' = 'source';
	let text = '';
	let generated = '';
	let copyState: 'idle' | 'copied' | 'failed' = 'idle';
	let copyTimer: ReturnType<typeof setTimeout> | undefined;
	let textarea: HTMLTextAreaElement;

	onMount(() => {
		try {
			const stored = JSON.parse(localStorage.getItem(OPTIONS_KEY) ?? 'null');
			if (stored && typeof stored === 'object') options = { ...options, ...stored };
		} catch {
			// Use defaults.
		}
		return () => clearTimeout(copyTimer);
	});

	function toggle(key: keyof MarkdownShareOptions) {
		options = { ...options, [key]: !options[key] };
		try {
			localStorage.setItem(OPTIONS_KEY, JSON.stringify(options));
		} catch {
			// Not persisted; still applies now.
		}
	}

	function toHtml(raw: string): string {
		const trimmed = raw.trim();
		// Entries are HTML; very old ones may be Markdown already.
		return trimmed.startsWith('<') ? trimmed : (marked.parse(trimmed) as string);
	}

	function buildMarkdown(): string {
		const parts: string[] = [];
		if (options.date && date) parts.push(`# ${formatDisplayDate(date)} ${getDayOfWeek(date)}`);
		const meta = [weather, mood].filter(Boolean).join(' ');
		if (options.meta && meta) parts.push(meta);
		const body = htmlToMarkdown(toHtml(content ?? ''), {
			baseUrl: window.location.origin,
			includeImages: options.images
		});
		if (body) parts.push(body);
		const tagLine = tags.filter(Boolean).map((tag) => `#${tag.replace(/\s+/g, '-')}`).join(' ');
		if (options.tags && tagLine) parts.push(tagLine);
		return parts.join('\n\n');
	}

	// Regenerate when inputs change; this replaces manual edits, which the
	// Reset button makes explicit.
	$: {
		options, content, date, mood, weather, tags;
		if (typeof window !== 'undefined') {
			generated = buildMarkdown();
			text = generated;
		}
	}
	$: edited = text !== generated;
	$: charCount = [...text].length;
	$: previewHtml = view === 'preview' ? (marked.parse(text, { breaks: false, gfm: true }) as string) : '';

	async function handleCopy() {
		// Copy the selection when the user highlighted part of the source.
		const hasSelection = view === 'source' && textarea && textarea.selectionStart !== textarea.selectionEnd;
		const value = hasSelection ? text.slice(textarea.selectionStart, textarea.selectionEnd) : text;
		const ok = await copyText(value);
		copyState = ok ? 'copied' : 'failed';
		if (!ok && view === 'source') {
			textarea?.focus();
			textarea?.select();
		}
		clearTimeout(copyTimer);
		copyTimer = setTimeout(() => (copyState = 'idle'), 1800);
	}

	function handleDownload() {
		const blob = new Blob([text], { type: 'text/markdown;charset=utf-8' });
		const url = URL.createObjectURL(blob);
		const link = document.createElement('a');
		link.href = url;
		link.download = `diary-${date}.md`;
		link.click();
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	}

	const chips: { key: keyof MarkdownShareOptions; label: string }[] = [
		{ key: 'date', label: 'Date' },
		{ key: 'meta', label: 'Mood & weather' },
		{ key: 'tags', label: 'Tags' },
		{ key: 'images', label: 'Images' }
	];
</script>

<div class="md-panel">
	<div class="md-toolbar">
		<div class="md-chips" role="group" aria-label="Include in Markdown">
			{#each chips as chip}
				<button
					type="button"
					class="md-chip"
					class:on={options[chip.key]}
					aria-pressed={options[chip.key]}
					on:click={() => toggle(chip.key)}
				>
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
					{chip.label}
				</button>
			{/each}
		</div>
		<div class="md-view" role="tablist" aria-label="Markdown view">
			<button type="button" role="tab" aria-selected={view === 'source'} class:active={view === 'source'} on:click={() => (view = 'source')}>Source</button>
			<button type="button" role="tab" aria-selected={view === 'preview'} class:active={view === 'preview'} on:click={() => (view = 'preview')}>Preview</button>
		</div>
	</div>

	<div class="md-body">
		{#if view === 'source'}
			<textarea
				bind:this={textarea}
				bind:value={text}
				class="md-source"
				spellcheck="false"
				aria-label="Markdown source"
			></textarea>
		{:else}
			<div class="md-preview tiptap-editor-content">{@html previewHtml}</div>
		{/if}
	</div>

	<div class="md-footer">
		<div class="md-meta">
			{#if isSelection}
				<span class="md-badge">Selected text</span>
			{/if}
			<span class="tabular-nums">{charCount.toLocaleString()} characters</span>
			{#if edited}
				<span class="md-dot">·</span>
				<button type="button" class="md-link" on:click={() => (text = generated)}>Reset edits</button>
			{/if}
		</div>
		<div class="md-actions">
			<button type="button" class="md-btn" on:click={handleDownload}>
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" /></svg>
				<span>.md</span>
			</button>
			<button type="button" class="md-btn primary" class:copied={copyState === 'copied'} on:click={handleCopy} disabled={!text}>
				{#if copyState === 'copied'}
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
					Copied
				{:else}
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2" /><path stroke-linecap="round" d="M5 15V6a2 2 0 012-2h9" /></svg>
					Copy Markdown
				{/if}
			</button>
		</div>
	</div>
	{#if copyState === 'failed'}
		<div class="md-copy-failed" role="alert">Couldn't copy automatically. The text is selected, press Ctrl+C (⌘C) to copy.</div>
	{/if}
</div>

<style>
	.md-panel {
		display: flex;
		flex-direction: column;
		min-height: 0;
		height: 100%;
	}

	.md-toolbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 10px 16px;
		border-bottom: 1px solid hsl(var(--border) / 0.5);
	}

	.md-chips {
		display: flex;
		gap: 6px;
		overflow-x: auto;
		scrollbar-width: none;
		-webkit-overflow-scrolling: touch;
	}

	.md-chips::-webkit-scrollbar {
		display: none;
	}

	.md-chip {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		flex-shrink: 0;
		padding: 5px 11px 5px 9px;
		border: 1px solid hsl(var(--border));
		border-radius: 999px;
		background: transparent;
		color: hsl(var(--muted-foreground));
		font-size: 12.5px;
		font-weight: 500;
		cursor: pointer;
		transition: background 0.15s ease, color 0.15s ease, border-color 0.15s ease;
	}

	.md-chip svg {
		width: 12px;
		height: 12px;
		opacity: 0;
		margin-right: -4px;
		transform: scale(0.6);
		transition: opacity 0.15s ease, transform 0.15s ease, margin 0.15s ease;
	}

	.md-chip.on {
		border-color: hsl(var(--primary) / 0.5);
		background: hsl(var(--primary) / 0.1);
		color: hsl(var(--foreground));
	}

	.md-chip.on svg {
		opacity: 1;
		margin-right: 0;
		transform: none;
		color: hsl(var(--primary));
	}

	.md-view {
		display: inline-flex;
		flex-shrink: 0;
		padding: 2px;
		border-radius: 8px;
		background: hsl(var(--muted) / 0.7);
	}

	.md-view button {
		padding: 4px 10px;
		border: 0;
		border-radius: 6px;
		background: transparent;
		color: hsl(var(--muted-foreground));
		font-size: 12.5px;
		font-weight: 500;
		cursor: pointer;
		transition: background 0.15s ease, color 0.15s ease;
	}

	.md-view button.active {
		background: hsl(var(--background));
		color: hsl(var(--foreground));
		box-shadow: 0 1px 2px hsl(var(--foreground) / 0.1);
	}

	.md-body {
		flex: 1;
		min-height: 0;
		display: flex;
		background: hsl(var(--muted) / 0.3);
	}

	.md-source {
		flex: 1;
		width: 100%;
		min-height: 260px;
		padding: 16px 18px;
		border: 0;
		outline: none;
		resize: none;
		background: transparent;
		color: hsl(var(--foreground));
		font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
		font-size: 13.5px;
		line-height: 1.65;
		tab-size: 2;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}

	.md-preview {
		flex: 1;
		min-height: 260px;
		overflow-y: auto;
		padding: 16px 20px;
	}

	.md-footer {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 10px 16px;
		padding-bottom: max(10px, env(safe-area-inset-bottom));
		border-top: 1px solid hsl(var(--border) / 0.5);
	}

	.md-meta {
		display: flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
		font-size: 12.5px;
		color: hsl(var(--muted-foreground));
	}

	.md-badge {
		padding: 1px 7px;
		border-radius: 999px;
		background: hsl(var(--primary) / 0.1);
		color: hsl(var(--primary));
		font-weight: 500;
	}

	.md-link {
		padding: 0;
		border: 0;
		background: transparent;
		color: hsl(var(--primary));
		font-size: inherit;
		cursor: pointer;
	}

	.md-link:hover {
		text-decoration: underline;
	}

	.md-actions {
		display: flex;
		gap: 8px;
		flex-shrink: 0;
	}

	.md-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		padding: 8px 14px;
		border: 1px solid hsl(var(--border));
		border-radius: 8px;
		background: hsl(var(--secondary));
		color: hsl(var(--secondary-foreground));
		font-size: 14px;
		font-weight: 500;
		cursor: pointer;
		transition: opacity 0.15s ease, background 0.2s ease, transform 0.1s ease;
	}

	.md-btn svg {
		width: 16px;
		height: 16px;
	}

	.md-btn:hover:not(:disabled) {
		opacity: 0.9;
	}

	.md-btn:active:not(:disabled) {
		transform: scale(0.97);
	}

	.md-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.md-btn.primary {
		min-width: 150px;
		border-color: hsl(var(--primary));
		background: hsl(var(--primary));
		color: hsl(var(--primary-foreground));
	}

	.md-btn.primary.copied {
		border-color: hsl(142 71% 40%);
		background: hsl(142 71% 40%);
		color: #fff;
	}

	.md-copy-failed {
		padding: 8px 16px;
		font-size: 12.5px;
		color: hsl(var(--destructive, 0 84% 60%));
		border-top: 1px solid hsl(var(--border) / 0.5);
	}

	@media (max-width: 640px) {
		.md-toolbar {
			flex-direction: column;
			align-items: stretch;
			gap: 8px;
			padding: 10px 12px;
		}

		.md-view {
			align-self: flex-start;
		}

		.md-source,
		.md-preview {
			padding: 12px 14px;
			font-size: 13px;
		}

		.md-footer {
			flex-direction: column;
			align-items: stretch;
			gap: 8px;
			padding: 10px 12px;
			padding-bottom: max(10px, env(safe-area-inset-bottom));
		}

		.md-actions {
			width: 100%;
		}

		.md-btn.primary {
			flex: 1;
		}
	}
</style>
