import { get } from 'svelte/store';
import { browser } from '$app/environment';
import { translate, getCurrentLocale } from '$lib/i18n';
import { liveDefaultMode } from '$lib/utils/imageDisplay';
import { isLiveMode, LIVE_MODES, type LiveMode } from '$lib/utils/livePhoto';

/**
 * Plays live photos in place: the clip fades in over the still while it
 * plays and back out when it stops, so the still is what shows whenever the
 * clip is not playing, loading, or cannot play in this browser.
 *
 * Each photo plays by its own mode (data-live-mode in the entry) or else the
 * default from settings: loop while on screen, play once when it comes into
 * view, or not at all. The LIVE badge in the top-left corner changes the
 * photo's mode and plays it on demand. Clips are only loaded once a photo is
 * on screen and are released when it leaves.
 */

function tr(key: string, params?: Record<string, string | number>) {
	return translate(getCurrentLocale(), key, params);
}

export function modeLabel(mode: LiveMode): string {
	return tr(`live.${mode}`);
}

/**
 * Choices made where entries are read-only, by clip URL, for this session:
 * a mode, or 'default' to follow the default despite the entry's own mode.
 */
const sessionModes = new Map<string, LiveMode | 'default'>();

/** The photo's own mode: the session's choice where read-only, else the entry's. */
function ownModeOf(entryMode: LiveMode | null, src: string, editable: boolean): LiveMode | null {
	if (editable) return entryMode;
	const session = sessionModes.get(src);
	if (session === 'default') return null;
	return session ?? entryMode;
}

function resolveMode(own: LiveMode | null, fallback: LiveMode): LiveMode {
	if (own) return own;
	// Reduced motion or data saver: no endless loops unless asked for.
	return prefersStill() && fallback === 'loop' ? 'once' : fallback;
}

function prefersStill(): boolean {
	if (!browser) return true;
	const connection = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection;
	return !!connection?.saveData || window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
}

// One observer for every photo on the page.
const players = new Map<Element, LivePhoto>();
let observer: IntersectionObserver | null = null;

function observe(player: LivePhoto, target: Element) {
	if (typeof IntersectionObserver === 'undefined') {
		player.setVisibility(true, true);
		return;
	}
	observer ??= new IntersectionObserver(
		(entries) => {
			for (const entry of entries) {
				const visible =
					entry.isIntersecting &&
					(entry.intersectionRatio >= 0.5 || entry.intersectionRect.height >= window.innerHeight * 0.5);
				players.get(entry.target)?.setVisibility(visible, entry.isIntersecting);
			}
		},
		{ threshold: [0, 0.25, 0.5, 0.75, 1] }
	);
	players.set(target, player);
	observer.observe(target);
}

function unobserve(target: Element) {
	players.delete(target);
	observer?.unobserve(target);
}

const ICON_LIVE = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><circle cx="12" cy="12" r="2.6" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="5.6"/><circle cx="12" cy="12" r="9.2" stroke-dasharray="1.6 2.4"/><path class="live-off-slash" d="M4 4l16 16"/></svg>`;
const ICON_CHEVRON = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" aria-hidden="true"><path d="M7 10l5 5 5-5"/></svg>`;
const ICON_CHECK = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5"/></svg>`;
const ICON_PLAY = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5.5v13l11-6.5z"/></svg>`;

export interface LivePhotoOptions {
	/** The photo's own mode; null follows the default. */
	mode: LiveMode | null;
	/**
	 * Saves a mode chosen from the badge (null: follow the default). Without
	 * it, the choice lasts for this session only.
	 */
	onModeChange?: (mode: LiveMode | null) => void;
}

export class LivePhoto {
	private video: HTMLVideoElement | null = null;
	private controls: HTMLDivElement;
	private badge: HTMLButtonElement;
	private badgeText: HTMLSpanElement;
	private menu: HTMLDivElement;
	private visible = false;
	private nearby = false;
	private playedOnce = false;
	/** Playing because it was asked for (badge hover, Play now), not by mode. */
	private preview = false;
	private failed = false;
	private blocked = false;
	private destroyed = false;
	private unsubscribeDefault: () => void;
	private defaultMode: LiveMode = 'loop';
	private hideTimer: ReturnType<typeof setTimeout> | undefined;

