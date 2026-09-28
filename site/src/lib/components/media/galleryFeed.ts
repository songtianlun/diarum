import { writable, type Readable } from 'svelte/store';
import { fetchMediaPage, getMediaFileUrl, type MediaWithDiary } from '$lib/api/media';
import { variantUrl } from '$lib/utils/imageDisplay';

export interface GalleryItem {
	key: string;
	/** Full-size image, as inserted into entries. */
	src: string;
	/** Thumbnail variant for grids; falls back to `src` if it fails. */
	thumb: string;
	title: string;
	/** Local calendar day (YYYY-MM-DD) or '' when unknown. */
	date: string;
	media: MediaWithDiary;
}

export interface GalleryState {
	items: GalleryItem[];
	loading: boolean;
	/** True until the first page has arrived. */
	initial: boolean;
	error: string;
	hasMore: boolean;
	total: number | null;
}

export interface GalleryFeed extends Readable<GalleryState> {
	loadMore: () => Promise<void>;
	reload: () => Promise<void>;
	remove: (key: string) => void;
	destroy: () => void;
}

function pad(value: number) {
	return String(value).padStart(2, '0');
}

/** Media timestamps are stored in UTC ("2024-05-01 23:10:00.000Z"); group by the viewer's day. */
export function localDay(timestamp: string | undefined): string {
	if (!timestamp) return '';
	const date = new Date(timestamp.replace(' ', 'T'));
	if (Number.isNaN(date.getTime())) return timestamp.slice(0, 10);
	return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function fromMedia(media: MediaWithDiary): GalleryItem {
	const src = getMediaFileUrl(media);
	return {
		key: media.id ?? media.file ?? '',
		src,
		thumb: variantUrl(src, 'th') ?? src,
		title: media.name || media.alt || 'Image',
		date: localDay(media.created),
		media
	};
}

/**
 * Paged, append-only list of the built-in media library. Responses that
 * arrive after a reload or destroy are discarded.
 */
export function createGalleryFeed(options: { pageSize?: number } = {}): GalleryFeed {
	const pageSize = options.pageSize ?? 30;
	const initialState = (): GalleryState => ({
		items: [],
		loading: false,
		initial: true,
		error: '',
		hasMore: true,
		total: null
	});

	const store = writable<GalleryState>(initialState());
	let state = initialState();
	let nextPage = 1;
	let generation = 0;
	let controller: AbortController | null = null;

	function set(patch: Partial<GalleryState>) {
		state = { ...state, ...patch };
		store.set(state);
	}

	async function loadMore() {
		if (state.loading || !state.hasMore) return;
		const current = ++generation;
		controller = new AbortController();
		set({ loading: true, error: '' });
		try {
			const result = await fetchMediaPage(nextPage, pageSize, controller.signal);
			if (current !== generation) return;
			const seen = new Set(state.items.map((item) => item.key));
			const fresh = result.items.map((media) => fromMedia(media)).filter((item) => !seen.has(item.key));
			set({
				items: [...state.items, ...fresh],
				hasMore: nextPage < result.totalPages,
				total: result.totalItems
			});
			nextPage++;
		} catch (error) {
			if (current !== generation || (error instanceof DOMException && error.name === 'AbortError')) return;
			set({ error: error instanceof Error ? error.message : 'Failed to load images' });
		} finally {
			if (current === generation) set({ loading: false, initial: false });
		}
	}

	async function reload() {
		generation++;
		controller?.abort();
		nextPage = 1;
		state = initialState();
		store.set(state);
		await loadMore();
	}

	function remove(key: string) {
		set({
			items: state.items.filter((item) => item.key !== key),
			total: state.total === null ? null : Math.max(0, state.total - 1)
		});
	}

	function destroy() {
		generation++;
		controller?.abort();
	}

	return { subscribe: store.subscribe, loadMore, reload, remove, destroy };
}

export interface DayGroup {
	date: string;
	items: GalleryItem[];
}

/** Groups consecutive items by day, keeping the feed's newest-first order. */
export function groupByDay(items: GalleryItem[]): DayGroup[] {
	const groups: DayGroup[] = [];
	const byDate = new Map<string, DayGroup>();
	for (const item of items) {
		let group = byDate.get(item.date);
		if (!group) {
			group = { date: item.date, items: [] };
			byDate.set(item.date, group);
			groups.push(group);
		}
		group.items.push(item);
	}
	return groups;
}

export function formatDayLabel(date: string, style: 'long' | 'short' = 'long'): string {
	if (!date) return 'Unknown date';
	const [year, month, day] = date.split('-').map(Number);
	const value = new Date(year, month - 1, day);
	const today = new Date();
	const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1);
	if (value.toDateString() === today.toDateString()) return 'Today';
	if (value.toDateString() === yesterday.toDateString()) return 'Yesterday';
	return value.toLocaleDateString('zh-CN', style === 'long'
		? { year: 'numeric', month: 'long', day: 'numeric', weekday: 'short' }
		: { month: 'short', day: 'numeric' });
}

/**
 * Svelte action: calls `callback` whenever the node is in (or near) view, for
 * infinite scrolling. Every parameter change re-checks, so pass something
 * that changes after each page (such as the item count): a short page leaves
 * the sentinel visible, which would otherwise never fire again.
 */
export function inView(node: HTMLElement, params: { callback: () => void; root?: HTMLElement | null; key?: unknown }) {
	let current = params;
	const observer = new IntersectionObserver(
		(entries) => {
			if (entries.some((entry) => entry.isIntersecting)) current.callback();
		},
		{ root: params.root ?? null, rootMargin: '400px 0px' }
	);
	observer.observe(node);
	return {
		update(next: typeof params) {
			current = next;
			observer.unobserve(node);
			observer.observe(node);
		},
		destroy() {
			observer.disconnect();
		}
	};
}
