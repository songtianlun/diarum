/**
 * Copies text to the clipboard. Falls back to a hidden textarea and
 * execCommand where the async Clipboard API is unavailable, which includes
 * self-hosted instances served over plain HTTP.
 */
export async function copyText(text: string): Promise<boolean> {
	try {
		if (navigator.clipboard && window.isSecureContext) {
			await navigator.clipboard.writeText(text);
			return true;
		}
	} catch {
		// Permission denied or unsupported: try the fallback.
	}
	const textarea = document.createElement('textarea');
	textarea.value = text;
	textarea.setAttribute('readonly', '');
	textarea.style.position = 'fixed';
	textarea.style.top = '0';
	textarea.style.left = '0';
	textarea.style.opacity = '0';
	document.body.appendChild(textarea);
	const selection = document.getSelection();
	const previousRange = selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : null;
	textarea.select();
	textarea.setSelectionRange(0, text.length); // iOS needs an explicit range
	let ok = false;
	try {
		ok = document.execCommand('copy');
	} catch {
		ok = false;
	}
	textarea.remove();
	if (previousRange && selection) {
		selection.removeAllRanges();
		selection.addRange(previousRange);
	}
	return ok;
}
