import type { SuggestionProps, SuggestionKeyDownProps } from '@tiptap/suggestion';
import type { CommandItem } from './commands';
import { mount, unmount } from 'svelte';
import CommandMenu from './CommandMenu.svelte';

let component: ReturnType<typeof mount> | null = null;
let container: HTMLElement | null = null;
let currentProps: SuggestionProps<CommandItem> | null = null;
let selectedIndex = 0;

function updateMenu() {
	if (container && currentProps) {
		// Unmount and remount with new props
		if (component) {
			unmount(component);
		}
		component = mount(CommandMenu, {
			target: container,
			props: {
				items: currentProps.items,
				selectedIndex,
				onSelect: (item: CommandItem) => {
					currentProps?.command(item);
				},
			},
		});
	}
}

function updatePosition() {
	if (!container || !currentProps) return;
	const rect = currentProps.clientRect?.();
	if (!rect) return;
	container.style.left = `${rect.left}px`;
	container.style.top = `${rect.bottom + 8}px`;
}

export const suggestionRenderer = {
	onStart: (props: SuggestionProps<CommandItem>) => {
		currentProps = props;
		selectedIndex = 0;

		container = document.createElement('div');
		container.style.position = 'absolute';
		container.style.zIndex = '1000';
		document.body.appendChild(container);

		updateMenu();
		updatePosition();
	},

	onUpdate: (props: SuggestionProps<CommandItem>) => {
		currentProps = props;
		updateMenu();
		updatePosition();
	},

	onKeyDown: (props: SuggestionKeyDownProps): boolean => {
		const { event } = props;

		if (event.key === 'ArrowUp') {
			selectedIndex = Math.max(0, selectedIndex - 1);
			updateMenu();
			return true;
		}

		if (event.key === 'ArrowDown') {
			const items = currentProps?.items || [];
			selectedIndex = Math.min(items.length - 1, selectedIndex + 1);
			updateMenu();
			return true;
		}

		if (event.key === 'Enter') {
			const items = currentProps?.items || [];
			const item = items[selectedIndex];
			if (item) {
				currentProps?.command(item);
			}
			return true;
		}

		if (event.key === 'Escape') {
			currentProps?.editor.commands.focus();
			return true;
		}

		return false;
	},

	onExit: () => {
		if (component) {
			unmount(component);
			component = null;
		}
		container?.remove();
		container = null;
		currentProps = null;
		selectedIndex = 0;
	},
};
