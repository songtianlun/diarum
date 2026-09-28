/**
 * Converts the HTML the diary editor produces (TipTap StarterKit plus task
 * lists, highlights, links and images) into GitHub-flavoured Markdown.
 * Formatting Markdown cannot express (underline, highlight) degrades to plain
 * text rather than leaking HTML tags.
 */

export interface MarkdownOptions {
	/** Base for relative image and link URLs, so they work outside the app. */
	baseUrl?: string;
	includeImages?: boolean;
}

const BLOCK_TAGS = new Set([
	'P', 'DIV', 'H1', 'H2', 'H3', 'H4', 'H5', 'H6', 'UL', 'OL', 'LI', 'BLOCKQUOTE', 'PRE', 'HR', 'IMG', 'TABLE', 'FIGURE'
]);

/** Resolves relative URLs against base; absolute ones are kept verbatim. */
function absolute(url: string, base?: string): string {
	if (!base || !url || /^[a-z][a-z0-9+.-]*:/i.test(url)) return url;
	try {
		return new URL(url, base).toString();
	} catch {
		return url;
	}
}

/**
 * Escapes characters that would otherwise start Markdown formatting, and no
 * more: shared text should stay readable. Underscores inside words (snake_case)
 * never format in GFM, so only those at word edges are escaped.
 */
function escapeText(text: string): string {
	return text
		.replace(/([\\`*[\]<])/g, '\\$1')
		.replace(/(^|[^\p{L}\p{N}])_|_(?=[^\p{L}\p{N}]|$)/gu, (match) => match.replace('_', '\\_'));
}

/** Escapes what only matters at the start of a line (headings, lists, quotes). */
function escapeLineStart(line: string): string {
	return line
		.replace(/^(\s*)([#>+-])(?=\s|$)/, '$1\\$2')
		.replace(/^(\s*)(\d+)([.)])(?=\s)/, '$1$2\\$3');
}

function inlineCode(text: string): string {
	const longest = Math.max(0, ...(text.match(/`+/g) ?? []).map((run) => run.length));
	const fence = '`'.repeat(longest + 1);
	const pad = text.startsWith('`') || text.endsWith('`') ? ' ' : '';
	return `${fence}${pad}${text}${pad}${fence}`;
}

/** Wraps inline content in a marker, keeping edge whitespace outside it (`** a **` is not bold). */
function wrap(content: string, marker: string): string {
	const match = content.match(/^(\s*)([\s\S]*?)(\s*)$/)!;
	if (!match[2]) return content;
	return `${match[1]}${marker}${match[2]}${marker}${match[3]}`;
}

class Converter {
	// Bullet used by the previous top-level unordered list: consecutive lists
	// must alternate, or Markdown renderers merge them into one.
	private lastBullet: '-' | '*' | null = null;

	constructor(private options: MarkdownOptions) {}

	inline(node: Node): string {
		if (node.nodeType === Node.TEXT_NODE) {
			return escapeText((node.textContent ?? '').replace(/\s+/g, ' '));
		}
		if (!(node instanceof HTMLElement)) return '';
		const children = () => Array.from(node.childNodes).map((child) => this.inline(child)).join('');

		switch (node.tagName) {
			case 'STRONG':
			case 'B':
				return wrap(children(), '**');
			case 'EM':
			case 'I':
				return wrap(children(), '*');
			case 'S':
			case 'DEL':
			case 'STRIKE':
				return wrap(children(), '~~');
			case 'CODE':
				return inlineCode(node.textContent ?? '');
			case 'BR':
				return '\\\n';
			case 'A': {
				const href = absolute(node.getAttribute('href') ?? '', this.options.baseUrl);
				const text = children().trim();
				if (!href) return text;
				if (!text || text === escapeText(href)) return `<${href}>`;
				return `[${text}](${href.replace(/\)/g, '%29').replace(/ /g, '%20')})`;
			}
			case 'IMG':
				return this.image(node as HTMLImageElement);
			case 'INPUT':
				return '';
			default:
				// u, mark, span, label… keep the text only.
				return children();
		}
	}

	image(img: HTMLImageElement): string {
		if (this.options.includeImages === false) return '';
		const src = img.dataset.fullSrc || img.getAttribute('src') || '';
		if (!src || src.startsWith('blob:') || img.dataset.uploading === 'true') return '';
		const alt = (img.getAttribute('alt') ?? '').replace(/[[\]]/g, '');
		return `![${alt}](${absolute(src, this.options.baseUrl).replace(/\)/g, '%29').replace(/ /g, '%20')})`;
	}

