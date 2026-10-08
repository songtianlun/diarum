/**
 * Live photos: a still image plus the short clip taken with it.
 *
 * Phones store them in two ways, and both are normalised here to one shape,
 * `{ still, video }`, before upload. The server keeps the clip beside the
 * still (photo.live.mp4) and entries mark the image with data-live-video.
 *
 * - Paired files: an Apple Live Photo is a HEIC/JPEG still and a QuickTime
 *   .MOV with the same name (shared with "All Photos Data", AirDropped, or
 *   exported from a Mac). Any still and clip that share a name pair up the
 *   same way, and a picker that hands over exactly one image and one video
 *   pairs those too, whatever their names.
 * - Embedded clips: Android Motion Photo 1.0 and the older Google MicroVideo
 *   format append an MP4 to a JPEG or HEIC still, described in its XMP. Pixel,
 *   Samsung, Xiaomi, OPPO, vivo, Honor, Huawei and HarmonyOS moving photos all
 *   use this layout, some with vendor data after the clip (Samsung's SEF
 *   trailer, Huawei's older LIVE_ marker). The clip is found from the XMP
 *   offsets when present and otherwise by scanning for an MP4 that parses as
 *   one, then cut out; vendor trailers are left behind.
 *
 * HEIC stills are converted to JPEG, which every browser can show: natively
 * where the browser decodes HEIC (Safari), otherwise with a WebAssembly
 * decoder loaded only when needed.
 */

export type LiveMode = 'loop' | 'once' | 'off';

export const LIVE_MODES: LiveMode[] = ['loop', 'once', 'off'];

export function isLiveMode(value: unknown): value is LiveMode {
	return value === 'loop' || value === 'once' || value === 'off';
}

/** Must match imaging.MaxLiveVideoBytes on the server. */
export const LIVE_VIDEO_MAX_BYTES = 50 * 1024 * 1024;
/** Largest source file read for an embedded clip (a still plus its video). */
export const LIVE_SOURCE_MAX_BYTES = 120 * 1024 * 1024;

const VIDEO_EXTENSION = /\.(mov|mp4|m4v|qt)$/i;
const HEIC_EXTENSION = /\.(heic|heif|hif)$/i;
const IMAGE_EXTENSION = /\.(jpe?g|png|gif|webp|svg|heic|heif|hif)$/i;

/** ftyp brands of still images (HEIF/HEIC/AVIF), as opposed to video. */
const IMAGE_BRANDS = new Set(['heic', 'heix', 'heim', 'heis', 'hevc', 'hevx', 'hevm', 'hevs', 'mif1', 'msf1', 'avif', 'avis', 'avio']);
const HEIF_BRANDS = new Set(['heic', 'heix', 'heim', 'heis', 'hevc', 'hevx', 'mif1', 'msf1']);

export function isVideoFile(file: File): boolean {
	return file.type.startsWith('video/') || (!file.type.startsWith('image/') && VIDEO_EXTENSION.test(file.name));
}

export function isHeicFile(file: File): boolean {
	return /^image\/hei[cf](-sequence)?$/.test(file.type) || HEIC_EXTENSION.test(file.name);
}

/** Images we can upload, directly or after conversion (HEIC). */
export function isImageFile(file: File): boolean {
	return file.type.startsWith('image/') || (!file.type && IMAGE_EXTENSION.test(file.name)) || isHeicFile(file);
}

function stem(name: string): string {
	const slash = Math.max(name.lastIndexOf('/'), name.lastIndexOf('\\'));
	const base = name.slice(slash + 1);
	const dot = base.lastIndexOf('.');
	return dot > 0 ? base.slice(0, dot) : base;
}

function withExtension(name: string, ext: string): string {
	return `${stem(name) || 'image'}.${ext}`;
}

export interface UploadSource {
	/** The still image (may still be HEIC or carry an embedded clip). */
	file: File;
	/** The clip picked alongside it, for paired live photos. */
	video?: File;
}

/**
 * Groups picked or dropped files into uploads: each image with the video of
 * the same name (an Apple Live Photo), and a lone image with a lone video.
 * Videos that pair with nothing are returned separately; they are not
 * uploaded on their own.
 */
export function groupLiveFiles(files: File[]): { sources: UploadSource[]; strayVideos: File[] } {
	const images = files.filter((file) => !isVideoFile(file) && isImageFile(file));
	const videos = files.filter(isVideoFile);
	const videosByStem = new Map<string, File>();
	for (const video of videos) {
		const key = stem(video.name).toLowerCase();
		if (key && !videosByStem.has(key)) videosByStem.set(key, video);
	}

	const used = new Set<File>();
	const sources: UploadSource[] = images.map((file) => {
		const video = videosByStem.get(stem(file.name).toLowerCase());
		if (video && !used.has(video)) {
			used.add(video);
			return { file, video };
		}
		return { file };
	});

	let strayVideos = videos.filter((video) => !used.has(video));
	const unpaired = sources.filter((source) => !source.video);
	if (unpaired.length === 1 && strayVideos.length === 1) {
		unpaired[0].video = strayVideos[0];
		strayVideos = [];
	}
	return { sources, strayVideos };
}

