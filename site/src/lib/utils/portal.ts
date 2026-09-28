/**
 * Svelte action: moves the node to document.body, so fixed-position overlays
 * are not clipped by an ancestor that creates a containing block (transform,
 * filter, backdrop-filter, contain).
 */
export function portal(node: HTMLElement) {
	document.body.appendChild(node);
	return {
		destroy() {
			node.remove();
		}
	};
}
