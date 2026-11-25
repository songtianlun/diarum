import { Marked } from 'marked';
import DOMPurify from 'dompurify';
import { common, createLowlight } from 'lowlight';

const lowlight = createLowlight(common);

type HastNode = {
	type: string;
	value?: string;
	tagName?: string;
	properties?: { className?: string[] };
	children?: HastNode[];
};

function escapeHtml(text: string): string {
	return text
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;')
		.replace(/'/g, '&#39;');
}

function hastToHtml(nodes: HastNode[]): string {
	let out = '';
	for (const node of nodes) {
		if (node.type === 'text') {
			out += escapeHtml(node.value ?? '');
		} else if (node.type === 'element' && node.tagName) {
			const classes = node.properties?.className?.join(' ');
			const clsAttr = classes ? ` class="${escapeHtml(classes)}"` : '';
			out += `<${node.tagName}${clsAttr}>${hastToHtml(node.children ?? [])}</${node.tagName}>`;
		}
	}
	return out;
}

function highlightCode(code: string, lang?: string): { html: string; label: string } {
	const normalized = (lang || '').trim().toLowerCase().split(/\s+/)[0];
	try {
		if (normalized && lowlight.registered(normalized)) {
			const tree = lowlight.highlight(normalized, code);
			return { html: hastToHtml(tree.children as HastNode[]), label: normalized };
		}
		if (!normalized && code.length < 4000) {
			const tree = lowlight.highlightAuto(code);
			const detected = (tree.data as { language?: string } | undefined)?.language || 'text';
			return { html: hastToHtml(tree.children as HastNode[]), label: detected };
		}
	} catch {
		// Fall through to plain escaped code
	}
	return { html: escapeHtml(code), label: normalized || 'text' };
}

const chatMarked = new Marked({
	gfm: true,
	breaks: true,
	renderer: {
		code({ text, lang }) {
			const { html, label } = highlightCode(text, lang);
			const encoded = encodeURIComponent(text);
			return `<div class="chat-code-block"><div class="chat-code-header"><span class="chat-code-lang">${escapeHtml(label)}</span><button type="button" class="chat-code-copy" data-copy-code="${encoded}">Copy</button></div><pre><code class="hljs">${html}</code></pre></div>`;
		},
		table(token) {
			let header = '';
			for (const cell of token.header) {
				header += `<th${cell.align ? ` style="text-align:${cell.align}"` : ''}>${this.parser.parseInline(cell.tokens)}</th>`;
			}
			let body = '';
			for (const row of token.rows) {
				body += '<tr>';
				for (const cell of row) {
					body += `<td${cell.align ? ` style="text-align:${cell.align}"` : ''}>${this.parser.parseInline(cell.tokens)}</td>`;
				}
				body += '</tr>';
			}
			return `<div class="chat-table-wrap"><table><thead><tr>${header}</tr></thead><tbody>${body}</tbody></table></div>`;
		},
		link({ href, title, tokens }) {
			const text = this.parser.parseInline(tokens);
			const safeHref = href ? escapeHtml(href) : '';
			const titleAttr = title ? ` title="${escapeHtml(title)}"` : '';
			return `<a href="${safeHref}"${titleAttr} target="_blank" rel="noopener noreferrer">${text}</a>`;
		}
	}
});

/**
 * Splits markdown into stable top-level blocks so completed paragraphs/code blocks
 * don't need to re-parse on every streamed token.
 */
export function splitMarkdownBlocks(markdown: string): string[] {
	if (!markdown) return [];
	const lines = markdown.split('\n');
	const blocks: string[] = [];
	let current: string[] = [];
	let inFence = false;
	let fenceChar = '';

	for (const line of lines) {
		const fenceMatch = line.match(/^\s*(```+|~~~+)/);
		if (fenceMatch) {
			const marker = fenceMatch[1][0];
			if (!inFence) {
				inFence = true;
				fenceChar = marker;
			} else if (marker === fenceChar) {
				inFence = false;
				fenceChar = '';
			}
			current.push(line);
			continue;
		}

		if (!inFence && line.trim() === '' && current.length > 0) {
			// Keep consecutive list/blockquote/table lines grouped until blank line
			blocks.push(current.join('\n'));
			current = [];
			continue;
		}

		current.push(line);
	}

	if (current.length > 0) {
		blocks.push(current.join('\n'));
	}

	return blocks;
}

/**
 * Gracefully closes unfinished markdown tokens (fenced code, bold, inline code)
 * while the model is actively streaming so the layout does not flicker.
 */
export function closeIncompleteMarkdown(chunk: string): string {
	let result = chunk;
	const fenceMatches = result.match(/(^|\n)\s*```/g);
	if (fenceMatches && fenceMatches.length % 2 === 1) {
		result += '\n```';
		return result;
	}

	// Count unescaped inline backticks
	const inlineTicks = (result.match(/`/g) || []).length;
	if (inlineTicks % 2 === 1) {
		result += '`';
	}

	// Close trailing unclosed bold marker **
	const boldMarkers = (result.match(/\*\*/g) || []).length;
	if (boldMarkers % 2 === 1) {
		result += '**';
	}

	return result;
}

const DATE_PATTERN = /\b(20\d{2}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12]\d|3[01]))\b/g;

/**
 * Turns plain YYYY-MM-DD dates in rendered HTML text nodes (outside code/links/buttons)
 * into interactive diary citation pills.
 */
function linkifyDiaryDates(html: string): string {
	// Split on HTML tags so we only replace inside text segments.
	const parts = html.split(/(<[^>]+>)/g);
	let skipDepth = 0;

	return parts
		.map((part) => {
			if (part.startsWith('<')) {
				const lower = part.toLowerCase();
				if (/^<(code|pre|a|button)\b/.test(lower) && !lower.endsWith('/>')) {
					skipDepth++;
				} else if (/^<\/(code|pre|a|button)>/.test(lower)) {
					skipDepth = Math.max(0, skipDepth - 1);
				}
				return part;
			}
			if (skipDepth > 0 || !part.includes('-')) return part;
			return part.replace(
				DATE_PATTERN,
				'<button type="button" class="diary-date-chip" data-diary-date="$1">$1</button>'
			);
		})
		.join('');
}

const PURIFY_CONFIG = {
	ADD_TAGS: ['button'],
	ADD_ATTR: ['target', 'rel', 'data-diary-date', 'data-copy-code', 'type', 'style']
};

export function renderMarkdownBlock(block: string, isStreamingBlock = false): string {
	if (!block) return '';
	const input = isStreamingBlock ? closeIncompleteMarkdown(block) : block;
	const rawHtml = chatMarked.parse(input) as string;
	const withDateChips = linkifyDiaryDates(rawHtml);
	return DOMPurify.sanitize(withDateChips, PURIFY_CONFIG);
}

/**
 * Strips HTML tags and entities from TipTap diary HTML for clean previews.
 */
export function stripHtmlPreview(html: string, maxLength = 120): string {
	if (!html) return '';
	const text = html
		.replace(/<\/(p|div|li|h[1-6])>/gi, ' ')
		.replace(/<br\s*\/?>/gi, ' ')
		.replace(/<[^>]*>/g, '')
		.replace(/&nbsp;/gi, ' ')
		.replace(/&amp;/gi, '&')
		.replace(/&lt;/gi, '<')
		.replace(/&gt;/gi, '>')
		.replace(/&quot;/gi, '"')
		.replace(/&#39;/gi, "'")
		.replace(/\s+/g, ' ')
		.trim();
	if (text.length <= maxLength) return text;
	return text.slice(0, maxLength).trimEnd() + '…';
}

export function sanitizeDiaryHtml(html: string): string {
	if (!html) return '';
	return DOMPurify.sanitize(html, {
		ADD_ATTR: ['target', 'rel']
	});
}
