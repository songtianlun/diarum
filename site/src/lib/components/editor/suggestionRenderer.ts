import type { SuggestionProps, SuggestionKeyDownProps } from '@tiptap/suggestion';
import type { CommandItem } from './commands';
import { mount, unmount } from 'svelte';
import CommandMenu from './CommandMenu.svelte';

interface MenuState {
	component: ReturnType<typeof mount> | null;
	container: HTMLElement | null;
	props: SuggestionProps<CommandItem> | null;
	selectedIndex: number;
}

// Encapsulate state in an object to avoid global variable pollution
const state: MenuState = {
	component: null,
	container: null,
	props: null,
	selectedIndex: 0,
};

function createMenu() {
	if (!state.container || !state.props) return;

	state.component = mount(CommandMenu, {
		target: state.container,
		props: {
			items: state.props.items,
			selectedIndex: state.selectedIndex,
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
	state.container.style.left = `${rect.left}px`;
	state.container.style.top = `${rect.bottom + 8}px`;
}

function cleanup() {
	if (state.component) {
		unmount(state.component);
		state.component = null;
	}
	state.container?.remove();
	state.container = null;
	state.props = null;
	state.selectedIndex = 0;
}

export const suggestionRenderer = {
	onStart: (props: SuggestionProps<CommandItem>) => {
		state.props = props;
		state.selectedIndex = 0;

		state.container = document.createElement('div');
		state.container.style.position = 'absolute';
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
