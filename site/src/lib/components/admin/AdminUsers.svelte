<script lang="ts">
	/**
	 * Admin → Users: every account with its role and activity, searchable and
	 * sortable, with role changes confirmed inline.
	 */
	import { onDestroy, onMount } from 'svelte';
	import { listUsers, setUserRole, parseStoredTime, type AdminUser, type Role } from '$lib/api/admin';
	import { getIntlLocale, t } from '$lib/i18n';

	let { currentUserId = '', onaudit }: { currentUserId?: string; onaudit?: (userId: string) => void } = $props();

	const PAGE = 50;
	const SORTS = ['created', '-created', 'username', '-diaries', 'last_active'];

	let users = $state<AdminUser[]>([]);
	let total = $state(0);
	let admins = $state(0);
	let query = $state('');
	let role = $state<'' | Role>('');
	let sort = $state('created');
	let page = $state(0);
	let loading = $state(true);
	let error = $state('');
	let confirming = $state<string | null>(null);
	let saving = $state<string | null>(null);
	let rowError = $state<{ id: string; message: string } | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let token = 0;

	onMount(() => {
		void load();
	});
	onDestroy(() => clearTimeout(timer));

	async function load() {
		const mine = ++token;
		loading = true;
		error = '';
		try {
			const result = await listUsers({ q: query, role, sort, limit: PAGE, offset: page * PAGE });
			if (mine !== token) return;
			users = result.users;
			total = result.total;
			admins = result.admins;
		} catch (e) {
			if (mine !== token) return;
			error = e instanceof Error ? e.message : $t('admin.loadFailed');
		} finally {
			if (mine === token) loading = false;
		}
	}

	function onSearch() {
		clearTimeout(timer);
		timer = setTimeout(() => {
			page = 0;
			void load();
		}, 250);
	}

	function setRoleFilter(next: '' | Role) {
		role = next;
		page = 0;
		void load();
	}

	function goPage(delta: number) {
		page = Math.max(0, page + delta);
		void load();
	}

	async function changeRole(user: AdminUser) {
		const next: Role = user.role === 'admin' ? 'user' : 'admin';
		saving = user.id;
		rowError = null;
		try {
			const updated = await setUserRole(user.id, next);
			users = users.map((u) => (u.id === user.id ? { ...u, role: updated.role } : u));
			admins += next === 'admin' ? 1 : -1;
			confirming = null;
		} catch (e) {
			rowError = { id: user.id, message: e instanceof Error ? e.message : $t('admin.users.roleFailed') };
		}
		saving = null;
	}

	function when(value: string): string {
		const date = parseStoredTime(value);
		if (!date) return $t('admin.users.never');
		return date.toLocaleDateString(getIntlLocale(), { year: 'numeric', month: 'short', day: 'numeric' });
	}

	function relative(value: string): string {
		const date = parseStoredTime(value);
		if (!date) return $t('admin.users.never');
		const days = Math.floor((Date.now() - date.getTime()) / 86400000);
		if (days <= 0) return $t('admin.users.today');
		if (days < 30) return $t('admin.users.daysAgo', { count: days });
		return when(value);
	}

	function initial(user: AdminUser): string {
		return (user.name || user.username || '?').trim().charAt(0).toUpperCase();
	}

	let pages = $derived(Math.max(1, Math.ceil(total / PAGE)));
</script>

