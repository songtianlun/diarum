import { Node, mergeAttributes } from '@tiptap/core';
import type { UploadQueue, UploadState } from './uploadQueue';
import { openLightboxFor } from '$lib/stores/lightbox';

export interface ImageOptions {
	inline: boolean;
	allowBase64: boolean;
	HTMLAttributes: Record<string, any>;
	/** Supplies the editor's upload queue so placeholders can follow their upload. */
	getUploadQueue: () => UploadQueue | null;
}

export interface ImageInsert {
	src: string;
	alt?: string;
	title?: string;
}

declare module '@tiptap/core' {
	interface Commands<ReturnType> {
		customImage: {
			setImage: (options: ImageInsert) => ReturnType;
			insertImages: (images: ImageInsert[]) => ReturnType;
			insertUploadPlaceholders: (placeholders: { id: string; src: string; alt?: string }[]) => ReturnType;
			removePlaceholder: (id: string) => ReturnType;
			replacePlaceholderWithImage: (options: { id: string; src: string; alt?: string }) => ReturnType;
		};
	}
}

const RING_RADIUS = 18;
const RING_CIRCUMFERENCE = 2 * Math.PI * RING_RADIUS;

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className?: string, text?: string): HTMLElementTagNameMap[K] {
	const node = document.createElement(tag);
	if (className) node.className = className;
	if (text) node.textContent = text;
	return node;
}