// ---------------------------------------------------------------------------
// Byte-level parsing

function u32(bytes: Uint8Array, at: number): number {
	return ((bytes[at] << 24) >>> 0) + (bytes[at + 1] << 16) + (bytes[at + 2] << 8) + bytes[at + 3];
}

function ascii(bytes: Uint8Array, at: number, length: number): string {
	let out = '';
	for (let i = 0; i < length; i++) out += String.fromCharCode(bytes[at + i]);
	return out;
}

function isBoxType(bytes: Uint8Array, at: number): boolean {
	for (let i = 0; i < 4; i++) {
		const c = bytes[at + i];
		// Printable ASCII, plus © (0xA9) which QuickTime uses in some types.
		if (!((c >= 0x20 && c <= 0x7e) || c === 0xa9)) return false;
	}
	return true;
}

function includesAscii(bytes: Uint8Array, from: number, to: number, text: string): boolean {
	const first = text.charCodeAt(0);
	const end = Math.min(to, bytes.length) - text.length;
	outer: for (let i = from; i <= end; i++) {
		if (bytes[i] !== first) continue;
		for (let j = 1; j < text.length; j++) {
			if (bytes[i + j] !== text.charCodeAt(j)) continue outer;
		}
		return true;
	}
	return false;
}

/** The major brand when `bytes` starts an ISO base media file, else ''. */
function majorBrand(bytes: Uint8Array, at = 0): string {
	if (at + 12 > bytes.length || ascii(bytes, at + 4, 4) !== 'ftyp') return '';
	return ascii(bytes, at + 8, 4).toLowerCase().trim();
}

/**
 * If an MP4/QuickTime video with a video track starts at `start`, returns
 * where it ends: after its last well-formed top-level box, so trailing vendor
 * data is excluded. Returns -1 otherwise.
 */
export function mp4End(bytes: Uint8Array, start: number, limit = bytes.length): number {
	const brand = majorBrand(bytes, start);
	if (!brand || IMAGE_BRANDS.has(brand)) return -1;
	let pos = start;
	let hasVideoTrack = false;
	let hasMedia = false;
	while (pos + 8 <= limit) {
		let size = u32(bytes, pos);
		let header = 8;
		if (!isBoxType(bytes, pos + 4)) break;
		if (size === 1) {
			if (pos + 16 > limit) break;
			size = u32(bytes, pos + 8) * 2 ** 32 + u32(bytes, pos + 12);
			header = 16;
		} else if (size === 0) {
			size = limit - pos;
		}
		if (size < header || pos + size > limit) break;
		const type = ascii(bytes, pos + 4, 4);
		if (type === 'moov') hasVideoTrack = includesAscii(bytes, pos + header, pos + size, 'vide');
		if (type === 'mdat') hasMedia = true;
		pos += size;
	}
	return hasVideoTrack && hasMedia ? pos : -1;
}

/** Where the XMP says the clip starts, newest scheme first. */
function xmpVideoStarts(bytes: Uint8Array): number[] {
	// XMP sits in the first segments of a JPEG, or in a HEIF item near the start.
	const head = ascii(bytes, 0, Math.min(bytes.length, 512 * 1024));
	const xmpStart = head.indexOf('<x:xmpmeta');
	if (xmpStart < 0) return [];
	const xmpEnd = head.indexOf('</x:xmpmeta>', xmpStart);
	const xmp = head.slice(xmpStart, xmpEnd > 0 ? xmpEnd : undefined);
	const starts: number[] = [];

	// Motion Photo 1.0: a Container:Directory whose MotionPhoto item (the last
	// one) holds the clip; Length is its size.
	for (const item of xmp.match(/<Container:Item\b[^>]*>/g) ?? []) {
		if (!/Semantic="MotionPhoto"/.test(item)) continue;
		const length = Number(/Length="(\d+)"/.exec(item)?.[1]);
		if (length > 0) starts.push(bytes.length - length);
	}
	// MicroVideo (and Samsung's copy of it): the clip's offset from the end.
	const offset = Number(/MicroVideoOffset(?:="|>)\s*(\d+)/.exec(xmp)?.[1]);
	if (offset > 0) starts.push(bytes.length - offset);
	return starts.filter((start) => start > 0 && start < bytes.length);
}

export interface EmbeddedVideo {
	start: number;
	end: number;
}

/**
 * Finds the clip embedded in a motion photo, or null for a plain image.
 */
export function findEmbeddedVideo(bytes: Uint8Array): EmbeddedVideo | null {
	for (const start of xmpVideoStarts(bytes)) {
		const end = mp4End(bytes, start);
		if (end > start) return { start, end };
	}
	// No (usable) XMP: look for an MP4 after the image's own header. Vendor
	// trailers can follow it, so the file end is not a reliable anchor.
	for (let i = 12; i + 8 <= bytes.length; i++) {
		if (bytes[i] !== 0x66 || bytes[i + 1] !== 0x74 || bytes[i + 2] !== 0x79 || bytes[i + 3] !== 0x70) continue;
		const start = i - 4;
		const end = mp4End(bytes, start);
		if (end > start) return { start, end };
	}
	return null;
}

function isJpeg(bytes: Uint8Array): boolean {
	return bytes.length > 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff;
}

function isHeif(bytes: Uint8Array): boolean {
	return HEIF_BRANDS.has(majorBrand(bytes));
}

/** Whether `head` (the first bytes of a file) is an MP4/QuickTime video. */
export function looksLikeVideo(head: Uint8Array): boolean {
	if (head.length < 12) return false;
	const type = ascii(head, 4, 4);
	if (type === 'ftyp') {
		const brand = majorBrand(head);
		return !!brand && !IMAGE_BRANDS.has(brand);
	}
	return ['moov', 'mdat', 'wide', 'free', 'skip'].includes(type);
}

// ---------------------------------------------------------------------------
// HEIC conversion

const MAX_CANVAS_PIXELS = 16_000_000; // iOS Safari's canvas limit is ~16.7M

function canvasToJpeg(canvas: HTMLCanvasElement): Promise<Blob> {
	return new Promise((resolve, reject) => {
		canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('JPEG encoding failed'))), 'image/jpeg', 0.92);
	});
}