	constructor(
		private readonly wrapper: HTMLElement,
		private readonly img: HTMLImageElement,
		private src: string,
		private options: LivePhotoOptions
	) {
		wrapper.classList.add('live-photo');
		this.controls = document.createElement('div');
		this.controls.className = 'live-controls';
		this.controls.contentEditable = 'false';
		this.controls.dataset.lightboxIgnore = '';

		this.badge = document.createElement('button');
		this.badge.type = 'button';
		this.badge.className = 'live-badge';
		this.badge.setAttribute('aria-haspopup', 'menu');
		this.badge.setAttribute('aria-expanded', 'false');
		this.badgeText = document.createElement('span');
		this.badgeText.className = 'live-badge-text';
		this.badge.innerHTML = ICON_LIVE;
		this.badge.append(this.badgeText);
		this.badge.insertAdjacentHTML('beforeend', ICON_CHEVRON);

		this.menu = document.createElement('div');
		this.menu.className = 'live-menu';
		this.menu.setAttribute('role', 'menu');
		this.menu.hidden = true;

		this.controls.append(this.badge, this.menu);
		wrapper.appendChild(this.controls);

		this.badge.addEventListener('click', this.handleBadgeClick);
		this.badge.addEventListener('pointerenter', this.handleBadgeEnter);
		this.badge.addEventListener('pointerleave', this.handleBadgeLeave);
		// Keep clicks on the controls away from the editor, lightbox and links.
		for (const type of ['mousedown', 'pointerdown', 'dblclick'] as const) {
			this.controls.addEventListener(type, (event) => event.stopPropagation());
		}

		this.unsubscribeDefault = liveDefaultMode.subscribe((mode) => {
			this.defaultMode = mode;
			this.refresh();
		});
		observe(this, wrapper);
		this.refresh();
	}

	/** The mode in effect: the photo's own, else the default. */
	get mode(): LiveMode {
		return resolveMode(this.ownMode, this.defaultMode);
	}

	private get ownMode(): LiveMode | null {
		return ownModeOf(this.options.mode, this.src, !!this.options.onModeChange);
	}

	update(src: string, mode: LiveMode | null) {
		const changedSrc = src !== this.src;
		const changedMode = mode !== this.options.mode;
		if (!changedSrc && !changedMode) return;
		this.options = { ...this.options, mode };
		if (changedSrc) {
			this.src = src;
			this.failed = false;
			this.releaseVideo();
		}
		this.playedOnce = false;
		this.refresh();
	}

	setVisibility(visible: boolean, nearby: boolean) {
		this.visible = visible;
		this.nearby = nearby;
		if (!visible) this.closeMenu();
		this.evaluate();
		// Off screen entirely: give the decoder and memory back.
		if (!nearby) this.releaseVideo();
	}

	destroy() {
		this.destroyed = true;
		this.unsubscribeDefault();
		unobserve(this.wrapper);
		this.closeMenu();
		this.releaseVideo();
		clearTimeout(this.hideTimer);
		this.controls.remove();
		this.wrapper.classList.remove('live-photo', 'live-playing', 'live-unsupported', 'live-off');
	}

	private refresh() {
		const mode = this.mode;
		this.badgeText.textContent = tr('live.badge');
		this.wrapper.classList.toggle('live-off', mode === 'off');
		this.wrapper.classList.toggle('live-unsupported', this.failed);
		this.badge.title = this.failed ? tr('live.unsupported') : tr('live.modeTitle', { mode: modeLabel(mode) });
		this.badge.setAttribute('aria-label', this.badge.title);
		if (!this.menu.hidden) this.renderMenu();
		this.evaluate();
	}

	/** Starts or stops the clip to match the mode and whether it is on screen. */
	private evaluate() {
		if (this.destroyed || this.preview) return;
		const mode = this.mode;
		const shouldPlay =
			this.visible && !this.failed && !this.blocked && (mode === 'loop' || (mode === 'once' && !this.playedOnce));
		if (shouldPlay) this.play(mode === 'loop');
		else this.stop();
	}

