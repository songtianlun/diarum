<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { Editor } from '@tiptap/core';
	import StarterKit from '@tiptap/starter-kit';
	import Placeholder from '@tiptap/extension-placeholder';
	import { ImageExtension } from './ImageNodeView';
	import Link from '@tiptap/extension-link';
	import Underline from '@tiptap/extension-underline';
	import Highlight from '@tiptap/extension-highlight';
	import TaskList from '@tiptap/extension-task-list';
	import TaskItem from '@tiptap/extension-task-item';
	import CharacterCount from '@tiptap/extension-character-count';
	import Typography from '@tiptap/extension-typography';
	import CodeBlockLowlight from '@tiptap/extension-code-block-lowlight';
	import Focus from '@tiptap/extension-focus';
	import { common, createLowlight } from 'lowlight';
	import { DOMSerializer, type Fragment } from '@tiptap/pm/model';
	import { validateUploadSource } from '$lib/utils/uploadImage';
	import { groupLiveFiles, isImageFile, isVideoFile } from '$lib/utils/livePhoto';
	import { t } from '$lib/i18n';
	import { SlashCommands } from './SlashCommands';
	import { getSuggestionItems, setImageUploadTrigger, setGalleryPickerTrigger, setImageUrlTrigger } from './commands';
	import { suggestionRenderer, showCommandMenu } from './suggestionRenderer';
	import MediaPicker from './MediaPicker.svelte';
	import ImageUrlDialog from './ImageUrlDialog.svelte';
	import { UploadQueue, type UploadNotice } from './uploadQueue';
	import type { ImageInsert } from './ImageNodeView';

	export let content = '';
	export let onChange: (value: string) => void = () => {};
	export let placeholder = 'Start writing...';
	export let selectedContent: string = '';
	export let emptyStatePrompt: string = '';

	let editorElement: HTMLDivElement;
	let editor: Editor | null = null;
	let fileInput: HTMLInputElement;
	let uploadError = '';
	let uploadErrorTone: 'error' | 'info' = 'error';
	let uploadErrorTimer: ReturnType<typeof setTimeout> | undefined;
	let showMediaPicker = false;
	let showImageUrlDialog = false;
	// Where to insert once a dialog closes; the dialog takes focus from the editor.
	let insertPos: number | null = null;
	let isFocused = false;
	let uploadingCount = 0;

	// The HTML last handed to onChange, so our own edits echoing back through
	// the `content` prop are not mistaken for an external replacement.
	let lastEmitted: string | null = null;

	// Add button state
	let showAddButton = false;
	let addButtonTop = 0;

	const lowlight = createLowlight(common);

	// Final images of uploads whose placeholder was undone and may come back.
	const finishedUploads = new Map<string, ImageInsert>();

	const uploadQueue = new UploadQueue({
		onSettled: () => scheduleReconcile(),
		onActivity: () => {
			uploadingCount = uploadQueue.busyCount;
		},
		onNotice: (notice) => showUploadError(noticeText(notice), 'info'),
	});

	function noticeText(notice: UploadNotice): string {
		switch (notice.kind) {
			case 'liveDroppedChevereto':
				return $t('live.droppedChevereto');
			case 'liveVideoTooLarge':
				return $t('live.videoTooLarge', { name: notice.fileName });
			case 'liveVideoInvalid':
				return $t('live.videoInvalid', { name: notice.fileName });
		}
	}

	/** Files an upload can start from: images, and videos that may pair with one. */
	function uploadableFiles(files: File[]): File[] {
		return files.filter((file) => isImageFile(file) || isVideoFile(file));
	}

	// Upload placeholders only exist in this editor session; their blob: URLs
	// must never reach the saved entry.
	const PENDING_IMAGE = /<img\b[^>]*\bdata-uploading="true"[^>]*>/g;
	function stripPendingImages(html: string): string {
		return html.includes('data-uploading') ? html.replace(PENDING_IMAGE, '') : html;
	}

	function showUploadError(message: string, tone: 'error' | 'info' = 'error') {
		uploadError = message;
		uploadErrorTone = tone;
		clearTimeout(uploadErrorTimer);
		uploadErrorTimer = setTimeout(() => (uploadError = ''), 4000);
	}

	let reconcileScheduled = false;
	function scheduleReconcile() {
		if (reconcileScheduled) return;
		reconcileScheduled = true;
		queueMicrotask(() => {
			reconcileScheduled = false;
			reconcilePlaceholders();
		});
	}

	/**
	 * Brings upload placeholders in line with their tasks: finished uploads
	 * become real images, and placeholders without a task (from undo/redo or
	 * a stale save) are resolved or dropped.
	 */
	function reconcilePlaceholders() {
		if (!editor || editor.isDestroyed) return;
		const { state } = editor;
		const imageType = state.schema.nodes.image;
		const changes: { pos: number; size: number; attrs: Record<string, any>; image?: ImageInsert }[] = [];

		state.doc.descendants((node, pos) => {
			if (node.type !== imageType || node.attrs['data-uploading'] !== 'true') return;
			const id = node.attrs['data-placeholder-id'] as string | null;
			const task = id ? uploadQueue.get(id) : undefined;
			if (id && task?.status === 'done' && task.url) {
				const image: ImageInsert = { src: task.url, alt: task.fileName, live: task.liveUrl };
				finishedUploads.set(id, image);
				uploadQueue.release(id);
				changes.push({ pos, size: node.nodeSize, attrs: node.attrs, image });
			} else if (!task) {
				changes.push({ pos, size: node.nodeSize, attrs: node.attrs, image: id ? finishedUploads.get(id) : undefined });
			}
		});
		if (changes.length === 0) return;

		const tr = state.tr;
		// Back to front, so deletions don't shift the positions still to visit.
		for (const change of changes.reverse()) {
			if (change.image) {
				tr.setNodeMarkup(change.pos, undefined, {
					...change.attrs,
					src: change.image.src,
					alt: change.image.alt || null,
					'data-live-video': change.image.live || null,
					'data-uploading': null,
					'data-placeholder-id': null,
				});
			} else {
				tr.delete(change.pos, change.pos + change.size);
			}
		}
		tr.setMeta('addToHistory', false);
		editor.view.dispatch(tr);
	}

	/**
	 * Uploads the images among `files`, showing a placeholder for each right
	 * away. A live photo given as a still and its video becomes one upload.
	 */
	function uploadFiles(files: File[], at?: number) {
		if (!editor) return;
		const { sources, strayVideos } = groupLiveFiles(uploadableFiles(files));
		if (sources.length === 0 && strayVideos.length === 0) return;

		const rejected: string[] = strayVideos.map((video) => $t('live.strayVideo', { name: video.name || 'Video' }));
		const placeholders: { id: string; src: string; alt: string }[] = [];
		for (const source of sources) {
			const invalid = validateUploadSource(source.file);
			if (invalid) {
				rejected.push(`${source.file.name || 'Image'}: ${invalid}`);
				continue;
			}
			const id = uploadQueue.add(source);
			placeholders.push({ id, src: uploadQueue.get(id)!.previewUrl, alt: source.file.name });
		}

		if (placeholders.length > 0) {
			const chain = editor.chain().focus();
			if (typeof at === 'number') chain.setTextSelection(at);
			chain.insertUploadPlaceholders(placeholders).run();
		}
		if (rejected.length > 0) {
			showUploadError(rejected.length === 1 ? rejected[0] : `${rejected.length} images skipped. ${rejected[0]}`);
		}
	}

	// Handle paste event
	function handlePaste(_view: any, event: ClipboardEvent) {
		const files = uploadableFiles(Array.from(event.clipboardData?.files ?? []));
		if (files.length === 0) return false;
		event.preventDefault();
		uploadFiles(files);
		return true;
	}

	// Handle drop event
	function handleDrop(view: any, event: DragEvent, _slice: unknown, moved: boolean) {
		if (moved) return false;
		const files = uploadableFiles(Array.from(event.dataTransfer?.files ?? []));
		if (files.length === 0) return false;
		event.preventDefault();
		const dropPos = view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos;
		uploadFiles(files, dropPos);
		return true;
	}

	// Handle slash command image trigger
	function handleSlashImage() {
		fileInput?.click();
	}

	// Handle gallery picker trigger
	function handleGalleryPicker() {
		showMediaPicker = true;
	}

	function handleImageUrl() {
		insertPos = editor?.state.selection.from ?? null;
		showImageUrlDialog = true;
	}

	function insertImagesAtSavedPos(images: ImageInsert[]) {
		if (!editor || images.length === 0) return;
		const chain = editor.chain().focus();
		if (insertPos !== null && insertPos <= editor.state.doc.content.size) chain.setTextSelection(insertPos);
		chain.insertImages(images).run();
		insertPos = null;
	}

	function handleMediaSelect(images: ImageInsert[]) {
		if (!editor || images.length === 0) return;
		editor.chain().focus().insertImages(images).run();
	}

	function handleFileSelect(event: Event) {
		const input = event.target as HTMLInputElement;
		const files = Array.from(input.files ?? []);
		input.value = '';
		uploadFiles(files);
	}

	function handleBeforeUnload(event: BeforeUnloadEvent) {
		if (uploadQueue.busyCount > 0) {
			event.preventDefault();
			event.returnValue = '';
		}
	}

	// Serializing into the live document creates real <img> elements, which
	// the browser starts downloading at full size (editor.getHTML() does this
	// on every change). An inert document never loads resources.
	let inertDocument: Document | null = null;
	function serializeHtml(content: Fragment): string {
		if (!editor) return '';
		inertDocument ??= document.implementation.createHTMLDocument('');
		const container = inertDocument.createElement('div');
		container.appendChild(DOMSerializer.fromSchema(editor.schema).serializeFragment(content, { document: inertDocument }));
		return container.innerHTML;
	}

	function currentHtml(): string {
		return editor ? serializeHtml(editor.state.doc.content) : '';
	}

	// Get HTML of current selection
	function getSelectionHtml(): string {
		if (!editor) return '';
		const { from, to, empty } = editor.state.selection;
		if (empty) return '';
		return serializeHtml(editor.state.doc.slice(from, to).content);
	}

	// Update add button position based on cursor
	function updateAddButton() {
		if (!editor || !editorElement) {
			showAddButton = false;
			return;
		}

		const { selection } = editor.state;
		const { $from } = selection;
		const node = $from.parent;

		// Show only on empty paragraph
		if (node.type.name === 'paragraph' && node.content.size === 0) {
			const coords = editor.view.coordsAtPos($from.pos);
			const editorRect = editorElement.getBoundingClientRect();
			addButtonTop = coords.top - editorRect.top;
			showAddButton = true;
		} else {
			showAddButton = false;
		}
	}

	// Handle add button click
	let addButtonEl: HTMLButtonElement;
	function handleAddClick() {
		if (!editor || !addButtonEl) return;
		editor.commands.focus();
		showCommandMenu(editor, addButtonEl);
	}

	onMount(() => {
		// Register image upload trigger for slash commands
		setImageUploadTrigger(handleSlashImage);
		// Register gallery picker trigger for slash commands
		setGalleryPickerTrigger(handleGalleryPicker);
		setImageUrlTrigger(handleImageUrl);

		editor = new Editor({
			element: editorElement,
			extensions: [
				StarterKit.configure({
					codeBlock: false,
				}),
				Placeholder.configure({
					placeholder: ({ node }) => {
						if (node.type.name === 'paragraph') {
							return 'Type / to browse options';
						}
						return placeholder;
					},
					showOnlyCurrent: true,
				}),
				ImageExtension.configure({
					inline: false,
					allowBase64: true,
					getUploadQueue: () => uploadQueue,
				}),
				Focus.configure({
					className: 'has-focus',
					mode: 'all',
				}),
				Link.configure({
					openOnClick: false,
				}),
				Underline,
				Highlight.configure({
					multicolor: true,
				}),
				TaskList,
				TaskItem.configure({
					nested: true,
				}),
				CharacterCount,
				Typography,
				CodeBlockLowlight.configure({
					lowlight,
				}),
				SlashCommands.configure({
					suggestion: {
						items: ({ query }: { query: string }) => getSuggestionItems(query),
						render: () => suggestionRenderer,
					},
				}),
			],
			// Content is loaded right after creation instead: TipTap's first render
			// happens before node views are registered and would create plain
			// <img> tags, downloading every original image at full size.
			content: '',
			editorProps: {
				handlePaste,
				handleDrop,
				attributes: {
					class: 'tiptap-editor-content',
				},
			},
			onUpdate: ({ editor }) => {
				const html = stripPendingImages(currentHtml());
				if (html === lastEmitted) return;
				lastEmitted = html;
				onChange(html);
			},
			onTransaction: ({ transaction }) => {
				editor = editor;
				if (transaction.docChanged) scheduleReconcile();
				updateAddButton();
				selectedContent = getSelectionHtml();
			},
			onFocus: () => {
				isFocused = true;
			},
			onBlur: () => {
				isFocused = false;
			},
		});

		// Loaded outside undo history, so undo can never empty the entry.
		editor.chain().setMeta('addToHistory', false).setContent(content, false).run();

		// When the user deselects outside the editor, Tiptap's onTransaction
		// doesn't fire, so we rely on the native selectionchange event to clear.
		function handleDocumentSelectionChange() {
			if (!editorElement) return;
			const sel = window.getSelection();
			if (!sel || sel.isCollapsed || !editorElement.contains(sel.anchorNode)) {
				selectedContent = '';
			}
		}
		document.addEventListener('selectionchange', handleDocumentSelectionChange);
		window.addEventListener('beforeunload', handleBeforeUnload);
		// Content saved while an older version had upload placeholders.
		scheduleReconcile();

		return () => {
			document.removeEventListener('selectionchange', handleDocumentSelectionChange);
			window.removeEventListener('beforeunload', handleBeforeUnload);
		};
	});

	onDestroy(() => {
		// Cleanup image upload trigger
		setImageUploadTrigger(null);
		// Cleanup gallery picker trigger
		setGalleryPickerTrigger(null);
		setImageUrlTrigger(null);
		uploadQueue.destroy();
		clearTimeout(uploadErrorTimer);
		editor?.destroy();
	});

	// Watch for external content changes (e.g. another entry was loaded).
	$: if (editor && content !== lastEmitted) {
		syncExternalContent(content);
	}

	function syncExternalContent(next: string) {
		if (!editor) return;
		lastEmitted = next;
		if (stripPendingImages(currentHtml()) === next) return;
		// In-flight uploads belong to the entry being replaced.
		uploadQueue.clear();
		editor.commands.setContent(next, false);
		scheduleReconcile();
	}