	/** Inline content of a block, one Markdown line per source line. */
	inlineBlock(el: Element): string {
		const text = Array.from(el.childNodes).map((child) => this.inline(child)).join('');
		return text
			.split('\n')
			.map((line) => escapeLineStart(line.trim()))
			.join('\n')
			.trim();
	}

	/** Converts a sequence of nodes to Markdown blocks separated by blank lines. */
	blocks(nodes: NodeListOf<ChildNode> | ChildNode[]): string {
		const out: string[] = [];
		let inlineRun: Node[] = [];
		const flushInline = () => {
			if (inlineRun.length === 0) return;
			const holder = document.createElement('p');
			inlineRun.forEach((n) => holder.appendChild(n.cloneNode(true)));
			const text = this.inlineBlock(holder);
			if (text) out.push(text);
			inlineRun = [];
		};
		for (const node of Array.from(nodes)) {
			if (node instanceof HTMLElement && BLOCK_TAGS.has(node.tagName)) {
				flushInline();
				const previousBullet = this.lastBullet;
				const block = this.block(node);
				if (node.tagName !== 'UL' || this.lastBullet === previousBullet) this.lastBullet = null;
				if (block) out.push(block);
			} else {
				inlineRun.push(node);
			}
		}
		flushInline();
		return out.join('\n\n');
	}

	block(el: HTMLElement): string {
		switch (el.tagName) {
			case 'H1':
			case 'H2':
			case 'H3':
			case 'H4':
			case 'H5':
			case 'H6': {
				const text = this.inlineBlock(el).replace(/\\\n/g, ' ');
				return text ? `${'#'.repeat(Number(el.tagName[1]))} ${text}` : '';
			}
			case 'P':
				return this.inlineBlock(el);
			case 'HR':
				return '---';
			case 'IMG':
				return this.image(el as HTMLImageElement);
			case 'BLOCKQUOTE': {
				const inner = this.blocks(el.childNodes);
				return inner ? inner.split('\n').map((line) => (line ? `> ${line}` : '>')).join('\n') : '';
			}
			case 'PRE': {
				const code = el.querySelector('code');
				const text = (code ?? el).textContent ?? '';
				const lang = (code?.className.match(/language-([\w+#-]+)/) ?? [])[1] ?? '';
				const longest = Math.max(2, ...(text.match(/`{3,}/g) ?? []).map((run) => run.length));
				const fence = '`'.repeat(longest + 1);
				return `${fence}${lang}\n${text.replace(/\n$/, '')}\n${fence}`;
			}
			case 'UL':
			case 'OL':
				return this.list(el);
			case 'LI':
				return this.blocks(el.childNodes);
			default:
				return this.blocks(el.childNodes);
		}
	}

	list(el: HTMLElement): string {
		const ordered = el.tagName === 'OL';
		const bullet = !ordered && this.lastBullet === '-' ? '*' : '-';
		const isTaskList = el.dataset.type === 'taskList';
		let index = Number(el.getAttribute('start') ?? '1') || 1;
		const items: string[] = [];
		for (const li of Array.from(el.children)) {
			if (li.tagName !== 'LI') continue;
			let marker = ordered ? `${index++}.` : bullet;
			if (isTaskList || (li as HTMLElement).dataset.type === 'taskItem') {
				const checked = (li as HTMLElement).dataset.checked === 'true' || !!li.querySelector(':scope > label input:checked');
				marker = `${bullet} [${checked ? 'x' : ' '}]`;
			}
			// Task items wrap their text in a <div>; skip the checkbox label.
			const contentNodes = Array.from(li.childNodes).filter((n) => !(n instanceof HTMLElement && n.tagName === 'LABEL'));
			// Nested lists start their own bullet sequence.
			const outer = this.lastBullet;
			this.lastBullet = null;
			const body = this.blocks(contentNodes.flatMap((n) => (n instanceof HTMLElement && n.tagName === 'DIV' ? Array.from(n.childNodes) : [n])));
			this.lastBullet = outer;
			const indent = ' '.repeat(ordered && !isTaskList ? marker.length + 1 : 2);
			// Items stay tight: blank lines only between a paragraph and a nested list.
			const lines = body.replace(/\n\n(?=\s*(?:[-*+]|\d+\.) )/g, '\n').split('\n');
			items.push(lines.map((line, i) => (i === 0 ? `${marker} ${line}` : line ? `${indent}${line}` : '')).join('\n'));
		}
		if (!ordered) this.lastBullet = bullet;
		return items.join('\n');
	}
}

export function htmlToMarkdown(html: string, options: MarkdownOptions = {}): string {
	if (!html?.trim()) return '';
	const doc = new DOMParser().parseFromString(html, 'text/html');
	return new Converter(options)
		.blocks(doc.body.childNodes)
		.replace(/\n{3,}/g, '\n\n')
		.trim();
}
