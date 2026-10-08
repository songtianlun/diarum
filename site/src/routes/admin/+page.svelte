<script lang="ts">
	/**
	 * Admin console. Only admins get past the role check; the server enforces
	 * the same rule on every /api/v1/admin endpoint.
	 */
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { isAuthenticated } from '$lib/api/client';
	import { fetchCurrentUser } from '$lib/api/auth';
	import type { User } from '$lib/api/client';
	import Footer from '$lib/components/ui/Footer.svelte';
	import AdminOverview from '$lib/components/admin/AdminOverview.svelte';
	import AdminUsers from '$lib/components/admin/AdminUsers.svelte';
	import AdminAudit from '$lib/components/admin/AdminAudit.svelte';
	import AdminArchive from '$lib/components/admin/AdminArchive.svelte';
	import AdminVisits from '$lib/components/admin/AdminVisits.svelte';
	import { t } from '$lib/i18n';

	type Tab = 'overview' | 'users' | 'audit' | 'archive' | 'visits';
	const TABS: Tab[] = ['overview', 'users', 'audit', 'archive', 'visits'];

	let me = $state<User | null>(null);
	let status = $state<'loading' | 'ok' | 'forbidden' | 'error'>('loading');
	let tab = $state<Tab>('overview');
	// Filters handed to the audit view when another tab links into it; the
	// key remounts the view so it starts from them.
	let auditInitial = $state<{ user?: string; category?: string; ip?: string; day?: string }>({});
	let auditKey = $state(0);

	function isTab(value: string): value is Tab {
		return (TABS as string[]).includes(value);
	}

	function syncFromHash() {
		const hash = window.location.hash.replace('#', '');
		if (isTab(hash)) tab = hash;
	}

	function setTab(next: Tab) {
		tab = next;
		const url = new URL(window.location.href);
		url.hash = next;
		history.replaceState(null, '', url);
		window.scrollTo({ top: 0 });
	}

	function openAudit(filter: { user?: string; category?: string; ip?: string; day?: string } = {}) {
		auditInitial = filter;
		auditKey++;
		setTab('audit');
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		syncFromHash();
		window.addEventListener('hashchange', syncFromHash);
		fetchCurrentUser()
			.then((user) => {
				me = user;
				status = user?.role === 'admin' ? 'ok' : 'forbidden';
			})
			.catch(() => (status = 'error'));
		return () => window.removeEventListener('hashchange', syncFromHash);
	});
</script>

<svelte:head>
	<title>{$t('admin.pageTitle')} - Diarum</title>
</svelte:head>

<div class="min-h-screen bg-background">
	<div class="sticky top-0 z-20">
		<header class="glass border-b border-border/50">
			<div class="max-w-6xl mx-auto px-4 h-11">
				<div class="grid grid-cols-[auto_1fr_auto] items-center gap-2 h-full">
					<a href="/" class="flex items-center gap-2 hover:opacity-80 transition-opacity" title={$t('nav.home')}>
						<img src="/logo.png" alt="Diarum" class="w-6 h-6" />
						<span class="hidden sm:inline text-lg font-semibold text-foreground">Diarum</span>
					</a>
					<div class="text-sm font-medium text-foreground text-center truncate">{$t('admin.title')}</div>
					<div class="justify-self-end flex items-center gap-1">
						<a href="/settings" class="p-1.5 hover:bg-muted/50 rounded-lg transition-colors" title={$t('nav.settings')} aria-label={$t('nav.settings')}>
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>
						</a>
						<a href="/diary" class="p-1.5 hover:bg-muted/50 rounded-lg transition-colors" title={$t('nav.diary')} aria-label={$t('nav.diary')}>
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
						</a>
					</div>
				</div>
			</div>
		</header>
	</div>

	<div class="max-w-6xl mx-auto px-4 py-6">
		<main class="w-full max-w-5xl mx-auto">
			{#if status === 'loading'}
				<div class="flex flex-col items-center justify-center py-20 gap-3">
					<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>
					<div class="text-muted-foreground text-sm">{$t('common.loading')}</div>
				</div>
			{:else if status !== 'ok'}
				<div class="max-w-md mx-auto text-center py-16 px-4">
					<div class="mx-auto w-12 h-12 rounded-full bg-muted/60 flex items-center justify-center mb-4">
						<svg class="w-6 h-6 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" /></svg>
					</div>
					<h1 class="text-lg font-semibold text-foreground">{status === 'forbidden' ? $t('admin.forbiddenTitle') : $t('admin.loadFailed')}</h1>
					<p class="text-sm text-muted-foreground mt-2">{status === 'forbidden' ? $t('admin.forbiddenDesc') : ''}</p>
					{#if status === 'forbidden'}
						<pre class="mt-4 p-3 rounded-lg bg-muted/50 font-mono text-xs text-left overflow-x-auto">diarum users set-role {me?.username ?? '<username>'} admin</pre>
					{/if}
					<a href="/diary" class="inline-block mt-6 px-4 py-2 rounded-lg bg-primary text-primary-foreground text-sm hover:bg-primary/90">{$t('admin.backToDiary')}</a>
				</div>
			{:else}
				<nav class="mb-4 -mx-4 px-4 sm:mx-0 sm:px-0 overflow-x-auto" aria-label={$t('admin.title')}>
					<div class="inline-flex min-w-full sm:min-w-0 p-1 rounded-xl bg-muted/60 gap-1">
						{#each TABS as id}
							<button
								onclick={() => setTab(id)}
								aria-current={tab === id ? 'page' : undefined}
								class="flex-1 sm:flex-none px-3 sm:px-4 py-1.5 rounded-lg text-sm whitespace-nowrap transition-all {tab === id ? 'bg-card text-foreground shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}"
							>{$t(`admin.tabs.${id}`)}</button>
						{/each}
					</div>
				</nav>

				{#if tab === 'overview'}
					<AdminOverview onnavigate={(next, filter) => (next === 'audit' ? openAudit(filter ?? {}) : setTab(next))} />
				{:else if tab === 'users'}
					<AdminUsers currentUserId={me?.id} onaudit={(user) => openAudit({ user })} />
				{:else if tab === 'audit'}
					{#key auditKey}<AdminAudit initial={auditInitial} />{/key}
				{:else if tab === 'archive'}
					<AdminArchive onview={(day) => openAudit({ day })} />
				{:else}
					<AdminVisits />
				{/if}
			{/if}
		</main>
	</div>

	<Footer maxWidth="6xl" />
</div>
