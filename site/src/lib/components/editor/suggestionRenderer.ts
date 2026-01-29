import type { SuggestionProps, SuggestionKeyDownProps } from '@tiptap/suggestion';
import type { Editor } from '@tiptap/core';
import type { CommandItem } from './commands';
import { mount, unmount } from 'svelte';
import CommandMenu from './CommandMenu.svelte';
import { getSuggestionItems } from './commands';

interface MenuState {
	component: ReturnType<typeof mount> | null;
	container: HTMLElement | null;
	props: SuggestionProps<CommandItem> | null;
	selectedIndex: number;
	isAbove: boolean;
}

// Encapsulate state in an object to avoid global variable pollution
const state: MenuState = {
	component: null,
	container: null,
	props: null,
	selectedIndex: 0,
	isAbove: false,
};

function createMenu() {
	if (!state.container || !state.props) return;

	state.component = mount(CommandMenu, {
		target: state.container,
		props: {
			items: state.props.items,
			selectedIndex: state.selectedIndex,
			isAbove: state.isAbove,
			onSelect: (item: CommandItem) => {
				state.props?.command(item);
			},
		},
	});
}

function updateMenuProps() {
	if (!state.container || !state.props || !state.component) return;

	// Unmount and remount with new props
	// Note: Svelte 5 mount() doesn't support updating props directly,
	// so we need to remount. This is still more efficient than before
	// because we only do it when items actually change.
	unmount(state.component);
	createMenu();
}

function updatePosition() {
	if (!state.container || !state.props) return;
	const rect = state.props.clientRect?.();
	if (!rect) return;

	const menuHeight = 340; // max-height of command-menu
	const gap = 8;
	const viewportHeight = window.innerHeight;

	// Check if there's enough space below
	const spaceBelow = viewportHeight - rect.bottom;
	const spaceAbove = rect.top;

	// If cursor is in the lower half of the screen or not enough space below, show above
	state.isAbove = spaceBelow < menuHeight + gap && spaceAbove > spaceBelow;

	state.container.style.left = `${rect.left}px`;

	if (state.isAbove) {
		// Position above the cursor
		state.container.style.top = 'auto';
		state.container.style.bottom = `${viewportHeight - rect.top + gap}px`;
	} else {
		// Position below the cursor
		state.container.style.top = `${rect.bottom + gap}px`;
		state.container.style.bottom = 'auto';
	}
}

function cleanup() {
	if (state.container) {
		// Add exit animation class
		state.container.classList.add('menu-exit');

		// Wait for animation to complete before removing
		setTimeout(() => {
			if (state.component) {
				unmount(state.component);
				state.component = null;
			}
			state.container?.remove();
			state.container = null;
			state.props = null;
			state.selectedIndex = 0;
			state.isAbove = false;
		}, 100); // Match the menuExit animation duration
	} else {
		if (state.component) {
			unmount(state.component);
			state.component = null;
		}
		state.props = null;
		state.selectedIndex = 0;
		state.isAbove = false;
	}
}

export const suggestionRenderer = {
	onStart: (props: SuggestionProps<CommandItem>) => {
		state.props = props;
		state.selectedIndex = 0;

		state.container = document.createElement('div');
		state.container.style.position = 'fixed';
		state.container.style.zIndex = '1000';
		document.body.appendChild(state.container);

		createMenu();
		updatePosition();
	},

	onUpdate: (props: SuggestionProps<CommandItem>) => {
		state.props = props;
		updateMenuProps();
		updatePosition();
	},

	onKeyDown: (props: SuggestionKeyDownProps): boolean => {
		const { event } = props;

		if (event.key === 'ArrowUp') {
			state.selectedIndex = Math.max(0, state.selectedIndex - 1);
			updateMenuProps();
			return true;
		}

		if (event.key === 'ArrowDown') {
			const items = state.props?.items || [];
			state.selectedIndex = Math.min(items.length - 1, state.selectedIndex + 1);
			updateMenuProps();
			return true;
		}

		if (event.key === 'Enter') {
			const items = state.props?.items || [];
			const item = items[state.selectedIndex];
			if (item) {
				state.props?.command(item);
			}
			return true;
		}

		if (event.key === 'Escape') {
			state.props?.editor.commands.focus();
			return true;
		}

		return false;
	},

	onExit: cleanup,
};

// Manual trigger for add button - shows menu without inserting /
export function showCommandMenu(editor: Editor, buttonElement: HTMLElement) {
	// Clean up any existing menu
	cleanup();

	const items = getSuggestionItems('');
	const rect = buttonElement.getBoundingClientRect();

	state.selectedIndex = 0;
	state.container = document.createElement('div');
	state.container.style.position = 'fixed';
	state.container.style.zIndex = '1000';
	document.body.appendChild(state.container);

	// Create a fake range at current cursor position
	const { from } = editor.state.selection;
	const range = { from, to: from };

	state.props = {
		editor,
		items,
		range,
		query: '',
		text: '',
		clientRect: () => rect,
		command: (item: CommandItem) => {
			item.command({ editor, range });
			cleanup();
		},
		decorationNode: null,
	} as unknown as SuggestionProps<CommandItem>;

	createMenu();
	updatePosition();

	// Close menu when clicking outside
	const handleClickOutside = (e: MouseEvent) => {
		if (state.container && !state.container.contains(e.target as Node)) {
			cleanup();
			document.removeEventListener('click', handleClickOutside);
		}
	};
	setTimeout(() => document.addEventListener('click', handleClickOutside), 0);
}
