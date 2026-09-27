<script lang="ts">
	/**
	 * Settings → Audit log. Shows the signed-in user's own trail of key
	 * operations (edits, deletions, reads, sign-ins, setting changes): the
	 * latest entries by default, a full local day on request, and a search
	 * across everything still retained.
	 */
	import { onDestroy, onMount } from 'svelte';
	import {
		getAuditSettings,
		saveAuditSettings,
		listAuditFiles,
		queryAuditEntries,
		downloadAuditFile,
		sanitizeAuditRetention,
		DEFAULT_AUDIT_RETENTION,
		MIN_AUDIT_RETENTION,
		MAX_AUDIT_RETENTION,
		type AuditEntry,
		type AuditFile
	} from '$lib/api/audit';
	import { formatDate, parseDate } from '$lib/utils/date';
	import { getIntlLocale, locale, t } from '$lib/i18n';

	type Mode = 'recent' | 'day';

	const RECENT_LIMIT = 100;
	const DAY_LIMIT = 5000;
	const SEARCH_LIMIT = 500;

	const FILTERS: { id: string; key: string }[] = [
		{ id: '', key: 'settings.audit.filters.all' },
		{ id: 'diary.create|diary.update|diary.delete|diary.restore', key: 'settings.audit.filters.edits' },
		{ id: 'diary.view|diary.search', key: 'settings.audit.filters.reads' },
		{ id: 'media', key: 'settings.audit.filters.media' },
		{ id: 'data', key: 'settings.audit.filters.data' },
		{ id: 'settings|token|conversation', key: 'settings.audit.filters.settings' },
		{ id: 'auth', key: 'settings.audit.filters.auth' }
	];

	// Retention
	let retention = DEFAULT_AUDIT_RETENTION;
	let savedRetention = DEFAULT_AUDIT_RETENTION;
	let retentionSaving = false;
	let retentionMessage = '';
	let retentionError = '';

	// Listing
	let mode: Mode = 'recent';
	let files: AuditFile[] = [];
	let selectedDay = formatDate(new Date());
	let query = '';
	let filter = '';
	let entries: AuditEntry[] = [];
	let truncated = false;
	let loading = true;
	let error = '';
	let expanded = new Set<string>();
	let downloading = false;
	let loadToken = 0;
	let searchTimer: ReturnType<typeof setTimeout> | undefined;

	$: fileByDate = new Map(files.map((f) => [f.date, f]));
	$: totalEntries = files.reduce((sum, f) => sum + f.entries, 0);
	$: selectedFile = fileByDate.get(selectedDay);
	$: today = formatDate(new Date());
	$: grouped = groupByDay(entries, $locale);
	$: searching = query.trim() !== '';

	onMount(() => {
		void init();
	});

	onDestroy(() => clearTimeout(searchTimer));

	async function init() {
		try {
			const settings = await getAuditSettings();
			retention = savedRetention = settings.retention_days;
		} catch {
			// Keep the default; the list below reports real failures.
		}
		await Promise.all([loadFiles(), load()]);
	}

	async function loadFiles() {
		try {
			files = await listAuditFiles();
		} catch {
			files = [];
		}
	}

	async function load() {
		const token = ++loadToken;
		loading = true;
		error = '';
		try {
			const actions = filter ? filter.split('|') : [''];
			const base = {
				q: query,
				...(mode === 'day' ? dayRange(selectedDay) : {}),
				limit: searching ? SEARCH_LIMIT : mode === 'day' ? DAY_LIMIT : RECENT_LIMIT
			};
			// A filter may span several action families; ask for each and merge.
			const results = await Promise.all(actions.map((action) => queryAuditEntries({ ...base, action })));
			if (token !== loadToken) return;
			const merged = results.flatMap((r) => r.entries);
			merged.sort((a, b) => b.time.localeCompare(a.time));
			entries = merged.slice(0, base.limit);
			truncated = results.some((r) => r.truncated) || merged.length > base.limit;
			expanded = new Set();
		} catch (e) {
			if (token !== loadToken) return;
			error = e instanceof Error ? e.message : $t('settings.audit.loadFailed');
			entries = [];
		} finally {
			if (token === loadToken) loading = false;
		}
	}

	function refresh() {
		void loadFiles();
		void load();
	}

	function dayRange(day: string) {
		const start = parseDate(day);
		const end = new Date(start);
		end.setDate(end.getDate() + 1);
		end.setMilliseconds(end.getMilliseconds() - 1);
		return { start, end };
	}

	function setMode(next: Mode) {
		if (mode === next) return;
		mode = next;
		void load();
	}

	function selectDay(day: string) {
		if (!day) return;
		selectedDay = day > today ? today : day;
		mode = 'day';
		void load();
	}

	function shiftDay(delta: number) {
		const date = parseDate(selectedDay);
		date.setDate(date.getDate() + delta);
		selectDay(formatDate(date));
	}

	function onQueryInput() {
		clearTimeout(searchTimer);
		searchTimer = setTimeout(() => void load(), 300);
	}

	function clearQuery() {
		query = '';
		clearTimeout(searchTimer);
		void load();
	}

	function setFilter(id: string) {
		filter = id;
		void load();
	}

	async function saveRetention() {
		retentionError = '';
		retentionMessage = '';
		retention = sanitizeAuditRetention(retention);
		retentionSaving = true;
		try {
			const settings = await saveAuditSettings(retention);
			retention = savedRetention = settings.retention_days;
			retentionMessage = $t('settings.audit.retentionSaved');
			setTimeout(() => (retentionMessage = ''), 3000);
			refresh();
		} catch (e) {
			retentionError = e instanceof Error ? e.message : $t('settings.audit.retentionFailed');
		}
		retentionSaving = false;
	}

	async function download() {
		if (!selectedFile) return;
		downloading = true;
		try {
			await downloadAuditFile(selectedFile.date);
		} catch (e) {
			error = e instanceof Error ? e.message : $t('settings.audit.loadFailed');
		}
		downloading = false;
	}

	function toggle(key: string) {
		const next = new Set(expanded);
		if (next.has(key)) next.delete(key);
		else next.add(key);
		expanded = next;
	}

	function entryKey(entry: AuditEntry, index: number) {
		return `${entry.time}-${entry.action}-${entry.target ?? ''}-${index}`;
	}

	// ----- Presentation helpers -----

	interface DayGroup {
		day: string;
		label: string;
		items: { entry: AuditEntry; key: string }[];
	}

	function groupByDay(list: AuditEntry[], _locale: string): DayGroup[] {
		const groups: DayGroup[] = [];
		list.forEach((entry, index) => {
			const day = formatDate(new Date(entry.time));
			let group = groups[groups.length - 1];
			if (!group || group.day !== day) {
				group = { day, label: dayLabel(day), items: [] };
				groups.push(group);
			}
			group.items.push({ entry, key: entryKey(entry, index) });
		});
		return groups;
	}

	function dayLabel(day: string): string {
		const now = formatDate(new Date());
		const yesterday = new Date();
		yesterday.setDate(yesterday.getDate() - 1);
		const date = parseDate(day);
		const formatted = date.toLocaleDateString(getIntlLocale(), {
			month: 'long',
			day: 'numeric',
			weekday: 'short',
			year: date.getFullYear() === new Date().getFullYear() ? undefined : 'numeric'
		});
		if (day === now) return `${$t('settings.audit.today')} · ${formatted}`;
		if (day === formatDate(yesterday)) return `${$t('settings.audit.yesterday')} · ${formatted}`;
		return formatted;
	}

	function chipLabel(day: string): string {
		return parseDate(day).toLocaleDateString(getIntlLocale(), { month: 'numeric', day: 'numeric' });
	}

	function timeOf(entry: AuditEntry): string {
		return new Date(entry.time).toLocaleTimeString(getIntlLocale(), {
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit'
		});
	}

	function isDate(value: string | undefined): value is string {
		return !!value && /^\d{4}-\d{2}-\d{2}$/.test(value);
	}

	const KNOWN_ACTIONS = new Set([
		'diary.create', 'diary.update', 'diary.delete', 'diary.restore', 'diary.view', 'diary.search',
		'media.upload', 'media.delete', 'data.import', 'data.export', 'settings.update', 'token.update',
		'conversation.delete', 'auth.login', 'auth.login_failed', 'auth.register'
	]);

	function actionLabel(action: string): string {
		return KNOWN_ACTIONS.has(action) ? $t(`settings.audit.actions.${action.replace('.', '_')}`) : action;
	}

	/** Human sentence for the entry, e.g. "Updated the entry for Sep 28". */
	function describe(entry: AuditEntry): string {
		const target = entry.target ?? '';
		const detail = entry.detail ?? {};
		const dates = Array.isArray(detail.dates) ? (detail.dates as string[]) : [];
		switch (entry.action) {
			case 'diary.view':
				if (target === 'recent') return $t('settings.audit.describe.viewRecent', { count: dates.length });
				if (target === 'on-this-day') return $t('settings.audit.describe.viewOnThisDay', { count: dates.length });
				if (detail.revision) return $t('settings.audit.describe.viewRevision', { date: target });
				if (isDate(target)) return $t('settings.audit.describe.viewDiary', { date: target });
				return $t('settings.audit.describe.viewRange', { range: target, count: dates.length });
			case 'diary.search':
				return $t('settings.audit.describe.search', { query: String(detail.query ?? ''), count: Number(detail.results ?? 0) });
			case 'diary.create':
			case 'diary.update':
			case 'diary.delete':
			case 'diary.restore':
				return $t(`settings.audit.describe.${entry.action.split('.')[1]}`, { date: target || '—' });
			case 'media.upload':
			case 'media.delete':
				return $t(`settings.audit.describe.media_${entry.action.split('.')[1]}`, { name: target || '—' });
			case 'settings.update':
			case 'token.update':
				return $t('settings.audit.describe.settings', { key: target || '—' });
			default:
				return target ? `${actionLabel(entry.action)} · ${target}` : actionLabel(entry.action);
		}
	}

	/** Small chips with the most useful facts from the detail. */
	function facts(entry: AuditEntry): string[] {
		const d = entry.detail ?? {};
		const out: string[] = [];
		if (typeof d.words === 'number') {
			if (typeof d.words_before === 'number' && d.words_before !== d.words) {
				const delta = d.words - d.words_before;
				out.push($t('settings.audit.facts.wordsChange', { from: d.words_before, to: d.words, delta: delta > 0 ? `+${delta}` : String(delta) }));
			} else {
				out.push($t('settings.audit.facts.words', { count: d.words }));
			}
		}
		if (typeof d.saves === 'number' && d.saves > 1) out.push($t('settings.audit.facts.saves', { count: d.saves }));
		if (d.mood_before !== undefined || (d.mood && entry.action === 'diary.create')) {
			out.push(`${$t('settings.audit.facts.mood')} ${d.mood_before ? `${d.mood_before} → ` : ''}${d.mood || '∅'}`);
		}
		if (d.weather_before !== undefined) {
			out.push(`${$t('settings.audit.facts.weather')} ${d.weather_before || '∅'} → ${d.weather || '∅'}`);
		}
		if (typeof d.tool === 'string') out.push(`MCP · ${d.tool}`);
		if (typeof d.memo === 'string') out.push(`Memo ${d.memo}`);
		if (typeof d.from === 'number' && typeof d.to === 'number') out.push(`${d.from} → ${d.to}`);
		if (typeof d.enabled === 'boolean') out.push(d.enabled ? $t('settings.audit.facts.on') : $t('settings.audit.facts.off'));
		if (typeof d.op === 'string') out.push(d.op);
		return out;
	}

	function sourceLabel(source?: string): string {
		switch (source) {
			case 'web': return $t('settings.audit.sources.web');
			case 'mcp': return 'MCP';
			case 'memos': return 'Memos';
			case 'system': return $t('settings.audit.sources.system');
			default: return source || '';
		}
	}

	type Tone = 'create' | 'update' | 'delete' | 'restore' | 'read' | 'media' | 'data' | 'settings' | 'auth' | 'danger';

	function toneOf(action: string): Tone {
		switch (action) {
			case 'diary.create': return 'create';
			case 'diary.update': return 'update';
			case 'diary.delete':
			case 'media.delete':
			case 'conversation.delete': return 'delete';
			case 'diary.restore': return 'restore';
			case 'diary.view':
			case 'diary.search': return 'read';
			case 'media.upload': return 'media';
			case 'data.import':
			case 'data.export': return 'data';
			case 'auth.login':
			case 'auth.register': return 'auth';
			case 'auth.login_failed': return 'danger';
			default: return 'settings';
		}
	}

	const TONE_CLASS: Record<Tone, string> = {
		create: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 ring-emerald-500/20',
		update: 'bg-sky-500/10 text-sky-600 dark:text-sky-400 ring-sky-500/20',
		delete: 'bg-rose-500/10 text-rose-600 dark:text-rose-400 ring-rose-500/20',
		restore: 'bg-amber-500/10 text-amber-600 dark:text-amber-400 ring-amber-500/20',
		read: 'bg-violet-500/10 text-violet-600 dark:text-violet-400 ring-violet-500/20',
		media: 'bg-teal-500/10 text-teal-600 dark:text-teal-400 ring-teal-500/20',
		data: 'bg-indigo-500/10 text-indigo-600 dark:text-indigo-400 ring-indigo-500/20',
		settings: 'bg-slate-500/10 text-slate-600 dark:text-slate-300 ring-slate-500/20',
		auth: 'bg-orange-500/10 text-orange-600 dark:text-orange-400 ring-orange-500/20',
		danger: 'bg-red-500/15 text-red-600 dark:text-red-400 ring-red-500/30'
	};

	const TONE_ICON: Record<Tone, string> = {
		create: 'M12 4v16m8-8H4',
		update: 'M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z',
		delete: 'M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16',
		restore: 'M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6',
		read: 'M15 12a3 3 0 11-6 0 3 3 0 016 0z M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z',
		media: 'M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z',
		data: 'M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4',
		settings: 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z',
		auth: 'M11 16l-4-4m0 0l4-4m-4 4h14m-5 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h7a3 3 0 013 3v1',
		danger: 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z'
	};

	/** Split text around case-insensitive matches of the search query. */
	function highlight(text: string, q: string): { text: string; hit: boolean }[] {
		const needle = q.trim().toLowerCase();
		if (!needle) return [{ text, hit: false }];
		const parts: { text: string; hit: boolean }[] = [];
		const lower = text.toLowerCase();
		let from = 0;
		for (let at = lower.indexOf(needle); at !== -1; at = lower.indexOf(needle, from)) {
			if (at > from) parts.push({ text: text.slice(from, at), hit: false });
			parts.push({ text: text.slice(at, at + needle.length), hit: true });
			from = at + needle.length;
		}
		if (from < text.length) parts.push({ text: text.slice(from), hit: false });
		return parts;
	}

	function formatBytes(size: number): string {
		if (size < 1024) return `${size} B`;
		if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
		return `${(size / 1024 / 1024).toFixed(1)} MB`;
	}

	function shortUA(ua?: string): string {
		if (!ua) return '';
		const browser = /Edg\//.test(ua) ? 'Edge' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : '';
		const os = /iPhone|iPad/.test(ua) ? 'iOS' : /Android/.test(ua) ? 'Android' : /Mac OS X/.test(ua) ? 'macOS' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : '';
		const label = [browser, os].filter(Boolean).join(' · ');
		return label || ua.slice(0, 40);
	}
