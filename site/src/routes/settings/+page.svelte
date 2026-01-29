<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { isAuthenticated } from '$lib/api/client';
	import { getApiToken, toggleApiToken, resetApiToken, type ApiTokenStatus } from '$lib/api/settings';

	let loading = true;
	let tokenStatus: ApiTokenStatus = { exists: false, enabled: false, token: '' };
	let copied = false;
	let resetting = false;
	let toggling = false;

	async function loadTokenStatus() {
		loading = true;
		tokenStatus = await getApiToken();
		loading = false;
	}

	async function handleToggle() {
		toggling = true;
		try {
			tokenStatus = await toggleApiToken();
		} catch (e) {
			console.error('Failed to toggle API token');
		}
		toggling = false;
	}

	async function handleReset() {
		if (!confirm('Are you sure you want to reset your API token? Any existing integrations will stop working.')) {
			return;
		}
		resetting = true;
		try {
			tokenStatus = await resetApiToken();
		} catch (e) {
			console.error('Failed to reset API token');
		}
		resetting = false;
	}

	async function copyToken() {
		if (tokenStatus.token) {
			await navigator.clipboard.writeText(tokenStatus.token);
			copied = true;
			setTimeout(() => copied = false, 2000);
		}
	}

	function getBaseUrl(): string {
		if (typeof window !== 'undefined') {
			return window.location.origin;
		}
		return '';
	}

	onMount(() => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		loadTokenStatus();
	});
</script>

<svelte:head>
	<title>Settings - Diaria</title>
</svelte:head>

<div class="min-h-screen bg-background">
	<!-- Header -->
	<header class="glass border-b border-border/50 sticky top-0 z-20">
		<div class="max-w-4xl mx-auto px-4 h-11">
			<div class="flex items-center justify-between h-full">
				<div class="flex items-center gap-3">
					<a href="/diary" class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200" title="Back">
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
						</svg>
					</a>
					<span class="text-lg font-semibold text-foreground">Settings</span>
				</div>
			</div>
		</div>
	</header>

	<!-- Main Content -->
	<main class="max-w-4xl mx-auto px-4 py-6">
		{#if loading}
			<div class="flex flex-col items-center justify-center py-20 gap-3">
				<svg class="w-6 h-6 animate-spin text-primary" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
				</svg>
				<div class="text-muted-foreground text-sm">Loading...</div>
			</div>
		{:else}
			<div class="space-y-6">
				<!-- API Settings Section -->
				<div class="bg-card rounded-xl shadow-sm border border-border/50 p-6 animate-fade-in">
					<h2 class="text-lg font-semibold text-foreground mb-4">API Access</h2>
					<p class="text-sm text-muted-foreground mb-6">
						Enable API access to retrieve your diary entries programmatically. Use your API token to authenticate requests.
					</p>

					<!-- Enable/Disable Toggle -->
					<div class="flex items-center justify-between py-4 border-b border-border/50">
						<div>
							<div class="font-medium text-foreground">Enable API</div>
							<div class="text-sm text-muted-foreground">Allow external access to your diary data</div>
						</div>
						<button
							on:click={handleToggle}
							disabled={toggling}
							class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 {tokenStatus.enabled ? 'bg-primary' : 'bg-muted'}"
						>
							<span
								class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform duration-200 {tokenStatus.enabled ? 'translate-x-6' : 'translate-x-1'}"
							/>
						</button>
					</div>

					{#if tokenStatus.enabled && tokenStatus.token}
						<!-- API Token Display -->
						<div class="py-4 border-b border-border/50">
							<div class="font-medium text-foreground mb-2">Your API Token</div>
							<div class="flex items-center gap-2">
								<code class="flex-1 px-3 py-2 bg-muted rounded-lg text-sm font-mono text-foreground overflow-x-auto">
									{tokenStatus.token}
								</code>
								<button
									on:click={copyToken}
									class="px-3 py-2 text-sm bg-muted hover:bg-muted/80 rounded-lg transition-colors duration-200"
								>
									{copied ? 'Copied!' : 'Copy'}
								</button>
							</div>
							<p class="text-xs text-muted-foreground mt-2">
								Keep this token secret. Anyone with this token can read your diary entries.
							</p>
						</div>

						<!-- Reset Token -->
						<div class="py-4 border-b border-border/50">
							<div class="flex items-center justify-between">
								<div>
									<div class="font-medium text-foreground">Reset Token</div>
									<div class="text-sm text-muted-foreground">Generate a new API token</div>
								</div>
								<button
									on:click={handleReset}
									disabled={resetting}
									class="px-4 py-2 text-sm bg-destructive/10 text-destructive hover:bg-destructive/20 rounded-lg transition-colors duration-200 disabled:opacity-50"
								>
									{resetting ? 'Resetting...' : 'Reset Token'}
								</button>
							</div>
						</div>

						<!-- API Documentation -->
						<div class="py-4">
							<div class="font-medium text-foreground mb-3">API Usage</div>
							<div class="space-y-4 text-sm">
								<div>
									<div class="text-muted-foreground mb-1">Get diary by date:</div>
									<code class="block px-3 py-2 bg-muted rounded-lg font-mono text-xs overflow-x-auto">
										GET {getBaseUrl()}/api/v1/diaries?token={tokenStatus.token}&date=YYYY-MM-DD
									</code>
								</div>
								<div>
									<div class="text-muted-foreground mb-1">Get diaries in date range:</div>
									<code class="block px-3 py-2 bg-muted rounded-lg font-mono text-xs overflow-x-auto">
										GET {getBaseUrl()}/api/v1/diaries?token={tokenStatus.token}&start=YYYY-MM-DD&end=YYYY-MM-DD
									</code>
								</div>
								<div>
									<div class="text-muted-foreground mb-1">Example with curl:</div>
									<code class="block px-3 py-2 bg-muted rounded-lg font-mono text-xs overflow-x-auto whitespace-pre-wrap">
curl "{getBaseUrl()}/api/v1/diaries?token={tokenStatus.token}&date={new Date().toISOString().split('T')[0]}"
									</code>
								</div>
							</div>
						</div>
					{/if}
				</div>
			</div>
		{/if}
	</main>
</div>