<div class="space-y-6">
	<section class="bg-card rounded-xl shadow-sm border border-border/50 p-4 sm:p-6 animate-fade-in">
		<div class="flex items-start justify-between gap-3 mb-4">
			<div class="min-w-0">
				<h2 class="text-lg font-semibold text-foreground">{$t('admin.users.heading')}</h2>
				<p class="text-sm text-muted-foreground mt-1">{$t('admin.users.description', { total, admins })}</p>
			</div>
			<button onclick={load} disabled={loading} title={$t('admin.refresh')} aria-label={$t('admin.refresh')} class="flex-shrink-0 p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors disabled:opacity-50">
				<svg class="w-4 h-4 {loading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>
			</button>
		</div>

		<!-- Toolbar -->
		<div class="flex flex-col sm:flex-row gap-2 mb-4">
			<div class="relative flex-1">
				<svg class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
				<input type="search" bind:value={query} oninput={onSearch} placeholder={$t('admin.users.searchPlaceholder')} class="w-full pl-9 pr-3 py-2 bg-muted/50 border border-transparent rounded-lg text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:bg-background focus:border-border focus:ring-2 focus:ring-primary/30" />
			</div>
			<div class="flex gap-2">
				<div class="inline-flex p-0.5 rounded-lg bg-muted/60" role="tablist">
					{#each [['', 'all'], ['admin', 'admins'], ['user', 'users']] as [id, key]}
						<button role="tab" aria-selected={role === id} onclick={() => setRoleFilter(id as '' | Role)} class="px-3 py-1.5 text-sm rounded-md transition-all whitespace-nowrap {role === id ? 'bg-card text-foreground shadow-sm font-medium' : 'text-muted-foreground hover:text-foreground'}">{$t(`admin.users.filter.${key}`)}</button>
					{/each}
				</div>
				<label class="sr-only" for="admin-user-sort">{$t('admin.users.sortLabel')}</label>
				<select id="admin-user-sort" bind:value={sort} onchange={() => { page = 0; void load(); }} class="min-w-0 flex-1 sm:flex-none px-2 py-1.5 bg-card border border-border/60 rounded-lg text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-primary/40">
					{#each SORTS as id}<option value={id}>{$t(`admin.users.sort.${id.replace('-', 'desc_')}`)}</option>{/each}
				</select>
			</div>
		</div>

		{#if error}
			<div class="p-3 bg-destructive/10 text-destructive rounded-lg text-sm">{error}</div>
		{:else if loading && users.length === 0}
			<div class="space-y-2">{#each Array(4) as _}<div class="h-16 rounded-lg bg-muted/40 animate-pulse"></div>{/each}</div>
		{:else if users.length === 0}
			<div class="py-12 text-center text-sm text-muted-foreground">{$t('admin.users.empty')}</div>
		{:else}
			<ul class="divide-y divide-border/50 -mx-2 transition-opacity {loading ? 'opacity-60' : ''}">
				{#each users as user (user.id)}
					{@const isMe = user.id === currentUserId}
					<li class="px-2 py-3">
						<div class="flex items-start gap-3">
							<div class="flex-shrink-0 w-9 h-9 rounded-full flex items-center justify-center text-sm font-semibold {user.role === 'admin' ? 'bg-fuchsia-500/15 text-fuchsia-700 dark:text-fuchsia-300' : 'bg-muted text-muted-foreground'}">{initial(user)}</div>
							<div class="min-w-0 flex-1">
								<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
									<span class="font-medium text-foreground truncate">{user.username}</span>
									{#if user.role === 'admin'}
										<span class="px-1.5 py-px rounded text-[11px] font-medium ring-1 ring-inset bg-fuchsia-500/10 text-fuchsia-700 dark:text-fuchsia-300 ring-fuchsia-500/20">{$t('admin.users.roleAdmin')}</span>
									{:else}
										<span class="px-1.5 py-px rounded text-[11px] ring-1 ring-inset bg-muted text-muted-foreground ring-border/60">{$t('admin.users.roleUser')}</span>
									{/if}
									{#if isMe}<span class="text-[11px] text-primary">{$t('admin.users.you')}</span>{/if}
								</div>
								<div class="text-xs text-muted-foreground truncate mt-0.5">{user.email || '—'}{#if user.name} · {user.name}{/if}</div>
								<div class="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
									<span>{$t('admin.users.diaries', { count: user.diaries })}</span>
									<span>{$t('admin.users.media', { count: user.media })}</span>
									<span class="hidden sm:inline">{$t('admin.users.chats', { count: user.conversations })}</span>
									<span>{$t('admin.users.joined', { date: when(user.created) })}</span>
									<span>{$t('admin.users.lastActive', { when: relative(user.last_diary_at) })}</span>
								</div>
								{#if rowError?.id === user.id}<div class="mt-1 text-xs text-rose-600 dark:text-rose-400">{rowError.message}</div>{/if}
							</div>
							<div class="flex flex-shrink-0 flex-col items-end gap-1.5 sm:flex-row sm:items-center">
								<button onclick={() => onaudit?.(user.id)} class="px-2 py-1 text-xs rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/60" title={$t('admin.users.viewAudit')}>{$t('admin.users.audit')}</button>
								{#if confirming === user.id}
									<div class="flex items-center gap-1">
										<button onclick={() => changeRole(user)} disabled={saving === user.id} class="px-2 py-1 text-xs rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50">{saving === user.id ? $t('common.saving') : $t('admin.users.confirm')}</button>
										<button onclick={() => (confirming = null)} class="px-2 py-1 text-xs rounded-md text-muted-foreground hover:bg-muted/60">{$t('common.cancel')}</button>
									</div>
								{:else}
									<button
										onclick={() => { confirming = user.id; rowError = null; }}
										disabled={isMe && user.role === 'admin'}
										title={isMe && user.role === 'admin' ? $t('admin.users.cannotDemoteSelf') : ''}
										class="px-2 py-1 text-xs rounded-md border border-border/60 text-foreground hover:bg-muted/50 disabled:opacity-40 disabled:cursor-not-allowed whitespace-nowrap"
									>{user.role === 'admin' ? $t('admin.users.makeUser') : $t('admin.users.makeAdmin')}</button>
								{/if}
							</div>
						</div>
					</li>
				{/each}
			</ul>
			{#if pages > 1}
				<div class="mt-4 flex items-center justify-between text-sm">
					<button onclick={() => goPage(-1)} disabled={page === 0} class="px-3 py-1.5 rounded-md border border-border/60 hover:bg-muted/50 disabled:opacity-40">{$t('admin.prev')}</button>
					<span class="text-muted-foreground tabular-nums">{page + 1} / {pages}</span>
					<button onclick={() => goPage(1)} disabled={page + 1 >= pages} class="px-3 py-1.5 rounded-md border border-border/60 hover:bg-muted/50 disabled:opacity-40">{$t('admin.next')}</button>
				</div>
			{/if}
		{/if}
	</section>

	<section class="rounded-xl border border-dashed border-border/70 p-4 sm:p-5 text-sm">
		<h3 class="font-medium text-foreground">{$t('admin.users.cliHeading')}</h3>
		<p class="text-muted-foreground mt-1 text-xs">{$t('admin.users.cliDesc')}</p>
		<pre class="mt-3 p-3 rounded-lg bg-muted/50 font-mono text-xs overflow-x-auto leading-relaxed">diarum users list
diarum users set-role &lt;username|email|id&gt; admin
docker exec -it &lt;container&gt; /app/diarum users list</pre>
	</section>
</div>
