<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { isAuthenticated } from '$lib/api/client';
	import { getApiToken, toggleApiToken, resetApiToken, type ApiTokenStatus } from '$lib/api/settings';
	import { getAISettings, saveAISettings, fetchModels, buildVectors, buildVectorsIncremental, getVectorStats, type AISettings, type ModelInfo, type BuildVectorsResult, type VectorStats } from '$lib/api/ai';

	let loading = true;
	let tokenStatus: ApiTokenStatus = { exists: false, enabled: false, token: '' };
	let copied = false;
	let resetting = false;
	let toggling = false;

	// AI Settings
	let aiSettings: AISettings = {
		api_key: '',
		base_url: '',
		chat_model: '',
		embedding_model: '',
		enabled: false
	};
	let aiSaving = false;
	let aiError = '';
	let aiSuccess = '';
	let models: ModelInfo[] = [];
	let fetchingModels = false;
	let modelsError = '';

	// Vector building
	let buildingVectors = false;
	let buildResult: BuildVectorsResult | null = null;
	let buildError = '';

	// Vector stats
	let vectorStats: VectorStats | null = null;
	let loadingStats = false;

	async function loadTokenStatus() {
		tokenStatus = await getApiToken();
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

	// AI Settings functions
	async function loadAISettings() {
		aiSettings = await getAISettings();
		// Initialize models array with configured models so they display before refresh
		const initialModels: ModelInfo[] = [];
		if (aiSettings.chat_model) {
			initialModels.push({ id: aiSettings.chat_model, object: 'model' });
		}
		if (aiSettings.embedding_model && aiSettings.embedding_model !== aiSettings.chat_model) {
			initialModels.push({ id: aiSettings.embedding_model, object: 'model' });
		}
		models = initialModels;
	}

	async function handleFetchModels() {
		if (!aiSettings.api_key || !aiSettings.base_url) {
			modelsError = 'Please enter API Key and Base URL first';
			return;
		}

		fetchingModels = true;
		modelsError = '';
		try {
			models = await fetchModels(aiSettings.api_key, aiSettings.base_url);
		} catch (e) {
			modelsError = e instanceof Error ? e.message : 'Failed to fetch models';
		}
		fetchingModels = false;
	}

	async function handleSaveAISettings() {
		aiError = '';
		aiSuccess = '';

		// Validate: if enabling, all fields must be filled
		if (aiSettings.enabled) {
			if (!aiSettings.api_key || !aiSettings.base_url || !aiSettings.chat_model || !aiSettings.embedding_model) {
				aiError = 'All fields must be filled before enabling AI features';
				return;
			}
		}

		aiSaving = true;
		try {
			await saveAISettings(aiSettings);
			aiSuccess = 'AI settings saved successfully';
			setTimeout(() => aiSuccess = '', 3000);
		} catch (e) {
			aiError = e instanceof Error ? e.message : 'Failed to save AI settings';
		}
		aiSaving = false;
	}

	async function handleBuildVectors(incremental: boolean = false) {
		if (!aiSettings.enabled) {
			buildError = 'Please enable AI features first';
			return;
		}

		buildingVectors = true;
		buildError = '';
		buildResult = null;

		try {
			if (incremental) {
				buildResult = await buildVectorsIncremental();
			} else {
				buildResult = await buildVectors();
			}
			// Refresh stats after building
			await loadVectorStats();
		} catch (e) {
			buildError = e instanceof Error ? e.message : 'Failed to build vectors';
		}
		buildingVectors = false;
	}

	async function loadVectorStats() {
		if (!aiSettings.enabled) return;

		loadingStats = true;
		try {
			vectorStats = await getVectorStats();
		} catch (e) {
			console.error('Failed to load vector stats:', e);
			vectorStats = null;
		}
		loadingStats = false;
	}

	// Check if AI can be enabled
	$: canEnableAI = aiSettings.api_key && aiSettings.base_url && aiSettings.chat_model && aiSettings.embedding_model;

	onMount(async () => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}
		loading = true;
		await Promise.all([loadTokenStatus(), loadAISettings()]);
		loading = false;
		// Load vector stats if AI is enabled
		if (aiSettings.enabled) {
			await loadVectorStats();
		}
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

				<!-- AI Settings Section -->
				<div class="bg-card rounded-xl shadow-sm border border-border/50 p-6 animate-fade-in">
					<h2 class="text-lg font-semibold text-foreground mb-4">AI Assistant</h2>
					<p class="text-sm text-muted-foreground mb-6">
						Configure AI services for intelligent diary analysis and conversation. Supports OpenAI-compatible APIs.
					</p>

					{#if aiError}
						<div class="mb-4 p-3 bg-destructive/10 text-destructive rounded-lg text-sm">
							{aiError}
						</div>
					{/if}

					{#if aiSuccess}
						<div class="mb-4 p-3 bg-green-500/10 text-green-600 rounded-lg text-sm">
							{aiSuccess}
						</div>
					{/if}

					<!-- API Key -->
					<div class="py-4 border-b border-border/50">
						<label class="block font-medium text-foreground mb-2">API Key</label>
						<input
							type="password"
							bind:value={aiSettings.api_key}
							placeholder="sk-..."
							class="w-full px-3 py-2 bg-muted rounded-lg text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary"
						/>
						<p class="text-xs text-muted-foreground mt-1">Your API key for the AI service</p>
					</div>

					<!-- Base URL -->
					<div class="py-4 border-b border-border/50">
						<label class="block font-medium text-foreground mb-2">API Base URL</label>
						<input
							type="text"
							bind:value={aiSettings.base_url}
							placeholder="https://api.openai.com"
							class="w-full px-3 py-2 bg-muted rounded-lg text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary"
						/>
						<p class="text-xs text-muted-foreground mt-1">Base URL for the OpenAI-compatible API</p>
					</div>

					{#if modelsError}
						<div class="mt-4 p-3 bg-destructive/10 text-destructive rounded-lg text-sm">
							{modelsError}
						</div>
					{/if}

					<!-- Chat Model -->
					<div class="py-4 border-b border-border/50">
						<label class="block font-medium text-foreground mb-2">Chat Model</label>
						<div class="flex items-center gap-2">
							<select
								bind:value={aiSettings.chat_model}
								class="flex-1 px-3 py-2 bg-muted rounded-lg text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-primary"
							>
								<option value="">Select a model</option>
								{#each models as model}
									<option value={model.id}>{model.id}</option>
								{/each}
							</select>
							<button
								on:click={handleFetchModels}
								disabled={fetchingModels}
								class="p-2 bg-muted hover:bg-muted/80 rounded-lg transition-colors duration-200 disabled:opacity-50"
								title="Refresh models"
							>
								<svg class="w-5 h-5 {fetchingModels ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
								</svg>
							</button>
						</div>
						<p class="text-xs text-muted-foreground mt-1">Model for AI conversations</p>
					</div>

					<!-- Embedding Model -->
					<div class="py-4 border-b border-border/50">
						<label class="block font-medium text-foreground mb-2">Embedding Model</label>
						<div class="flex items-center gap-2">
							<select
								bind:value={aiSettings.embedding_model}
								class="flex-1 px-3 py-2 bg-muted rounded-lg text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-primary"
							>
								<option value="">Select a model</option>
								{#each models as model}
									<option value={model.id}>{model.id}</option>
								{/each}
							</select>
							<button
								on:click={handleFetchModels}
								disabled={fetchingModels}
								class="p-2 bg-muted hover:bg-muted/80 rounded-lg transition-colors duration-200 disabled:opacity-50"
								title="Refresh models"
							>
								<svg class="w-5 h-5 {fetchingModels ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
								</svg>
							</button>
						</div>
						<p class="text-xs text-muted-foreground mt-1">Model for text vectorization</p>
					</div>

					<!-- Enable AI Toggle -->
					<div class="py-4 border-b border-border/50">
						<div class="flex items-center justify-between">
							<div>
								<div class="font-medium text-foreground">Enable AI Features</div>
								<div class="text-sm text-muted-foreground">
									{#if !canEnableAI}
										Fill all fields above to enable
									{:else}
										AI assistant is ready to use
									{/if}
								</div>
							</div>
							<button
								on:click={() => { if (canEnableAI) aiSettings.enabled = !aiSettings.enabled; }}
								disabled={!canEnableAI && !aiSettings.enabled}
								class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 {aiSettings.enabled ? 'bg-primary' : 'bg-muted'} {!canEnableAI && !aiSettings.enabled ? 'opacity-50 cursor-not-allowed' : ''}"
							>
								<span
									class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform duration-200 {aiSettings.enabled ? 'translate-x-6' : 'translate-x-1'}"
								/>
							</button>
						</div>
					</div>

					<!-- Build Vectors -->
					{#if aiSettings.enabled}
						<div class="py-4 border-b border-border/50">
							<div class="flex items-center justify-between">
								<div>
									<div class="font-medium text-foreground">Build Vector Index</div>
									<div class="text-sm text-muted-foreground">
										Generate embeddings for diary entries
									</div>
								</div>
								<div class="flex items-center gap-2">
									<button
										on:click={() => handleBuildVectors(true)}
										disabled={buildingVectors}
										class="px-3 py-1.5 text-sm bg-primary text-primary-foreground hover:bg-primary/90 rounded-lg transition-colors duration-200 disabled:opacity-50 flex items-center gap-1.5"
										title="Only build new and outdated entries"
									>
										{#if buildingVectors}
											<svg class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
												<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
												<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
											</svg>
										{:else}
											<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
												<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
											</svg>
										{/if}
										Update
									</button>
									<button
										on:click={() => handleBuildVectors(false)}
										disabled={buildingVectors}
										class="px-3 py-1.5 text-sm bg-muted hover:bg-muted/80 rounded-lg transition-colors duration-200 disabled:opacity-50 flex items-center gap-1.5"
										title="Rebuild all entries from scratch"
									>
										<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
										</svg>
										Rebuild All
									</button>
								</div>
							</div>

							{#if buildError}
								<div class="mt-3 p-3 bg-destructive/10 text-destructive rounded-lg text-sm">
									{buildError}
								</div>
							{/if}

							{#if buildResult}
								<div class="mt-3 p-3 bg-muted rounded-lg text-sm">
									<div class="font-medium text-foreground mb-2">Build Result</div>
									<div class="space-y-1 text-muted-foreground">
										<div>Total diaries: {buildResult.total}</div>
										<div class="text-green-600">Success: {buildResult.success}</div>
										{#if buildResult.failed > 0}
											<div class="text-destructive">Failed: {buildResult.failed}</div>
										{/if}
									</div>
									{#if buildResult.error_details && buildResult.error_details.length > 0}
										<div class="mt-2 pt-2 border-t border-border/50">
											<div class="font-medium text-destructive mb-1">Errors:</div>
											<div class="text-xs text-muted-foreground space-y-1 max-h-32 overflow-y-auto">
												{#each buildResult.error_details as error}
													<div>{error}</div>
												{/each}
											</div>
										</div>
									{/if}
								</div>
							{/if}
						</div>

						<!-- Vector Index Status -->
						<div class="py-4 border-b border-border/50">
							<div class="font-medium text-foreground mb-2">Vector Index Status</div>
							{#if loadingStats}
								<div class="flex items-center gap-2 text-sm text-muted-foreground">
									<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
										<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
										<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
									</svg>
									Loading...
								</div>
							{:else if vectorStats}
								<div class="space-y-3">
									<!-- Segmented Progress Bar -->
									<div class="space-y-2">
										<div class="flex items-center justify-between text-sm">
											<span class="text-muted-foreground">Total diaries</span>
											<span class="font-medium text-foreground">{vectorStats.diary_count}</span>
										</div>
										<div class="w-full bg-muted rounded-full h-2 flex overflow-hidden">
											{#if vectorStats.diary_count > 0}
												{#if vectorStats.indexed_count > 0}
													<div
														class="h-2 bg-green-500 transition-all duration-300"
														style="width: {(vectorStats.indexed_count / vectorStats.diary_count * 100)}%"
													></div>
												{/if}
												{#if vectorStats.outdated_count > 0}
													<div
														class="h-2 bg-amber-500 transition-all duration-300"
														style="width: {(vectorStats.outdated_count / vectorStats.diary_count * 100)}%"
													></div>
												{/if}
												{#if vectorStats.pending_count > 0}
													<div
														class="h-2 bg-gray-400 transition-all duration-300"
														style="width: {(vectorStats.pending_count / vectorStats.diary_count * 100)}%"
													></div>
												{/if}
											{/if}
										</div>
									</div>

									<!-- Stats Legend -->
									<div class="flex flex-wrap gap-4 text-xs">
										<div class="flex items-center gap-1.5">
											<div class="w-2.5 h-2.5 rounded-full bg-green-500"></div>
											<span class="text-muted-foreground">Indexed: <span class="font-medium text-foreground">{vectorStats.indexed_count}</span></span>
										</div>
										<div class="flex items-center gap-1.5">
											<div class="w-2.5 h-2.5 rounded-full bg-amber-500"></div>
											<span class="text-muted-foreground">Outdated: <span class="font-medium text-foreground">{vectorStats.outdated_count}</span></span>
										</div>
										<div class="flex items-center gap-1.5">
											<div class="w-2.5 h-2.5 rounded-full bg-gray-400"></div>
											<span class="text-muted-foreground">Pending: <span class="font-medium text-foreground">{vectorStats.pending_count}</span></span>
										</div>
									</div>

									<!-- Status Message -->
									{#if vectorStats.indexed_count === vectorStats.diary_count && vectorStats.diary_count > 0}
										<div class="text-xs text-green-600 flex items-center gap-1">
											<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
												<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
											</svg>
											All diaries indexed and up to date
										</div>
									{:else if vectorStats.outdated_count > 0 || vectorStats.pending_count > 0}
										<div class="text-xs text-amber-600 flex items-center gap-1">
											<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
												<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
											</svg>
											{vectorStats.outdated_count + vectorStats.pending_count} diaries need indexing
										</div>
									{:else if vectorStats.diary_count === 0}
										<div class="text-xs text-muted-foreground">
											No diaries to index
										</div>
									{/if}
								</div>
							{:else}
								<div class="text-sm text-muted-foreground">
									No index data available
								</div>
							{/if}
						</div>
					{/if}

					<!-- Save Button -->
					<div class="pt-4">
						<button
							on:click={handleSaveAISettings}
							disabled={aiSaving}
							class="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors duration-200 disabled:opacity-50"
						>
							{aiSaving ? 'Saving...' : 'Save AI Settings'}
						</button>
					</div>
				</div>
			</div>
		{/if}
	</main>
</div>
