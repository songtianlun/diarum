/** Shared shapes for the Windows 95 skin components. */

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