	private ensureVideo(): HTMLVideoElement {
		if (this.video) return this.video;
		const video = document.createElement('video');
		video.className = 'live-video';
		video.muted = true;
		video.defaultMuted = true;
		video.playsInline = true;
		video.setAttribute('playsinline', '');
		video.setAttribute('muted', '');
		video.setAttribute('aria-hidden', 'true');
		video.disablePictureInPicture = true;
		video.disableRemotePlayback = true;
		video.preload = 'auto';
		video.tabIndex = -1;
		video.addEventListener('playing', () => {
			clearTimeout(this.hideTimer);
			this.wrapper.classList.add('live-playing');
		});
		video.addEventListener('ended', this.handleEnded);
		video.addEventListener('error', () => {
			if (video !== this.video) return;
			this.failed = true;
			this.preview = false;
			this.releaseVideo();
			this.refresh();
		});
		video.src = this.src;
		this.img.insertAdjacentElement('afterend', video);
		this.video = video;
		return video;
	}

	private play(loop: boolean) {
		const video = this.ensureVideo();
		video.loop = loop;
		if (!video.paused && !video.ended) return;
		if (video.ended || !this.wrapper.classList.contains('live-playing')) video.currentTime = 0;
		video.play().catch((error: unknown) => {
			// Autoplay refused (e.g. iOS Low Power Mode): wait for a tap on
			// the badge, which counts as a user gesture.
			if (error instanceof DOMException && error.name === 'NotAllowedError') {
				this.blocked = true;
				this.preview = false;
				this.stop();
			}
		});
	}

	private stop() {
		const video = this.video;
		this.wrapper.classList.remove('live-playing');
		if (!video || video.paused) return;
		// Let the still fade back in before the clip rewinds underneath it.
		clearTimeout(this.hideTimer);
		this.hideTimer = setTimeout(() => {
			if (this.video === video && !this.wrapper.classList.contains('live-playing')) {
				video.pause();
				video.currentTime = 0;
			}
		}, 300);
	}

	private releaseVideo() {
		const video = this.video;
		if (!video) return;
		this.video = null;
		this.wrapper.classList.remove('live-playing');
		video.pause();
		video.removeAttribute('src');
		video.load();
		video.remove();
	}

	private handleEnded = () => {
		if (this.preview) {
			this.preview = false;
			this.wrapper.classList.remove('live-playing');
			// Back to what the mode wants (a looping photo carries on).
			setTimeout(() => this.evaluate(), 300);
			return;
		}
		this.playedOnce = true;
		this.evaluate();
	};

	/** Plays the clip once from the start, whatever the mode. */
	playNow() {
		if (this.failed) return;
		this.blocked = false;
		this.preview = true;
		const video = this.ensureVideo();
		video.loop = false;
		video.currentTime = 0;
		video.play().catch(() => {
			this.preview = false;
			this.evaluate();
		});
	}

	private handleBadgeEnter = (event: PointerEvent) => {
		if (event.pointerType !== 'mouse' || this.failed || !this.menu.hidden) return;
		const video = this.video;
		if (video && !video.paused && this.wrapper.classList.contains('live-playing')) return;
		this.playNow();
	};

	private handleBadgeLeave = (event: PointerEvent) => {
		if (event.pointerType !== 'mouse' || !this.preview) return;
		this.preview = false;
		this.evaluate();
	};

	private handleBadgeClick = (event: MouseEvent) => {
		event.preventDefault();
		event.stopPropagation();
		if (this.menu.hidden) this.openMenu();
		else this.closeMenu();
	};

	private openMenu() {
		this.renderMenu();
		this.menu.hidden = false;
		this.badge.setAttribute('aria-expanded', 'true');
		this.wrapper.classList.add('live-menu-open');
		document.addEventListener('pointerdown', this.handleOutside, true);
		document.addEventListener('keydown', this.handleKeydown, true);
		this.menu.querySelector<HTMLButtonElement>('[aria-checked="true"]')?.focus({ preventScroll: true });
	}

	private closeMenu() {
		if (this.menu.hidden) return;
		this.menu.hidden = true;
		this.badge.setAttribute('aria-expanded', 'false');
		this.wrapper.classList.remove('live-menu-open');
		document.removeEventListener('pointerdown', this.handleOutside, true);
		document.removeEventListener('keydown', this.handleKeydown, true);
	}

	private handleOutside = (event: PointerEvent) => {
		if (!this.controls.contains(event.target as Node)) this.closeMenu();
	};