</script>

<div class="tiptap-editor">
	<div bind:this={editorElement} class="editor-container"></div>
	{#if emptyStatePrompt && !content && !isFocused}
		<button
			type="button"
			class="empty-state-overlay"
			on:click={() => editor?.commands.focus()}
			aria-label="Focus editor"
		>
			<div class="text-center text-muted-foreground">
				<p class="text-sm">{emptyStatePrompt}</p>
			</div>
		</button>
	{/if}
	{#if showAddButton}
		<button
			bind:this={addButtonEl}
			type="button"
			class="add-button"
			style="top: {addButtonTop}px;"
			on:click={handleAddClick}
		>
			+
		</button>
	{/if}
	<input
		type="file"
		accept="image/jpeg,image/png,image/gif,image/webp,image/svg+xml,image/heic,image/heif,.heic,.heif,video/quicktime,video/mp4,.mov,.mp4"
		multiple
		bind:this={fileInput}
		on:change={handleFileSelect}
		style="display: none;"
	/>
	{#if uploadingCount > 0}
		<div class="upload-status" class:raised={!!uploadError} role="status" aria-live="polite">
			<span class="upload-status-spinner"></span>
			Uploading {uploadingCount} {uploadingCount === 1 ? 'image' : 'images'}…
		</div>
	{/if}
	{#if uploadError}
		<div class="upload-error" class:info={uploadErrorTone === 'info'} role={uploadErrorTone === 'info' ? 'status' : 'alert'}>{uploadError}</div>
	{/if}
</div>

{#if showImageUrlDialog}
	<ImageUrlDialog
		onInsert={(image) => insertImagesAtSavedPos([image])}
		onClose={() => (showImageUrlDialog = false)}
	/>
{/if}

{#if showMediaPicker}
	<MediaPicker
		onSelect={handleMediaSelect}
		onClose={() => showMediaPicker = false}
	/>
{/if}

<style>
	.tiptap-editor {
		position: relative;
		width: 100%;
		min-height: 500px;
	}

	.editor-container {
		min-height: 500px;
	}

	.add-button {
		position: absolute;
		left: 0;
		width: 24px;
		height: 24px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: transparent;
		border: none;
		color: hsl(var(--muted-foreground));
		font-size: 18px;
		font-weight: 300;
		cursor: pointer;
		opacity: 0.4;
		transition: opacity 0.15s ease;
		padding: 0;
	}

	.add-button:hover {
		opacity: 0.8;
	}

	.upload-error,
	.upload-status {
		position: fixed;
		right: 20px;
		max-width: min(420px, calc(100vw - 40px));
		padding: 10px 14px;
		border-radius: 10px;
		font-size: 14px;
		z-index: 1000;
		box-shadow: 0 8px 24px rgb(0 0 0 / 0.18);
		animation: slideIn 0.2s ease;
	}

	.upload-error {
		bottom: 20px;
		background: hsl(0 84% 60%);
		color: white;
	}

	.upload-error.info {
		background: hsl(var(--card));
		color: hsl(var(--foreground));
		border: 1px solid hsl(var(--border));
	}

	.upload-status {
		bottom: 20px;
		display: flex;
		align-items: center;
		gap: 10px;
		background: hsl(var(--card));
		color: hsl(var(--foreground));
		border: 1px solid hsl(var(--border));
	}

	.upload-status.raised {
		bottom: 72px;
	}

	.upload-status-spinner {
		width: 14px;
		height: 14px;
		border-radius: 50%;
		border: 2px solid hsl(var(--muted-foreground) / 0.3);
		border-top-color: hsl(var(--primary));
		animation: spin 0.8s linear infinite;
	}

	@keyframes slideIn {
		from {
			transform: translateX(100%);
			opacity: 0;
		}
		to {
			transform: translateX(0);
			opacity: 1;
		}
	}

	.empty-state-overlay {
		position: absolute;
		inset: 0;
		display: flex;
		align-items: center;
		justify-content: center;
		width: 100%;
		background: transparent;
		border: 0;
		padding: 0;
		cursor: text;
		pointer-events: auto;
	}
</style>
