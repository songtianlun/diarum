import type { Locale } from '$lib/i18n';

/** The Diarum website: product docs, feature guides, releases and blog. */
export const DOCS_URL = 'https://docs.diarum.app';

/**
 * Link to a page on the docs site in the reader's language, e.g.
 * docsUrl('zh', '/features/mcp/') -> https://docs.diarum.app/zh/features/mcp/
 */
export function docsUrl(locale: Locale, path = '/'): string {
	const lang = locale === 'zh' ? 'zh' : 'en';
	return `${DOCS_URL}/${lang}${path.startsWith('/') ? path : `/${path}`}`;
}