	private handleKeydown = (event: KeyboardEvent) => {
		if (event.key === 'Escape') {
			event.preventDefault();
			event.stopPropagation();
			this.closeMenu();
			this.badge.focus({ preventScroll: true });
			return;
		}
		if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
		const items = Array.from(this.menu.querySelectorAll<HTMLButtonElement>('button'));
		const current = items.indexOf(document.activeElement as HTMLButtonElement);
		const next = (current + (event.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length;
		event.preventDefault();
		items[next]?.focus({ preventScroll: true });
	};

	private renderMenu() {
		const own = this.ownMode;
		const item = (label: string, checked: boolean | null, onSelect: () => void, icon = '') => {
			const button = document.createElement('button');
			button.type = 'button';
			button.className = 'live-menu-item';
			if (checked === null) {
				button.setAttribute('role', 'menuitem');
			} else {
				button.setAttribute('role', 'menuitemradio');
				button.setAttribute('aria-checked', String(checked));
			}
			const mark = document.createElement('span');
			mark.className = 'live-menu-mark';
			mark.innerHTML = icon || (checked ? ICON_CHECK : '');
			const text = document.createElement('span');
			text.textContent = label;
			button.append(mark, text);
			button.addEventListener('click', (event) => {
				event.preventDefault();
				event.stopPropagation();
				onSelect();
			});
			return button;
		};

		const heading = document.createElement('div');
		heading.className = 'live-menu-heading';
		heading.textContent = this.failed ? tr('live.unsupported') : tr('live.menuLabel');
		const items: HTMLElement[] = [heading];
		if (!this.failed) {
			items.push(item(tr('live.playNow'), null, () => {
				this.closeMenu();
				this.playNow();
			}, ICON_PLAY));
			const divider = document.createElement('div');
			divider.className = 'live-menu-divider';
			items.push(divider);
		}
		items.push(item(tr('live.followDefault', { mode: modeLabel(this.defaultMode) }), own === null, () => this.choose(null)));
		for (const mode of LIVE_MODES) {
			items.push(item(modeLabel(mode), own === mode, () => this.choose(mode)));
		}
		this.menu.replaceChildren(...items);
		this.menu.setAttribute('aria-label', tr('live.menuLabel'));
	}

	private choose(mode: LiveMode | null) {
		this.closeMenu();
		this.blocked = false;
		this.preview = false;
		const save = this.options.onModeChange;
		if (save) {
			this.options = { ...this.options, mode };
			save(mode);
		} else {
			sessionModes.set(this.src, mode ?? 'default');
		}
		// Show the choice right away: a looping or play-once photo starts over.
		this.playedOnce = false;
		this.refresh();
	}
}

/** Reads data-live-mode, ignoring anything that is not a known mode. */
export function readLiveMode(value: unknown): LiveMode | null {
	return isLiveMode(value) ? value : null;
}

/**
 * Svelte action for read-only entry HTML: every live photo frame that
 * `withDisplayImages` put around an image with a clip gets the player.
 * Content swapped in later is picked up too. The player only adds elements
 * inside the frame, so the HTML Svelte inserted keeps its own nodes.
 */
export function livePhotos(node: HTMLElement) {
	const attached = new Map<HTMLElement, LivePhoto>();

	function scan() {
		for (const [frame, player] of attached) {
			if (!node.contains(frame)) {
				player.destroy();
				attached.delete(frame);
			}
		}
		for (const frame of node.querySelectorAll<HTMLElement>('.live-photo-frame')) {
			const img = frame.querySelector<HTMLImageElement>(':scope > img[data-live-video]');
			if (!img) continue;
			const src = img.dataset.liveVideo!;
			const mode = readLiveMode(img.dataset.liveMode);
			const existing = attached.get(frame);
			if (existing) existing.update(src, mode);
			else attached.set(frame, new LivePhoto(frame, img, src, { mode }));
		}
	}

	let scheduled = false;
	const mutations = new MutationObserver((records) => {
		// The players' own changes inside frames need no rescan.
		if (scheduled || records.every((record) => (record.target as Element).closest?.('.live-photo-frame'))) return;
		scheduled = true;
		queueMicrotask(() => {
			scheduled = false;
			scan();
		});
	});
	scan();
	mutations.observe(node, { childList: true, subtree: true });

	return {
		destroy() {
			mutations.disconnect();
			for (const player of attached.values()) player.destroy();
			attached.clear();
		}
	};
}

/** The mode a photo plays in, for code outside the players (the lightbox). */
export function effectiveLiveMode(entryMode: LiveMode | null, src: string): LiveMode {
	return resolveMode(ownModeOf(entryMode, src, false), get(liveDefaultMode));
}
