import type { Editor, Range } from '@tiptap/core';

export interface CommandItem {
	title: string;
	description: string;
	icon: string;
	group: string;
	command: (props: { editor: Editor; range: Range }) => void;
}

export type ImageUploadTrigger = () => void;

let imageUploadTrigger: ImageUploadTrigger | null = null;

export function setImageUploadTrigger(trigger: ImageUploadTrigger | null) {
	imageUploadTrigger = trigger;
}

export const getSuggestionItems = (query: string): CommandItem[] => {
	const items: CommandItem[] = [
		{
			title: 'Text',
			description: 'Plain text',
			icon: 'text',
			group: 'FORMAT',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).setParagraph().run();
			},
		},
		{
			title: 'Heading 1',
			description: 'Large heading',
			icon: 'h1',
			group: 'FORMAT',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).setHeading({ level: 1 }).run();
			},
		},
		{
			title: 'Heading 2',
			description: 'Medium heading',
			icon: 'h2',
			group: 'FORMAT',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).setHeading({ level: 2 }).run();
			},
		},
		{
			title: 'Heading 3',
			description: 'Small heading',
			icon: 'h3',
			group: 'FORMAT',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).setHeading({ level: 3 }).run();
			},
		},
		{
			title: 'Bullet List',
			description: 'Unordered list',
			icon: 'list',
			group: 'LIST',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).toggleBulletList().run();
			},
		},
		{
			title: 'Numbered List',
			description: 'Ordered list',
			icon: 'list-ordered',
			group: 'LIST',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).toggleOrderedList().run();
			},
		},
		{
			title: 'Task List',
			description: 'Checklist',
			icon: 'check-square',
			group: 'LIST',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).toggleTaskList().run();
			},
		},
		{
			title: 'Quote',
			description: 'Blockquote',
			icon: 'quote',
			group: 'INSERT',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).setBlockquote().run();
			},
		},
		{
			title: 'Code Block',
			description: 'Code snippet',
			icon: 'code',
			group: 'INSERT',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).setCodeBlock().run();
			},
		},
		{
			title: 'Divider',
			description: 'Horizontal line',
			icon: 'minus',
			group: 'INSERT',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).setHorizontalRule().run();
			},
		},
		{
			title: 'Image',
			description: 'Upload image',
			icon: 'image',
			group: 'INSERT',
			command: ({ editor, range }) => {
				editor.chain().focus().deleteRange(range).run();
				if (imageUploadTrigger) {
					imageUploadTrigger();
				}
			},
		},
	];

	return items.filter((item) =>
		item.title.toLowerCase().includes(query.toLowerCase()) ||
		item.description.toLowerCase().includes(query.toLowerCase())
	);
};
