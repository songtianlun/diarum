import { uploadImage, isCheveretoResult, getMediaUrl, getMediaLiveUrl, getMediaPosterUrl, UploadAbortedError } from '$lib/utils/uploadImage';
import { prepareUpload, isHeicFile, isVideoFile, type UploadSource, type PreparedUpload } from '$lib/utils/livePhoto';
import { probeVideo } from '$lib/utils/video';

export type UploadStatus = 'queued' | 'preparing' | 'uploading' | 'finishing' | 'done' | 'error';

/** Something worth telling the user about an upload that still went ahead. */
export type UploadNotice =
	| { kind: 'liveDroppedChevereto'; fileName: string }
	| { kind: 'liveVideoTooLarge'; fileName: string }
	| { kind: 'liveVideoInvalid'; fileName: string }
	/** This browser could not decode the video; others may still play it. */
	| { kind: 'videoNoPreview'; fileName: string };

export interface UploadState {
	status: UploadStatus;
	/** 0–100, meaningful while uploading */
	progress: number;
	fileName: string;
	previewUrl: string;
	url?: string;
	/** The uploaded live photo clip, when the image is a live photo. */
	liveUrl?: string;
	/** Set for a video upload; `url` is then the video itself. */
	video?: boolean;
	/** The uploaded video's poster frame. */
	posterUrl?: string;
	error?: string;
}

type Listener = (state: UploadState) => void;

interface Task {
	id: string;
	source: UploadSource;
	/** Normalised once and kept for retries. */
	prepared?: PreparedUpload & { poster?: File };
	state: UploadState;
	listeners: Set<Listener>;
	controller?: AbortController;
}

const DEFAULT_CONCURRENCY = 3;
// Stands in for the preview of an image the browser cannot show yet (HEIC).
const BLANK_PREVIEW = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';
// How long to wait for the uploaded image to load before swapping anyway.
const PRELOAD_TIMEOUT_MS = 15000;

function preloadImage(url: string): Promise<void> {
	return new Promise((resolve) => {
		const img = new Image();
		const timer = setTimeout(resolve, PRELOAD_TIMEOUT_MS);
		const finish = () => {
			clearTimeout(timer);
			resolve();
		};
		img.onload = () => {
			// decode() avoids a blank frame when the element swaps its src.
			img.decode?.().then(finish, finish) ?? finish();
		};
		img.onerror = finish;
		img.src = url;
	});
}

/**
 * Runs image and video uploads for one editor with bounded concurrency. Each upload is
 * shown in the document as a placeholder node that follows its task's state;
 * the editor swaps the placeholder for the final image once the task is done.
 */
export class UploadQueue {
	private tasks = new Map<string, Task>();
	private pending: string[] = [];
	private active = 0;
	private destroyed = false;

	constructor(
		private readonly hooks: {
			/** A task reached `done` or `error`. */
			onSettled: (id: string) => void;
			/** Any task changed state; for aggregate progress displays. */
			onActivity?: () => void;
			/** An upload went ahead with a caveat, e.g. a live photo kept as a still. */
			onNotice?: (notice: UploadNotice) => void;
		},
		private readonly concurrency = DEFAULT_CONCURRENCY
	) {}

