import { writable } from 'svelte/store';

export interface LightboxItem {
	src: string;
	/** Low-resolution version shown while the full image loads. */
	thumb?: string;
	alt?: string;
	title?: string;
}

export interface LightboxRequest {
	items: LightboxItem[];
	index: number;
}

/** The lightbox mounted in the root layout renders whatever is set here. */
export const lightbox = writable<LightboxRequest | null>(null);

export function openLightbox(items: LightboxItem[], index = 0) {
	if (items.length === 0) return;
	lightbox.set({ items, index: Math.min(Math.max(index, 0), items.length - 1) });
}

export function closeLightbox() {
	lightbox.set(null);
}

function imageItem(img: HTMLImageElement): LightboxItem {
	return { src: img.currentSrc || img.src, alt: img.alt || undefined };
}

/**
 * Opens the given image in the lightbox, with every other image of `scope`
 * reachable through prev/next.
 */
export function openLightboxFor(img: HTMLImageElement, scope: ParentNode | null) {
	const images = scope
		? Array.from(scope.querySelectorAll<HTMLImageElement>('img')).filter(
				(el) => el.src && !el.closest('[data-lightbox-ignore]') && el.dataset.uploading !== 'true'
			)
		: [img];
	const index = Math.max(images.indexOf(img), 0);
	openLightbox((images.length ? images : [img]).map(imageItem), index);
}

/**
 * Svelte action for read-only content (book pages, history previews): clicking
 * any image inside the node opens it in the lightbox.
 */
export function lightboxImages(node: HTMLElement) {
	function handleClick(event: MouseEvent) {
		const target = event.target as HTMLElement | null;
		if (!(target instanceof HTMLImageElement) || target.closest('a[href]')) return;
		event.preventDefault();
		event.stopPropagation();
		openLightboxFor(target, node);
	}
	node.addEventListener('click', handleClick);
	node.classList.add('lightbox-scope');
	return {
		destroy() {
			node.removeEventListener('click', handleClick);
			node.classList.remove('lightbox-scope');
		}
	};
}