</script>

<div id="audit" class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6 animate-fade-in scroll-mt-16">
	<!-- Header -->
	<div class="flex items-start justify-between gap-3 mb-5">
		<div class="min-w-0">
			<h2 class="text-lg font-semibold text-foreground">{$t('settings.audit.heading')}</h2>
			<p class="text-sm text-muted-foreground mt-1">{$t('settings.audit.description')}</p>
		</div>
		<button
			on:click={refresh}
			disabled={loading}
			title={$t('settings.audit.refresh')}
			aria-label={$t('settings.audit.refresh')}
			class="flex-shrink-0 p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50"
		>
			<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
			</svg>
		</button>
	</div>

	<!-- Overview + retention -->
	<div class="grid grid-cols-2 gap-3 sm:grid-cols-[1fr_1fr_1.4fr] mb-6">
		<div class="rounded-lg bg-muted/40 px-4 py-3">
			<div class="text-xs text-muted-foreground">{$t('settings.audit.statDays')}</div>
			<div class="text-xl font-semibold text-foreground tabular-nums mt-0.5">{files.length}</div>
		</div>
		<div class="rounded-lg bg-muted/40 px-4 py-3">
			<div class="text-xs text-muted-foreground">{$t('settings.audit.statEntries')}</div>
			<div class="text-xl font-semibold text-foreground tabular-nums mt-0.5">{totalEntries.toLocaleString(getIntlLocale())}</div>
		</div>
		<div class="col-span-2 sm:col-span-1 rounded-lg bg-muted/40 px-4 py-3">
			<label for="audit-retention" class="text-xs text-muted-foreground block">{$t('settings.audit.retention')}</label>
			<div class="flex items-center gap-2 mt-1">
				<input
					id="audit-retention"
					type="number"
					min={MIN_AUDIT_RETENTION}
					max={MAX_AUDIT_RETENTION}
					step="1"
					bind:value={retention}
					on:blur={() => (retention = sanitizeAuditRetention(retention))}
					on:keydown={(e) => e.key === 'Enter' && retention !== savedRetention && saveRetention()}
					class="w-20 px-2 py-1 bg-background text-foreground border border-border rounded-md text-sm tabular-nums focus:outline-none focus:ring-2 focus:ring-primary/40"
				/>
				<span class="text-sm text-muted-foreground">{$t('settings.audit.days')}</span>
				<button
					on:click={saveRetention}
					disabled={retentionSaving || retention === savedRetention}
					class="ml-auto px-3 py-1 text-sm bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
				>
					{retentionSaving ? $t('common.saving') : $t('settings.audit.save')}
				</button>
			</div>
		</div>
	</div>
	<p class="-mt-4 mb-5 text-xs text-muted-foreground">
		{$t('settings.audit.retentionHint', { min: MIN_AUDIT_RETENTION, max: MAX_AUDIT_RETENTION, default: DEFAULT_AUDIT_RETENTION })}
		{#if retentionMessage}<span class="ml-1 text-emerald-600 dark:text-emerald-400">{retentionMessage}</span>{/if}
		{#if retentionError}<span class="ml-1 text-rose-600 dark:text-rose-400">{retentionError}</span>{/if}
	</p>

	<!-- Toolbar -->
	<div class="space-y-3 mb-4">
		<div class="flex flex-wrap items-center gap-2">
			<div class="inline-flex p-0.5 rounded-lg bg-muted/60" role="tablist">
				<button
					role="tab"
					aria-selected={mode === 'recent'}
					on:click={() => setMode('recent')}
					class="px-3 py-1.5 text-sm rounded-md transition-all {mode === 'recent' ? 'bg-card text-foreground shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
				>{$t('settings.audit.modeRecent')}</button>
				<button
					role="tab"
					aria-selected={mode === 'day'}
					on:click={() => setMode('day')}
					class="px-3 py-1.5 text-sm rounded-md transition-all {mode === 'day' ? 'bg-card text-foreground shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
				>{$t('settings.audit.modeDay')}</button>
			</div>

			{#if mode === 'day'}
				<div class="flex items-center gap-1">
					<button on:click={() => shiftDay(-1)} class="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/60" aria-label={$t('settings.audit.prevDay')}>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" /></svg>
					</button>
					<input
						type="date"
						value={selectedDay}
						max={today}
						on:change={(e) => selectDay(e.currentTarget.value)}
						class="px-2 py-1 bg-background text-foreground border border-border rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-primary/40"
						aria-label={$t('settings.audit.pickDay')}
					/>
					<button on:click={() => shiftDay(1)} disabled={selectedDay >= today} class="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/60 disabled:opacity-30" aria-label={$t('settings.audit.nextDay')}>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" /></svg>
					</button>
				</div>
				<button
					on:click={download}
					disabled={!selectedFile || downloading}
					title={$t('settings.audit.downloadHint')}
					class="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-sm rounded-md border border-border/60 text-foreground hover:bg-muted/50 disabled:opacity-40 disabled:cursor-not-allowed"
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" /></svg>
					{$t('settings.audit.download')}
					{#if selectedFile}<span class="text-xs text-muted-foreground">{formatBytes(selectedFile.size)}</span>{/if}
				</button>
			{/if}
		</div>

		{#if mode === 'day' && files.length > 0}
			<div class="flex gap-1.5 overflow-x-auto pb-1 -mx-1 px-1" aria-label={$t('settings.audit.availableDays')}>
				{#each files as file (file.date)}
					<button
						on:click={() => selectDay(file.date)}
						class="flex-shrink-0 flex flex-col items-center min-w-[3.5rem] px-2.5 py-1.5 rounded-lg border text-xs transition-colors
							{selectedDay === file.date ? 'border-primary bg-primary/10 text-primary' : 'border-border/60 text-muted-foreground hover:bg-muted/50 hover:text-foreground'}"
					>
						<span class="font-medium">{file.date === today ? $t('settings.audit.today') : chipLabel(file.date)}</span>
						<span class="tabular-nums opacity-80">{file.entries}</span>
					</button>
				{/each}
			</div>
		{/if}

		<div class="flex flex-col sm:flex-row gap-2">
			<div class="relative flex-1">
				<svg class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
				</svg>
				<input
					type="search"
					bind:value={query}
					on:input={onQueryInput}
					placeholder={$t('settings.audit.searchPlaceholder')}
					class="w-full pl-9 pr-8 py-2 bg-muted/50 border border-transparent rounded-lg text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:bg-background focus:border-border focus:ring-2 focus:ring-primary/30"
				/>
				{#if query}
					<button on:click={clearQuery} class="absolute right-2 top-1/2 -translate-y-1/2 p-1 rounded text-muted-foreground hover:text-foreground" aria-label={$t('settings.audit.clearSearch')}>
						<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
					</button>
				{/if}
			</div>
		</div>

		<div class="flex gap-1.5 overflow-x-auto pb-1 -mx-1 px-1">
			{#each FILTERS as item (item.id)}
				<button
					on:click={() => setFilter(item.id)}
					class="flex-shrink-0 px-2.5 py-1 rounded-full text-xs border transition-colors
						{filter === item.id ? 'bg-foreground text-background border-foreground' : 'border-border/60 text-muted-foreground hover:text-foreground hover:bg-muted/50'}"
				>{$t(item.key)}</button>
			{/each}
		</div>
	</div>

	<!-- Summary line -->
	<div class="flex items-center justify-between text-xs text-muted-foreground mb-2 min-h-[1.25rem]">
		<span>
			{#if !loading && !error}
				{#if searching}
					{$t('settings.audit.summarySearch', { count: entries.length })}
				{:else if mode === 'day'}
					{$t('settings.audit.summaryDay', { count: entries.length })}
				{:else}
					{$t('settings.audit.summaryRecent', { count: entries.length })}
				{/if}
				{#if truncated}<span class="ml-1 text-amber-600 dark:text-amber-400">{$t('settings.audit.truncated')}</span>{/if}
			{/if}
		</span>
		<span class="hidden sm:inline">{$t('settings.audit.localTime')}</span>
	</div>

	<!-- List -->
	{#if error}
		<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm">{error}</div>
	{:else if loading && entries.length === 0}
		<div class="space-y-2">
			{#each Array(5) as _}
				<div class="h-14 rounded-lg bg-muted/40 animate-pulse"></div>
			{/each}
		</div>
	{:else if entries.length === 0}
		<div class="flex flex-col items-center text-center py-12 px-4">
			<div class="w-12 h-12 rounded-full bg-muted/60 flex items-center justify-center mb-3">
				<svg class="w-6 h-6 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4" />
				</svg>
			</div>
			<div class="text-sm font-medium text-foreground">
				{searching ? $t('settings.audit.noMatches') : mode === 'day' ? $t('settings.audit.emptyDay') : $t('settings.audit.empty')}
			</div>
			<div class="text-xs text-muted-foreground mt-1 max-w-sm">{$t('settings.audit.emptyHint')}</div>
		</div>
	{:else}
		<div class="relative transition-opacity {loading ? 'opacity-60' : ''}">
			{#each grouped as group (group.day)}
				<div class="sticky top-11 z-[1] -mx-1 px-1 py-1.5 bg-card/95 backdrop-blur text-xs font-medium text-muted-foreground">
					{group.label}
					<span class="ml-1 opacity-70 tabular-nums">· {group.items.length}</span>
				</div>
				<ol class="relative ml-3 border-l border-border/60 mb-3">
					{#each group.items as { entry, key } (key)}
						{@const tone = toneOf(entry.action)}
						{@const open = expanded.has(key)}
						<li class="relative pl-6 pr-1 py-1">
							<span class="absolute -left-[13px] top-3 w-6 h-6 rounded-full ring-4 ring-card flex items-center justify-center {TONE_CLASS[tone]}">
								<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d={TONE_ICON[tone]} />
								</svg>
							</span>
							<button
								on:click={() => toggle(key)}
								aria-expanded={open}
								class="w-full text-left rounded-lg px-3 py-2 hover:bg-muted/40 transition-colors {open ? 'bg-muted/40' : ''}"
							>
								<div class="flex items-start gap-2">
									<div class="min-w-0 flex-1">
										<div class="text-sm text-foreground leading-snug break-words">
											{#if entry.actor}<span class="font-medium">{entry.actor}</span>{' '}{/if}
											{#each highlight(describe(entry), query) as part}
												{#if part.hit}<mark class="bg-amber-200/70 dark:bg-amber-500/30 text-inherit rounded px-0.5">{part.text}</mark>{:else}{part.text}{/if}
											{/each}
										</div>
										<div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
											<span class="tabular-nums">{timeOf(entry)}</span>
											<span class="px-1.5 py-px rounded ring-1 ring-inset {TONE_CLASS[tone]}">{actionLabel(entry.action)}</span>
											{#if entry.source}<span>{sourceLabel(entry.source)}</span>{/if}
											{#if entry.ip}<span class="font-mono">{entry.ip}</span>{/if}
											{#if entry.ua}<span class="hidden sm:inline">{shortUA(entry.ua)}</span>{/if}
											{#each facts(entry) as fact}
												<span class="px-1.5 py-px rounded bg-muted/70 text-foreground/80">{fact}</span>
											{/each}
										</div>
									</div>
									<svg class="w-4 h-4 mt-0.5 flex-shrink-0 text-muted-foreground transition-transform {open ? 'rotate-180' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
									</svg>
								</div>
							</button>
							{#if open}
								<div class="mx-3 mb-2 mt-1 rounded-lg border border-border/60 bg-background/60 p-3 text-xs space-y-1.5 animate-fade-in">
									<div class="grid grid-cols-[5rem_1fr] gap-x-3 gap-y-1">
										<span class="text-muted-foreground">{$t('settings.audit.fields.time')}</span>
										<span class="font-mono break-all">{new Date(entry.time).toLocaleString(getIntlLocale())} <span class="text-muted-foreground">({entry.time})</span></span>
										<span class="text-muted-foreground">{$t('settings.audit.fields.action')}</span>
										<span class="font-mono">{entry.action}</span>
										{#if entry.target}
											<span class="text-muted-foreground">{$t('settings.audit.fields.target')}</span>
											<span class="font-mono break-all">
												{#if isDate(entry.target) && entry.action !== 'diary.delete'}
													<a href="/diary/{entry.target}" class="text-primary hover:underline">{entry.target}</a>
												{:else}{entry.target}{/if}
											</span>
										{/if}
										{#if entry.source}
											<span class="text-muted-foreground">{$t('settings.audit.fields.source')}</span>
											<span>{sourceLabel(entry.source)}</span>
										{/if}
										{#if entry.ip}
											<span class="text-muted-foreground">IP</span>
											<span class="font-mono">{entry.ip}</span>
										{/if}
										{#if entry.ua}
											<span class="text-muted-foreground">{$t('settings.audit.fields.device')}</span>
											<span class="font-mono break-all">{entry.ua}</span>
										{/if}
									</div>
									{#if entry.detail && Object.keys(entry.detail).length > 0}
										<pre class="mt-2 p-2 rounded bg-muted/50 font-mono text-[11px] leading-relaxed overflow-x-auto whitespace-pre-wrap break-all">{JSON.stringify(entry.detail, null, 2)}</pre>
									{/if}
								</div>
							{/if}
						</li>
					{/each}
				</ol>
			{/each}
		</div>
	{/if}
</div>