	/** Queues an image, a video, or a live photo given as `{ file, video }`. */
	add(input: File | UploadSource): string {
		const source = input instanceof File ? { file: input } : input;
		const { file } = source;
		const video = !source.video && isVideoFile(file);
		const id = `upload-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
		// Most browsers cannot show HEIC: its preview appears once converted.
		// A video's preview is its poster frame, once captured.
		const previewUrl = video || isHeicFile(file) ? BLANK_PREVIEW : URL.createObjectURL(file);
		this.tasks.set(id, {
			id,
			source,
			listeners: new Set(),
			state: { status: 'queued', progress: 0, fileName: file.name, previewUrl, video: video || undefined }
		});
		this.pending.push(id);
		this.pump();
		this.hooks.onActivity?.();
		return id;
	}

	get(id: string): UploadState | undefined {
		return this.tasks.get(id)?.state;
	}

	/** Number of uploads that have not finished yet (failed ones excluded). */
	get busyCount(): number {
		let count = 0;
		for (const task of this.tasks.values()) {
			if (task.state.status !== 'done' && task.state.status !== 'error') count++;
		}
		return count;
	}

	subscribe(id: string, listener: Listener): () => void {
		const task = this.tasks.get(id);
		if (!task) return () => {};
		task.listeners.add(listener);
		listener(task.state);
		return () => task.listeners.delete(listener);
	}

	retry(id: string) {
		const task = this.tasks.get(id);
		if (!task || task.state.status !== 'error') return;
		this.update(task, { status: 'queued', progress: 0, error: undefined });
		this.pending.push(id);
		this.pump();
	}

	/** Stops the upload (if running) and forgets the task. */
	cancel(id: string) {
		const task = this.tasks.get(id);
		if (!task) return;
		task.controller?.abort();
		this.pending = this.pending.filter((pendingId) => pendingId !== id);
		this.forget(task);
		this.hooks.onActivity?.();
	}

	/** Called once the editor has put the final image in place. */
	release(id: string) {
		const task = this.tasks.get(id);
		if (task) this.forget(task);
	}

	/** Aborts and forgets every task, e.g. when the editor loads another entry. */
	clear() {
		this.pending = [];
		for (const task of [...this.tasks.values()]) {
			task.controller?.abort();
			this.forget(task);
		}
		this.hooks.onActivity?.();
	}

	destroy() {
		this.destroyed = true;
		this.clear();
	}

	private forget(task: Task) {
		this.tasks.delete(task.id);
		task.listeners.clear();
		// Keep the blob alive briefly: the image element may still be painting it.
		const previewUrl = task.state.previewUrl;
		if (previewUrl.startsWith('blob:')) setTimeout(() => URL.revokeObjectURL(previewUrl), 2000);
	}

	private update(task: Task, patch: Partial<UploadState>) {
		task.state = { ...task.state, ...patch };
		for (const listener of task.listeners) listener(task.state);
		this.hooks.onActivity?.();
	}

	private pump() {
		while (!this.destroyed && this.active < this.concurrency && this.pending.length > 0) {
			const task = this.tasks.get(this.pending.shift()!);
			if (task) void this.run(task);
		}
	}

	/**
	 * Splits off a live photo clip and converts HEIC, or captures a video's
	 * poster frame, once per task.
	 */
	private async prepare(task: Task): Promise<PreparedUpload & { poster?: File }> {
		if (task.prepared) return task.prepared;
		this.update(task, { status: 'preparing', progress: 0 });
		if (task.state.video) {
			const probe = await probeVideo(task.source.file);
			task.prepared = { still: task.source.file, poster: probe.poster };
			if (!probe.playable) this.hooks.onNotice?.({ kind: 'videoNoPreview', fileName: task.state.fileName });
			if (probe.poster && this.tasks.has(task.id)) this.update(task, { previewUrl: URL.createObjectURL(probe.poster) });
			return task.prepared;
		}
		const prepared = await prepareUpload(task.source);
		task.prepared = prepared;
		if (prepared.still !== task.source.file && this.tasks.has(task.id)) {
			const previous = task.state.previewUrl;
			this.update(task, { previewUrl: URL.createObjectURL(prepared.still) });
			if (previous.startsWith('blob:')) setTimeout(() => URL.revokeObjectURL(previous), 2000);
		}
		return prepared;
	}

	private async run(task: Task) {
		this.active++;
		const controller = new AbortController();
		task.controller = controller;
		try {
			const prepared = await this.prepare(task);
			if (controller.signal.aborted || !this.tasks.has(task.id)) return;
			this.update(task, { status: 'uploading', progress: 0 });
			const result = await uploadImage(prepared.still, {
				live: prepared.video,
				poster: prepared.poster,
				signal: controller.signal,
				onProgress: ({ percentage }) => {
					if (task.state.status === 'uploading') this.update(task, { progress: percentage });
				}
			});
			const fileName = task.state.fileName;
			if (prepared.videoSkipped === 'too-large') this.hooks.onNotice?.({ kind: 'liveVideoTooLarge', fileName });
			if (prepared.videoSkipped === 'invalid') this.hooks.onNotice?.({ kind: 'liveVideoInvalid', fileName });
			let url: string;
			let liveUrl: string | undefined;
			let posterUrl: string | undefined;
			if (isCheveretoResult(result)) {
				url = result.cheveretoUrl;
				if (result.liveDropped) this.hooks.onNotice?.({ kind: 'liveDroppedChevereto', fileName });
			} else {
				url = getMediaUrl(result);
				liveUrl = getMediaLiveUrl(result) ?? undefined;
				posterUrl = getMediaPosterUrl(result) ?? undefined;
			}
			this.update(task, { status: 'finishing', progress: 100, url, liveUrl, posterUrl });
			// A video is streamed when played: only its poster is worth loading now.
			if (!task.state.video) await preloadImage(url);
			else if (posterUrl) await preloadImage(posterUrl);
			if (!this.tasks.has(task.id)) return;
			this.update(task, { status: 'done' });
			this.hooks.onSettled(task.id);
		} catch (error) {
			if (error instanceof UploadAbortedError || !this.tasks.has(task.id)) return;
			this.update(task, {
				status: 'error',
				error: error instanceof Error ? error.message : 'Upload failed'
			});
			this.hooks.onSettled(task.id);
		} finally {
			task.controller = undefined;
			this.active--;
			this.pump();
		}
	}
}
