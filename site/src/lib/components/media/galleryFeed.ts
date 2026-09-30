import { get, writable, type Readable } from 'svelte/store';
import { t, getIntlLocale } from '$lib/i18n';
import { fetchMediaPage, getMediaFileUrl, type MediaWithDiary } from '$lib/api/media';
import { variantUrl } from '$lib/utils/imageDisplay';

export interface GalleryItem {
	key: string;
	/** Full-size image, as inserted into entries. */
	src: string;
	/** Thumbnail variant for grids; falls back to `src` if it fails. */
	thumb: string;
	title: string;
	/** The image's own day (YYYY-MM-DD): see imageDay. '' when unknown. */
	date: string;
	/** Stored by Diarum (local or S3): can be selected and deleted. External images (e.g. Chevereto) cannot. */
	managed: boolean;
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
	removeMany: (keys: string[]) => void;
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

/**
 * The day an image belongs to on the timeline: its own date (the day of the
 * first entry it was used in), not when it was uploaded. Images never used in
 * an entry have no date yet and fall back to their upload day.
 */
export function imageDay(media: Pick<MediaWithDiary, 'date' | 'created'>): string {
	if (media.date && /^\d{4}-\d{2}-\d{2}/.test(media.date)) return media.date.slice(0, 10);
	return localDay(media.created);
}

function fromMedia(media: MediaWithDiary): GalleryItem {
	const external = media.kind === 'external' && !!media.url;
	const src = external ? media.url! : getMediaFileUrl(media);
	return {
		key: external ? `ext:${media.url}` : media.id ?? media.file ?? '',
		managed: !external,
		src,
		thumb: variantUrl(src, 'th') ?? src,
		title: media.name || media.alt || 'Image',
		date: imageDay(media),
		media
	};
}

/**
 * Paged, append-only list of the built-in media library. Responses that
 * arrive after a reload or destroy are discarded.
 */
export interface GalleryFeedOptions<P extends GalleryPage = GalleryPage> {
	pageSize?: number;
	/** Where pages come from; defaults to the media library. */
	fetchPage?: (page: number, perPage: number, signal: AbortSignal) => Promise<P>;
	/** Called with every page that arrives, e.g. to read extra fields. */
	onPage?: (page: P) => void;
}

export interface GalleryPage {
	items: MediaWithDiary[];
	totalPages: number;
	totalItems: number;
}

export function createGalleryFeed<P extends GalleryPage = GalleryPage>(options: GalleryFeedOptions<P> = {}): GalleryFeed {
	const pageSize = options.pageSize ?? 30;
	const fetchPage = options.fetchPage ?? (fetchMediaPage as unknown as (page: number, perPage: number, signal: AbortSignal) => Promise<P>);
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
			// Ask for the page holding the first item not loaded yet. Counting
			// what is loaded, rather than the pages fetched, keeps removals
			// (which shift the server's pages) from skipping images; any overlap
			// is dropped below.
			const page = Math.floor(state.items.length / pageSize) + 1;
			const result = await fetchPage(page, pageSize, controller.signal);
			if (current !== generation) return;
			options.onPage?.(result);
			const seen = new Set(state.items.map((item) => item.key));
			const fresh = result.items.map((media) => fromMedia(media)).filter((item) => !seen.has(item.key));
			const items = [...state.items, ...fresh];
			set({
				items,
				hasMore: fresh.length > 0 && page < result.totalPages,
				total: result.totalItems
			});
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

	function removeMany(keys: string[]) {
		const drop = new Set(keys);
		const items = state.items.filter((item) => !drop.has(item.key));
		const removed = state.items.length - items.length;
		set({ items, total: state.total === null ? null : Math.max(0, state.total - removed) });
	}

	function destroy() {
		generation++;
		controller?.abort();
	}

	return { subscribe: store.subscribe, loadMore, reload, remove, removeMany, destroy };
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
	const translate = get(t);
	if (!date) return translate('mediaLib.library.unknownDate');
	const [year, month, day] = date.split('-').map(Number);
	const value = new Date(year, month - 1, day);
	const today = new Date();
	const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1);
	if (value.toDateString() === today.toDateString()) return translate('mediaLib.library.today');
	if (value.toDateString() === yesterday.toDateString()) return translate('mediaLib.library.yesterday');
	return value.toLocaleDateString(getIntlLocale(), style === 'long'
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
