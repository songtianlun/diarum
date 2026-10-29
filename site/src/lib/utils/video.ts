/**
 * Video uploads. Videos are stored as uploaded and played by the browser
 * straight from the file (the server streams them with range requests), so
 * only containers browsers play are accepted. On upload the browser grabs a
 * frame as the poster, which libraries and entries show before playing.
 */

export const VIDEO_UPLOAD_LIMITS = {
	maxSize: 200 * 1024 * 1024, // must match the backend video limit
	allowedTypes: ['video/mp4', 'video/x-m4v', 'video/quicktime', 'video/webm', 'video/ogg']
};

const VIDEO_EXTENSIONS = /\.(mp4|m4v|mov|webm|ogv)$/i;

/** File picker `accept` entries for videos. */
export const VIDEO_ACCEPT = 'video/mp4,video/x-m4v,video/quicktime,video/webm,video/ogg,.mp4,.m4v,.mov,.webm,.ogv';

/** Addresses of videos Diarum stores or entries link to, by extension. */
const VIDEO_URL = /\.(mp4|m4v|mov|webm|ogv)(?:[?#]|$)/i;

export function isVideoUrl(src: string | null | undefined): boolean {
	return !!src && VIDEO_URL.test(src);
}

/** Returns a user-facing reason the video cannot be uploaded, or null. */
export function validateVideoFile(file: File): string | null {
	if (!VIDEO_UPLOAD_LIMITS.allowedTypes.includes(file.type) && !VIDEO_EXTENSIONS.test(file.name)) {
		return 'Unsupported video format. Use MP4, M4V, MOV, WebM or OGV';
	}
	if (file.size > VIDEO_UPLOAD_LIMITS.maxSize) {
		return `Video larger than ${VIDEO_UPLOAD_LIMITS.maxSize / 1024 / 1024}MB`;
	}
	return null;
}

export interface VideoProbe {
	/** A frame of the video as a JPEG; absent when it could not be decoded. */
	poster?: File;
	/** Whether this browser could decode the video at all. */
	playable: boolean;
}

const POSTER_MAX_EDGE = 1280;
const PROBE_TIMEOUT_MS = 10000;

function stem(name: string): string {
	const dot = name.lastIndexOf('.');
	return dot > 0 ? name.slice(0, dot) : name || 'video';
}

/**
 * Loads the video in a detached element and captures a frame a little in
 * (the very first one is often black). Never rejects: a video this browser
 * cannot decode (e.g. HEVC outside Safari) is reported as not playable.
 */
export function probeVideo(file: File): Promise<VideoProbe> {
	return new Promise((resolve) => {
		const url = URL.createObjectURL(file);
		const video = document.createElement('video');
		let settled = false;
		const finish = (result: VideoProbe) => {
			if (settled) return;
			settled = true;
			clearTimeout(timer);
			video.removeAttribute('src');
			video.load();
			URL.revokeObjectURL(url);
			resolve(result);
		};
		const timer = setTimeout(() => finish({ playable: video.readyState >= 1 }), PROBE_TIMEOUT_MS);

		video.muted = true;
		video.playsInline = true;
		video.preload = 'auto';
		video.addEventListener('error', () => finish({ playable: false }));
		video.addEventListener('loadedmetadata', () => {
			const duration = Number.isFinite(video.duration) ? video.duration : 0;
			const at = Math.min(1, duration * 0.1);
			if (at > 0) video.currentTime = at;
			else video.addEventListener('loadeddata', capture, { once: true });
		});
		video.addEventListener('seeked', capture, { once: true });

		function capture() {
			const width = video.videoWidth;
			const height = video.videoHeight;
			// Audio only, or a codec without picture support here.
			if (!width || !height) {
				finish({ playable: video.readyState >= 1 });
				return;
			}
			const scale = Math.min(1, POSTER_MAX_EDGE / Math.max(width, height));
			const canvas = document.createElement('canvas');
			canvas.width = Math.round(width * scale);
			canvas.height = Math.round(height * scale);
			const context = canvas.getContext('2d');
			if (!context) {
				finish({ playable: true });
				return;
			}
			try {
				context.drawImage(video, 0, 0, canvas.width, canvas.height);
			} catch {
				finish({ playable: true });
				return;
			}
			canvas.toBlob(
				(blob) => {
					const poster = blob ? new File([blob], `${stem(file.name)}.poster.jpg`, { type: 'image/jpeg' }) : undefined;
					finish({ poster, playable: true });
				},
				'image/jpeg',
				0.82
			);
		}

		video.src = url;
	});
}