function iconButton(className: string, label: string, path: string): HTMLButtonElement {
	const button = el('button', className);
	button.type = 'button';
	button.title = label;
	button.setAttribute('aria-label', label);
	button.contentEditable = 'false';
	button.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="${path}"/></svg>`;
	return button;
}

const ICON_EXPAND = 'M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5';
const ICON_EDIT = 'M4 20h4L18.5 9.5a2.1 2.1 0 00-3-3L5 17v3zM13.5 6.5l3 3';
const ICON_CLOSE = 'M6 6l12 12M18 6L6 18';

function statusLabel(state: UploadState | undefined): string {
	if (!state) return 'Upload interrupted';
	switch (state.status) {
		case 'queued':
			return 'Waiting…';
		case 'uploading':
			return state.progress >= 100 ? 'Processing…' : `${state.progress}%`;
		case 'finishing':
		case 'done':
			return 'Almost done…';
		case 'error':
			return state.error || 'Upload failed';
	}
}

export const ImageExtension = Node.create<ImageOptions>({
	name: 'image',

	addOptions() {
		return {
			inline: false,
			allowBase64: true,
			HTMLAttributes: {},
			getUploadQueue: () => null,
		};
	},

	inline() {
		return this.options.inline;
	},

	group() {
		return this.options.inline ? 'inline' : 'block';
	},

	draggable: true,

	addAttributes() {
		return {
			src: {
				default: null,
			},
			alt: {
				default: null,
			},
			title: {
				default: null,
			},
			'data-uploading': {
				default: null,
			},
			'data-placeholder-id': {
				default: null,
			},
		};
	},

	parseHTML() {
		return [{ tag: 'img[src]' }];
	},

	renderHTML({ HTMLAttributes }) {
		return ['img', mergeAttributes(this.options.HTMLAttributes, HTMLAttributes)];
	},

	addNodeView() {
		return ({ node, editor, getPos }) => {
			const wrapper = el('div', 'image-wrapper');
			const img = el('img');
			img.draggable = false;
			wrapper.appendChild(img);

			let currentNode = node;
			let unsubscribe: (() => void) | null = null;
			let overlay: HTMLDivElement | null = null;

			const applyAttrs = (attrs: Record<string, any>) => {
				const merged = mergeAttributes(this.options.HTMLAttributes, attrs);
				for (const [key, value] of Object.entries(merged)) {
					if (key === 'src') continue;
					if (value === null || value === undefined) img.removeAttribute(key);
					else img.setAttribute(key, String(value));
				}
			};

			// Final images: fade in once loaded, with a skeleton until then.
			const showImage = (src: string, fromPlaceholder: boolean) => {
				wrapper.classList.remove('is-error');
				if (fromPlaceholder) {
					// The upload queue preloaded this URL, so swapping is seamless.
					img.src = src;
					return;
				}
				if (img.getAttribute('src') === src && img.complete && img.naturalWidth > 0) return;
				wrapper.classList.add('is-loading');
				img.onload = () => wrapper.classList.remove('is-loading', 'is-error');
				img.onerror = () => {
					wrapper.classList.remove('is-loading');
					wrapper.classList.add('is-error');
				};
				img.src = src;
				if (img.complete && img.naturalWidth > 0) wrapper.classList.remove('is-loading');
			};

			const tools = el('div', 'image-tools');
			tools.contentEditable = 'false';
			const editButton = iconButton('image-tool-btn image-edit-btn', 'Edit image URL', ICON_EDIT);
			const zoomButton = iconButton('image-tool-btn image-zoom-btn', 'View full size', ICON_EXPAND);
			zoomButton.addEventListener('click', (event) => {
				event.preventDefault();
				event.stopPropagation();
				openLightboxFor(img, editor.view.dom);
			});
			tools.append(editButton, zoomButton);
			wrapper.appendChild(tools);

			// Inline URL editor
			let urlEditor: HTMLFormElement | null = null;

			const closeUrlEditor = (refocus: boolean) => {
				urlEditor?.remove();
				urlEditor = null;
				wrapper.classList.remove('is-editing');
				if (refocus) editor.commands.focus();
			};

			const saveUrl = (value: string) => {
				const src = value.trim();
				const pos = typeof getPos === 'function' ? getPos() : undefined;
				closeUrlEditor(true);
				if (!src || src === currentNode.attrs.src || typeof pos !== 'number') return;
				editor.view.dispatch(editor.state.tr.setNodeMarkup(pos, undefined, { ...currentNode.attrs, src }));
			};

			const openUrlEditor = () => {
				if (urlEditor || !editor.isEditable) return;
				urlEditor = el('form', 'image-url-editor');
				const input = el('input');
				// Not type="url": built-in media uses relative paths, which it rejects.
				input.type = 'text';
				input.inputMode = 'url';
				input.value = currentNode.attrs.src ?? '';
				input.placeholder = 'https://…';
				input.spellcheck = false;
				input.setAttribute('aria-label', 'Image URL');
				const save = el('button', 'image-url-save', 'Save');
				save.type = 'submit';
				const cancel = el('button', 'image-url-cancel', 'Cancel');
				cancel.type = 'button';

				urlEditor.addEventListener('submit', (event) => {
					event.preventDefault();
					saveUrl(input.value);
				});
				cancel.addEventListener('click', () => closeUrlEditor(true));
				input.addEventListener('keydown', (event) => {
					if (event.key === 'Escape') {
						event.preventDefault();
						event.stopPropagation();
						closeUrlEditor(true);
					}
				});
				// Clicking elsewhere discards the edit.
				input.addEventListener('blur', () => {
					setTimeout(() => {
						if (urlEditor && !urlEditor.contains(document.activeElement)) closeUrlEditor(false);
					}, 0);
				});

				// Keep focus in the input when pressing the buttons: Safari does not
				// focus buttons on click, so the blur below would close the form first.
				urlEditor.addEventListener('mousedown', (event) => {
					if (event.target !== input) event.preventDefault();
				});

				urlEditor.append(input, save, cancel);
				wrapper.appendChild(urlEditor);
				wrapper.classList.add('is-editing');
				input.focus();
				input.select();
			};

			editButton.addEventListener('click', (event) => {
				event.preventDefault();
				event.stopPropagation();
				openUrlEditor();
			});

			wrapper.addEventListener('dblclick', (event) => {
				if (wrapper.classList.contains('is-pending') || wrapper.classList.contains('is-error')) return;
				if ((event.target as HTMLElement).closest('.image-url-editor, .image-tools')) return;
				event.preventDefault();
				openLightboxFor(img, editor.view.dom);
			});

			const buildOverlay = (id: string) => {
				overlay = el('div', 'upload-overlay');
				overlay.contentEditable = 'false';

				const ring = el('div', 'upload-ring');
				ring.innerHTML = `<svg viewBox="0 0 44 44"><circle class="upload-ring-track" cx="22" cy="22" r="${RING_RADIUS}"/><circle class="upload-ring-bar" cx="22" cy="22" r="${RING_RADIUS}" stroke-dasharray="${RING_CIRCUMFERENCE}" stroke-dashoffset="${RING_CIRCUMFERENCE}"/></svg>`;
				const bar = ring.querySelector<SVGCircleElement>('.upload-ring-bar')!;
				const errorIcon = el('div', 'upload-error-icon', '!');
				const label = el('span', 'upload-text');
				const actions = el('div', 'upload-actions');
				const retry = el('button', 'upload-action primary', 'Retry');
				retry.type = 'button';
				const remove = el('button', 'upload-action', 'Remove');
				remove.type = 'button';
				const cancel = iconButton('upload-cancel', 'Cancel upload', ICON_CLOSE);

				retry.addEventListener('click', (event) => {
					event.preventDefault();
					event.stopPropagation();
					this.options.getUploadQueue()?.retry(id);
				});
				const removeUpload = (event: Event) => {
					event.preventDefault();
					event.stopPropagation();
					this.options.getUploadQueue()?.cancel(id);
					editor.commands.removePlaceholder(id);
				};
				remove.addEventListener('click', removeUpload);
				cancel.addEventListener('click', removeUpload);

				actions.append(retry, remove);
				overlay.append(ring, errorIcon, label, actions, cancel);
				wrapper.appendChild(overlay);

				const render = (state: UploadState | undefined) => {
					const failed = !state || state.status === 'error';
					wrapper.classList.toggle('upload-failed', failed);
					wrapper.classList.toggle('upload-indeterminate', state?.status === 'queued' || state?.status === 'finishing' || (state?.status === 'uploading' && state.progress >= 100));
					label.textContent = statusLabel(state);
					label.title = state?.fileName ?? '';
					const progress = state?.status === 'uploading' ? state.progress : state?.status === 'finishing' ? 100 : 0;
					bar.style.strokeDashoffset = String(RING_CIRCUMFERENCE * (1 - progress / 100));
				};

				const queue = this.options.getUploadQueue();
				if (queue?.get(id)) {
					unsubscribe = queue.subscribe(id, render);
				} else {
					render(undefined);
				}
			};

			const clearOverlay = (animate: boolean) => {
				unsubscribe?.();
				unsubscribe = null;
				const current = overlay;
				overlay = null;
				wrapper.classList.remove('is-pending', 'upload-failed', 'upload-indeterminate');
				if (!current) return;
				if (!animate) {
					current.remove();
					return;
				}
				wrapper.classList.add('upload-complete');
				current.classList.add('leaving');
				setTimeout(() => {
					current.remove();
					wrapper.classList.remove('upload-complete');
				}, 320);
			};

			const render = (next: typeof node, previous: typeof node | null) => {
				applyAttrs(next.attrs);
				const pendingId = next.attrs['data-uploading'] === 'true' ? next.attrs['data-placeholder-id'] : null;
				const wasPending = previous?.attrs['data-uploading'] === 'true';

				if (pendingId) {
					if (!wasPending || previous?.attrs['data-placeholder-id'] !== pendingId) {
						clearOverlay(false);
						wrapper.classList.add('is-pending');
						buildOverlay(pendingId);
					}
					if (img.getAttribute('src') !== next.attrs.src) img.src = next.attrs.src;
					return;
				}

				if (wasPending) clearOverlay(true);
				if (next.attrs.src && img.getAttribute('src') !== next.attrs.src) {
					showImage(next.attrs.src, wasPending);
				}
			};

			render(node, null);

			return {
				dom: wrapper,
				contentDOM: null,
				update: (updatedNode) => {
					if (updatedNode.type.name !== this.name) return false;
					const previous = currentNode;
					currentNode = updatedNode;
					render(updatedNode, previous);
					return true;
				},
				// Clicks and typing in our controls belong to them, not to ProseMirror.
				stopEvent: (event) => {
					const target = event.target as HTMLElement | null;
					return !!target?.closest('button, .image-url-editor');
				},
				// Our own class/overlay changes must not make ProseMirror redraw the node.
				ignoreMutation: (mutation) => mutation.type !== 'selection',
				destroy: () => {
					urlEditor?.remove();
					unsubscribe?.();
					unsubscribe = null;
				},
			};
		};
	},

	addCommands() {
		const findPlaceholder = (doc: any, id: string) => {
			let found: { pos: number; node: any } | null = null;
			doc.descendants((node: any, pos: number) => {
				if (found) return false;
				if (node.type.name === this.name && node.attrs['data-placeholder-id'] === id) {
					found = { pos, node };
					return false;
				}
			});
			return found as { pos: number; node: any } | null;
		};

		return {
			setImage:
				(options) =>
				({ commands }) => {
					return commands.insertContent({
						type: this.name,
						attrs: options,
					});
				},

			insertImages:
				(images) =>
				({ commands }) => {
					return commands.insertContent(images.map((attrs) => ({ type: this.name, attrs })));
				},

			insertUploadPlaceholders:
				(placeholders) =>
				({ commands }) => {
					return commands.insertContent(
						placeholders.map(({ id, src, alt }) => ({
							type: this.name,
							attrs: {
								src,
								alt: alt || null,
								'data-uploading': 'true',
								'data-placeholder-id': id,
							},
						}))
					);
				},

			removePlaceholder:
				(id) =>
				({ tr, state, dispatch }) => {
					const found = findPlaceholder(state.doc, id);
					if (!found) return false;
					if (dispatch) {
						tr.delete(found.pos, found.pos + found.node.nodeSize);
						tr.setMeta('addToHistory', false);
					}
					return true;
				},

			// Kept out of undo history: undoing should never bring back a
			// half-finished upload, only remove the image entirely.
			replacePlaceholderWithImage:
				(options) =>
				({ tr, state, dispatch }) => {
					const found = findPlaceholder(state.doc, options.id);
					if (!found) return false;
					if (dispatch) {
						tr.setNodeMarkup(found.pos, undefined, {
							...found.node.attrs,
							src: options.src,
							alt: options.alt || null,
							'data-uploading': null,
							'data-placeholder-id': null,
						});
						tr.setMeta('addToHistory', false);
					}
					return true;
				},
		};
	},
});

export default ImageExtension;