async function decodeHeicNatively(file: Blob): Promise<Blob | null> {
	if (typeof createImageBitmap !== 'function') return null;
	let bitmap: ImageBitmap;
	try {
		bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' });
	} catch {
		return null; // the browser cannot decode HEIC
	}
	try {
		const ratio = Math.min(1, Math.sqrt(MAX_CANVAS_PIXELS / (bitmap.width * bitmap.height)));
		const canvas = document.createElement('canvas');
		canvas.width = Math.max(1, Math.round(bitmap.width * ratio));
		canvas.height = Math.max(1, Math.round(bitmap.height * ratio));
		const context = canvas.getContext('2d');
		if (!context) return null;
		context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
		return await canvasToJpeg(canvas);
	} finally {
		bitmap.close();
	}
}

/** Converts a HEIC/HEIF image to JPEG. */
export async function heicToJpeg(file: File, data: Blob = file): Promise<File> {
	let jpeg = await decodeHeicNatively(data);
	if (!jpeg) {
		const { heicTo } = await import('heic-to');
		jpeg = await heicTo({ blob: data, type: 'image/jpeg', quality: 0.92 });
	}
	return new File([jpeg], withExtension(file.name, 'jpg'), { type: 'image/jpeg', lastModified: file.lastModified });
}

// ---------------------------------------------------------------------------
// Normalisation

export interface PreparedUpload {
	still: File;
	/** The live photo clip; absent for a plain image. */
	video?: File;
	/** Why a clip that came with the image was left out. */
	videoSkipped?: 'too-large' | 'invalid';
}

async function readHead(file: Blob, length: number): Promise<Uint8Array> {
	return new Uint8Array(await file.slice(0, length).arrayBuffer());
}

/**
 * Turns one upload source into what is sent to the server: a still every
 * browser can show (JPEG instead of HEIC, without an embedded clip) and the
 * live photo clip, if there is one.
 */
export async function prepareUpload(source: UploadSource): Promise<PreparedUpload> {
	let still = source.file;
	let stillData: Blob = still;
	let video: File | undefined;
	let videoSkipped: PreparedUpload['videoSkipped'];

	if (source.video) {
		if (source.video.size > LIVE_VIDEO_MAX_BYTES) videoSkipped = 'too-large';
		else if (!looksLikeVideo(await readHead(source.video, 16))) videoSkipped = 'invalid';
		else video = source.video;
	}

	const head = await readHead(still, 16);
	const heif = isHeif(head);
	if (!video && (isJpeg(head) || heif) && still.size <= LIVE_SOURCE_MAX_BYTES) {
		const bytes = new Uint8Array(await still.arrayBuffer());
		const found = findEmbeddedVideo(bytes);
		if (found) {
			const clip = bytes.subarray(found.start, found.end);
			if (clip.length <= LIVE_VIDEO_MAX_BYTES) {
				video = new File([clip], withExtension(still.name, 'mp4'), { type: 'video/mp4', lastModified: still.lastModified });
				// The still without the clip: a JPEG ends at its EOI, so the
				// bytes before the clip are the complete image.
				stillData = new Blob([bytes.subarray(0, found.start)], { type: still.type || (heif ? 'image/heic' : 'image/jpeg') });
				if (!heif) still = new File([stillData], still.name, { type: 'image/jpeg', lastModified: still.lastModified });
			} else {
				videoSkipped = 'too-large';
			}
		}
	}

	if (heif || isHeicFile(still)) {
		still = await heicToJpeg(still, stillData);
	} else if (!still.type && isJpeg(head)) {
		// Some pickers report no type; the upload check needs one.
		still = new File([still], still.name, { type: 'image/jpeg', lastModified: still.lastModified });
	}
	return { still, video, videoSkipped };
}
