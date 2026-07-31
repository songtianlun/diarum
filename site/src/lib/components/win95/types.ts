/** Shared shapes for the Windows 95 skin components. */

/** Every glyph `Win95Icon` knows how to draw. */
export type Win95IconName =
	| 'book'
	| 'folder'
	| 'folder-open'
	| 'page'
	| 'page-blank'
	| 'notepad'
	| 'contents'
	| 'calendar'
	| 'left'
	| 'right'
	| 'robot'
	| 'share'
	| 'search'
	| 'settings'
	| 'media'
	| 'disk'
	| 'saved'
	| 'dirty'
	| 'offline'
	| 'syncing'
	| 'info'
	| 'h1'
	| 'h2'
	| 'h3'
	| 'outline'
	| 'tree-expand'
	| 'tree-collapse'
	| 'star'
	| 'home'
	| 'diarum';

/** A row in the Start menu: either a separator or an icon + label + action. */
export interface Win95StartItem {
	sep?: boolean;
	label?: string;
	icon?: Win95IconName;
	action?: () => void;
}

export interface Win95MenuItem {
	/** A horizontal separator; every other field is ignored. */
	sep?: boolean;
	label?: string;
	shortcut?: string;
	checked?: boolean;
	disabled?: boolean;
	action?: () => void;
}

export interface Win95Menu {
	label: string;
	/** Index of the character to underline as the access key. */
	mnemonic?: number;
	items: Win95MenuItem[];
}
