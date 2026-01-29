<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { Editor } from '@tiptap/core';
	import StarterKit from '@tiptap/starter-kit';
	import Placeholder from '@tiptap/extension-placeholder';
	import Image from '@tiptap/extension-image';
	import Link from '@tiptap/extension-link';
	import Underline from '@tiptap/extension-underline';
	import Highlight from '@tiptap/extension-highlight';
	import TaskList from '@tiptap/extension-task-list';
	import TaskItem from '@tiptap/extension-task-item';
	import CharacterCount from '@tiptap/extension-character-count';
	import Typography from '@tiptap/extension-typography';
	import CodeBlockLowlight from '@tiptap/extension-code-block-lowlight';
	import { common, createLowlight } from 'lowlight';
	import { uploadImage, getMediaUrl } from '$lib/utils/uploadImage';
	import { SlashCommands } from './SlashCommands';
	import { getSuggestionItems } from './commands';
	import { suggestionRenderer } from './suggestionRenderer';

	export let content = '';
	export let onChange: (value: string) => void = () => {};
	export let placeholder = 'Start writing...';
	export let diaryId: string | undefined = undefined;

	let editorElement: HTMLDivElement;
	let editor: Editor | null = null;
	let fileInput: HTMLInputElement;
	let isUploading = false;

	const lowlight = createLowlight(common);

	// Handle image upload
	async function handleImageUpload(file: File): Promise<string | null> {
		if (isUploading) return null;
		isUploading = true;
		try {
			const media = await uploadImage(file, { diaryId });
			return getMediaUrl(media);
		} catch (error) {
			console.error('Image upload failed:', error);
			return null;
		} finally {
			isUploading = false;
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
					handleImageUpload(file).then((url) => {
						if (url && editor) {
							editor.chain().focus().setImage({ src: url }).run();
						}
					});
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
			handleImageUpload(file).then((url) => {
				if (url && editor) {
					editor.chain().focus().setImage({ src: url }).run();
				}
			});
			return true;
		}
		return false;
	}

	// Handle slash command image trigger
	function handleSlashImage() {
		fileInput?.click();
	}

	function handleFileSelect(event: Event) {
		const input = event.target as HTMLInputElement;
		const file = input.files?.[0];
		if (file) {
			handleImageUpload(file).then((url) => {
				if (url && editor) {
					editor.chain().focus().setImage({ src: url }).run();
				}
			});
			input.value = '';
		}
	}

	onMount(() => {
		document.addEventListener('slash-command-image', handleSlashImage);

		editor = new Editor({
			element: editorElement,
			extensions: [
				StarterKit.configure({
					codeBlock: false,
				}),
				Placeholder.configure({
					placeholder,
				}),
				Image.configure({
					inline: false,
					allowBase64: true,
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
		});
	});

	onDestroy(() => {
		document.removeEventListener('slash-command-image', handleSlashImage);
		editor?.destroy();
	});

	// Watch for external content changes
	$: if (editor && content !== editor.getHTML()) {
		editor.commands.setContent(content, false);
	}
</script>

<div class="tiptap-editor">
	<div bind:this={editorElement} class="editor-container"></div>
	<input
		type="file"
		accept="image/*"
		bind:this={fileInput}
		on:change={handleFileSelect}
		style="display: none;"
	/>
</div>

<style>
	.tiptap-editor {
		position: relative;
		width: 100%;
		min-height: 500px;
	}

	.editor-container {
		min-height: 500px;
	}
</style>
