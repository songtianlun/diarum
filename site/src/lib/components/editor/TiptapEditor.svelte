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
	import { uploadImage, getMediaUrl, isCheveretoResult } from '$lib/utils/uploadImage';
	import { SlashCommands } from './SlashCommands';
	import { getSuggestionItems, setImageUploadTrigger, setGalleryPickerTrigger } from './commands';
	import { suggestionRenderer, showCommandMenu } from './suggestionRenderer';
	import MediaPicker from './MediaPicker.svelte';
	import { getMediaFileUrl, addMediaDiary } from '$lib/api/media';
	import type { MediaWithDiary } from '$lib/api/media';

	export let content = '';
	export let onChange: (value: string) => void = () => {};
	export let placeholder = 'Start writing...';
	export let diaryDate: string | undefined = undefined;

	let editorElement: HTMLDivElement;
	let editor: Editor | null = null;
	let fileInput: HTMLInputElement;
	let uploadError = '';
	let showMediaPicker = false;

	// Add button state
	let showAddButton = false;
	let addButtonTop = 0;

	const lowlight = createLowlight(common);

	// Image upload config
	const IMAGE_CONFIG = {
		maxSize: 50 * 1024 * 1024, // 50MB
		allowedTypes: ['image/jpeg', 'image/png', 'image/gif', 'image/webp'],
	};

	// Validate image file
	function validateImageFile(file: File): string | null {
		if (!IMAGE_CONFIG.allowedTypes.includes(file.type)) {
			return `Unsupported image format. Please use JPG, PNG, GIF or WebP`;
		}
		if (file.size > IMAGE_CONFIG.maxSize) {
			const maxMB = IMAGE_CONFIG.maxSize / 1024 / 1024;
			return `Image size cannot exceed ${maxMB}MB`;
		}
		return null;
	}

	// Generate unique placeholder ID
	function generatePlaceholderId(): string {
		return `upload-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
	}

	// Handle image upload with placeholder
	async function handleImageUploadWithPlaceholder(file: File): Promise<void> {
		if (!editor) return;

		// Validate file
		const validationError = validateImageFile(file);
		if (validationError) {
			uploadError = validationError;
			setTimeout(() => (uploadError = ''), 3000);
			return;
		}

		const placeholderId = generatePlaceholderId();

		// Insert placeholder with preview
		editor.chain().focus().setImagePlaceholder({ id: placeholderId, file }).run();

		uploadError = '';

		try {
			const result = await uploadImage(file, { diaryDate });
			let url: string;

			if (isCheveretoResult(result)) {
				url = result.cheveretoUrl;
			} else {
				url = getMediaUrl(result);
			}

			// Replace placeholder with actual image
			editor.commands.replacePlaceholderWithImage({
				id: placeholderId,
				src: url,
				alt: file.name,
			});
		} catch (error) {
			console.error('Image upload failed:', error);
			uploadError = 'Image upload failed, please try again';
			setTimeout(() => (uploadError = ''), 3000);

			// Remove placeholder on error
			editor.commands.removePlaceholder(placeholderId);
		}
	}

	// Handle paste event
	function handlePaste(view: any, event: ClipboardEvent) {
		const items = event.clipboardData?.items;
		if (!items) return false;

		for (const item of items) {
			if (item.type.startsWith('image/')) {
				event.preventDefault();
				const file = item.getAsFile();
				if (file) {
					handleImageUploadWithPlaceholder(file);
				}
				return true;
			}
		}
		return false;
	}

	// Handle drop event
	function handleDrop(view: any, event: DragEvent) {
		const files = event.dataTransfer?.files;
		if (!files || files.length === 0) return false;

		const file = files[0];
		if (file.type.startsWith('image/')) {
			event.preventDefault();
			handleImageUploadWithPlaceholder(file);
			return true;
		}
		return false;
	}

	// Handle slash command image trigger
	function handleSlashImage() {
		fileInput?.click();
	}

	// Handle gallery picker trigger
	function handleGalleryPicker() {
		showMediaPicker = true;
	}

	// Handle media selection from gallery
	async function handleMediaSelect(media: MediaWithDiary) {
		if (!editor) return;

		const url = getMediaFileUrl(media);
		editor.chain().focus().setImage({ src: url }).run();

		// Associate media with current diary
		if (diaryDate && media.id) {
			try {
				const { getOrCreateDiaryId } = await import('$lib/utils/uploadImage');
				const diaryId = await getOrCreateDiaryId(diaryDate);
				if (diaryId) {
					await addMediaDiary(media.id, diaryId);
				}
			} catch (error) {
				console.error('Failed to associate media with diary:', error);
			}
		}
	}

	function handleFileSelect(event: Event) {
		const input = event.target as HTMLInputElement;
		const file = input.files?.[0];
		if (file) {
			handleImageUploadWithPlaceholder(file);
			input.value = '';
		}
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
			content,
			editorProps: {
				handlePaste,
				handleDrop,
				attributes: {
					class: 'tiptap-editor-content',
				},
			},
			onUpdate: ({ editor }) => {
				onChange(editor.getHTML());
			},
			onTransaction: () => {
				editor = editor;
				updateAddButton();
			},
		});
	});

	onDestroy(() => {
		// Cleanup image upload trigger
		setImageUploadTrigger(null);
		// Cleanup gallery picker trigger
		setGalleryPickerTrigger(null);
		editor?.destroy();
	});

	// Watch for external content changes
	$: if (editor) {
		const isSame = editor.getHTML() === content;
		if (!isSame) {
			editor.commands.setContent(content, false);
		}
	}
</script>

<div class="tiptap-editor">
	<div bind:this={editorElement} class="editor-container"></div>
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
		accept="image/*"
		bind:this={fileInput}
		on:change={handleFileSelect}
		style="display: none;"
	/>
	{#if uploadError}
		<div class="upload-error">{uploadError}</div>
	{/if}
</div>

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

	.upload-error {
		position: fixed;
		bottom: 20px;
		right: 20px;
		background: hsl(0 84% 60%);
		color: white;
		padding: 12px 16px;
		border-radius: 8px;
		font-size: 14px;
		z-index: 1000;
		animation: slideIn 0.2s ease;
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
</style>
