<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { isAuthenticated } from '$lib/api/client';
	import { getApiToken, toggleApiToken, resetApiToken, type ApiTokenStatus } from '$lib/api/settings';
	import { getAISettings, saveAISettings, fetchModels, buildVectors, buildVectorsIncremental, getVectorStats, type AISettings, type ModelInfo, type BuildVectorsResult, type VectorStats } from '$lib/api/ai';
	import { exportDiaries, importDiaries, type ExportStats, type ImportStats, type ExportOptions } from '$lib/api/exportImport';
	import { getCheveretoSettings, saveCheveretoSettings, testCheveretoConnection, type CheveretoSettings } from '$lib/api/chevereto';
	import { cheveretoSettings as cheveretoStore, loadCheveretoSettings } from '$lib/stores/chevereto';
	import PageHeader from '$lib/components/ui/PageHeader.svelte';
	import Footer from '$lib/components/ui/Footer.svelte';
	import SettingsToc from '$lib/components/ui/SettingsToc.svelte';
	import SyncSettings from '$lib/components/ui/SyncSettings.svelte';

	// TOC state
	let showMobileToc = false;
	let showDesktopToc = true;
	let isMobile = false;

	function checkMobile() {
		if (typeof window !== 'undefined') {
			isMobile = window.innerWidth < 1024;
		}
	}

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
	let originalAISettings: AISettings = { ...aiSettings };
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

	// Chevereto Settings
	let cheveretoSettingsLocal: CheveretoSettings = {
		enabled: false,
		domain: '',
		api_key: '',
		album_id: ''
	};
	let originalCheveretoSettings: CheveretoSettings = { ...cheveretoSettingsLocal };
	let cheveretoSaving = false;
	let cheveretoError = '';
	let cheveretoSuccess = '';
	let cheveretoTesting = false;
	let cheveretoTestResult: { success: boolean; message: string } | null = null;

	// Data management (export/import)
	let exporting = false;
	let exportStats: ExportStats | null = null;
	let exportError = '';
	let importing = false;
	let importStats: ImportStats | null = null;
	let importError = '';
	let importFile: File | null = null;

	// Export options
	let exportOptions: ExportOptions = {
		date_range: '3m',
		include_diaries: true,
		include_media: true,
		include_conversations: true
	};
	let customStartDate = '';
	let customEndDate = '';
	let showExportOptions = true;

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
		originalAISettings = JSON.parse(JSON.stringify(aiSettings));
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
			originalAISettings = JSON.parse(JSON.stringify(aiSettings));
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

	// Check if AI settings have changed
	$: aiSettingsChanged = aiSettings.api_key !== originalAISettings.api_key ||
		aiSettings.base_url !== originalAISettings.base_url ||
		aiSettings.chat_model !== originalAISettings.chat_model ||
		aiSettings.embedding_model !== originalAISettings.embedding_model ||
		aiSettings.enabled !== originalAISettings.enabled;

	// Embedding model keywords for sorting
	const embeddingKeywords = ['embed', 'bge', 'e5', 'voyage', 'jina'];

	// Check if a model is likely an embedding model
	function isEmbeddingModel(modelId: string): boolean {
		const lower = modelId.toLowerCase();
		return embeddingKeywords.some(keyword => lower.includes(keyword));
	}

	// Check if a model is likely a chat model (not embedding)
	function isChatModel(modelId: string): boolean {
		return !isEmbeddingModel(modelId);
	}

	// Sorted models for embedding selection (embedding models first)
	$: embeddingModels = [...models].sort((a, b) => {
		const aIsEmbed = isEmbeddingModel(a.id);
		const bIsEmbed = isEmbeddingModel(b.id);
		if (aIsEmbed && !bIsEmbed) return -1;
		if (!aIsEmbed && bIsEmbed) return 1;
		return a.id.localeCompare(b.id);
	});

	// Sorted models for chat selection (chat models first)
	$: chatModels = [...models].sort((a, b) => {
		const aIsChat = isChatModel(a.id);
		const bIsChat = isChatModel(b.id);
		if (aIsChat && !bIsChat) return -1;
		if (!aIsChat && bIsChat) return 1;
		return a.id.localeCompare(b.id);
	});

	// Chevereto functions
	async function loadCheveretoSettingsLocal() {
		cheveretoSettingsLocal = await getCheveretoSettings();
		originalCheveretoSettings = JSON.parse(JSON.stringify(cheveretoSettingsLocal));
	}

	$: canEnableChevereto = cheveretoSettingsLocal.domain && cheveretoSettingsLocal.api_key;

	$: cheveretoSettingsChanged = cheveretoSettingsLocal.enabled !== originalCheveretoSettings.enabled ||
		cheveretoSettingsLocal.domain !== originalCheveretoSettings.domain ||
		cheveretoSettingsLocal.api_key !== originalCheveretoSettings.api_key ||
		cheveretoSettingsLocal.album_id !== originalCheveretoSettings.album_id;

	async function handleTestChevereto() {
		if (!cheveretoSettingsLocal.domain || !cheveretoSettingsLocal.api_key) {
			cheveretoError = 'Please enter Domain and API Key first';
			return;
		}
		cheveretoTesting = true;
		cheveretoTestResult = null;
		cheveretoError = '';
		try {
			cheveretoTestResult = await testCheveretoConnection(
				cheveretoSettingsLocal.domain,
				cheveretoSettingsLocal.api_key
			);
		} catch (e) {
			cheveretoError = e instanceof Error ? e.message : 'Connection test failed';
		}
		cheveretoTesting = false;
	}

	async function handleSaveCheveretoSettings() {
		cheveretoError = '';
		cheveretoSuccess = '';

		if (cheveretoSettingsLocal.enabled) {
			if (!cheveretoSettingsLocal.domain || !cheveretoSettingsLocal.api_key) {
				cheveretoError = 'Domain and API Key are required to enable Chevereto';
				return;
			}
		}

		cheveretoSaving = true;
		try {
			await saveCheveretoSettings(cheveretoSettingsLocal);
			originalCheveretoSettings = JSON.parse(JSON.stringify(cheveretoSettingsLocal));
			// Update the global store so the editor picks up changes
			await loadCheveretoSettings();
			cheveretoSuccess = 'Chevereto settings saved successfully';
			setTimeout(() => cheveretoSuccess = '', 3000);
		} catch (e) {
			cheveretoError = e instanceof Error ? e.message : 'Failed to save Chevereto settings';
		}
		cheveretoSaving = false;
	}

	async function handleExport() {
		exporting = true;
		exportError = '';
		exportStats = null;
		try {
			// Build options with custom dates if needed
			const options: ExportOptions = { ...exportOptions };
			if (options.date_range === 'custom') {
				options.start_date = customStartDate;
				options.end_date = customEndDate;
			}
			exportStats = await exportDiaries(options);
		} catch (e) {
			exportError = e instanceof Error ? e.message : 'Export failed';
		}
		exporting = false;
	}

	function handleImportFileChange(e: Event) {
		const input = e.target as HTMLInputElement;
		importFile = input.files?.[0] || null;
	}

	async function handleImport() {
		if (!importFile) return;
		importing = true;
		importError = '';
		importStats = null;
		try {
			importStats = await importDiaries(importFile);
		} catch (e) {
			importError = e instanceof Error ? e.message : 'Import failed';
		}
		importing = false;
	}

	onMount(async () => {
		if (!$isAuthenticated) {
			goto('/login');
			return;
		}

		// Initialize mobile detection
		checkMobile();
		showDesktopToc = !isMobile;
		window.addEventListener('resize', checkMobile);

		loading = true;
		await Promise.all([loadTokenStatus(), loadAISettings(), loadCheveretoSettingsLocal()]);
		loading = false;
		// Load vector stats if AI is enabled
		if (aiSettings.enabled) {
			await loadVectorStats();
		}

		return () => {
			window.removeEventListener('resize', checkMobile);
		};
	});
</script>

<svelte:head>
	<title>Settings - Diarum</title>
</svelte:head>

<div class="min-h-screen bg-background">
	<!-- Sticky Header Container -->
	<div class="sticky top-0 z-20">
		<!-- Header -->
		<header class="glass border-b border-border/50">
			<div class="max-w-6xl mx-auto px-4 h-11">
				<div class="flex items-center justify-between h-full">
					<!-- Left: Brand -->
					<a href="/" class="flex items-center gap-2 hover:opacity-80 transition-opacity">
						<img src="/logo.png" alt="Diarum" class="w-6 h-6" />
						<span class="hidden sm:inline text-lg font-semibold text-foreground hover:text-primary transition-colors">Diarum</span>
					</a>

					<!-- Center: Title -->
					<div class="text-sm font-medium text-foreground">Settings</div>

					<!-- Right: Actions -->
					<div class="flex items-center gap-2">
						<button
							on:click={() => {
								if (window.innerWidth >= 1024) {
									showDesktopToc = !showDesktopToc;
								} else {
									showMobileToc = !showMobileToc;
								}
							}}
							class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200 {(showDesktopToc || showMobileToc) ? 'bg-muted/50' : ''}"
							title="Table of contents"
						>
							<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
							</svg>
						</button>
					</div>
				</div>
			</div>
		</header>
	</div>

	<!-- Main Content -->
	<div class="max-w-6xl mx-auto px-4 py-6">
		<div class="flex gap-6 {showDesktopToc ? '' : 'justify-center'}">
			<!-- Settings Content -->
			<main class="flex-1 min-w-0 max-w-4xl {showDesktopToc ? 'lg:mx-0' : 'mx-auto'}">
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
				<div id="api-access" class="bg-card rounded-xl shadow-sm border border-border/50 p-6 animate-fade-in scroll-mt-16">
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
				<div id="ai-assistant" class="bg-card rounded-xl shadow-sm border border-border/50 p-6 animate-fade-in scroll-mt-16">
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
						<p class="text-xs text-muted-foreground mt-1">Your API key for the AI service. OpenAI keys start with sk-, e.g. sk-xxx...</p>
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
						<p class="text-xs text-muted-foreground mt-1">Base URL for the OpenAI-compatible API, e.g. https://api.openai.com</p>
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
								{#each chatModels as model}
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
						<p class="text-xs text-muted-foreground mt-1">Model for AI conversations, e.g. gpt-4o, deepseek-chat</p>
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
								{#each embeddingModels as model}
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
						<p class="text-xs text-muted-foreground mt-1">Model for text vectorization, e.g. text-embedding-3-small</p>
					</div>

					<!-- Enable AI Toggle -->
					<div class="py-4 border-b border-border/50">
						<div class="flex items-center justify-between gap-4">
							<div class="min-w-0 flex-1">
								<div class="font-medium text-foreground">Enable AI Features</div>
								<div class="text-sm text-muted-foreground">
									{#if !canEnableAI}
										Fill all fields above to enable
									{:else if aiSettings.enabled}
										AI assistant is active. Vector data is automatically built when you save diary entries.
									{:else}
										Enable to use AI assistant. Vector data will be automatically built in the background when you save diary entries.
									{/if}
								</div>
							</div>
							<button
								on:click={() => { if (canEnableAI) aiSettings.enabled = !aiSettings.enabled; }}
								disabled={!canEnableAI && !aiSettings.enabled}
								class="relative inline-flex h-6 w-11 flex-shrink-0 items-center rounded-full transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 {aiSettings.enabled ? 'bg-primary' : 'bg-muted'} {!canEnableAI && !aiSettings.enabled ? 'opacity-50 cursor-not-allowed' : ''}"
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
					<div class="pt-4 flex items-center gap-3">
						<button
							on:click={handleSaveAISettings}
							disabled={aiSaving || !aiSettingsChanged}
							class="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
						>
							{#if aiSaving}
								<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
								</svg>
								Saving...
							{:else}
								Save AI Settings
							{/if}
						</button>
						{#if aiSuccess}
							<span class="text-sm text-green-600 flex items-center gap-1 animate-fade-in">
								<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
								</svg>
								Saved
							</span>
						{/if}
					</div>
				</div>

				<!-- Image Upload (Chevereto) Section -->
				<div id="image-upload" class="bg-card rounded-xl shadow-sm border border-border/50 p-6 animate-fade-in scroll-mt-16">
					<h2 class="text-lg font-semibold text-foreground mb-4">Image Upload</h2>
					<p class="text-sm text-muted-foreground mb-6">
						Configure Chevereto as an external image hosting backend. When enabled, images inserted into diary entries will be uploaded to your Chevereto instance instead of PocketBase.
					</p>

					{#if cheveretoError}
						<div class="mb-4 p-3 bg-destructive/10 text-destructive rounded-lg text-sm">
							{cheveretoError}
						</div>
					{/if}

					{#if cheveretoSuccess}
						<div class="mb-4 p-3 bg-green-500/10 text-green-600 rounded-lg text-sm">
							{cheveretoSuccess}
						</div>
					{/if}

					<!-- Domain -->
					<div class="py-4 border-b border-border/50">
						<label class="block font-medium text-foreground mb-2">Domain</label>
						<input
							type="text"
							bind:value={cheveretoSettingsLocal.domain}
							placeholder="https://img.example.com"
							class="w-full px-3 py-2 bg-muted rounded-lg text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary"
						/>
						<p class="text-xs text-muted-foreground mt-1">Your Chevereto instance URL, e.g. https://img.example.com</p>
					</div>

					<!-- API Key -->
					<div class="py-4 border-b border-border/50">
						<label class="block font-medium text-foreground mb-2">API Key</label>
						<input
							type="password"
							bind:value={cheveretoSettingsLocal.api_key}
							placeholder="chv-key-..."
							class="w-full px-3 py-2 bg-muted rounded-lg text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary"
						/>
						<p class="text-xs text-muted-foreground mt-1">Chevereto API v1 key for authentication</p>
					</div>

					<!-- Album ID -->
					<div class="py-4 border-b border-border/50">
						<label class="block font-medium text-foreground mb-2">Album ID (optional)</label>
						<input
							type="text"
							bind:value={cheveretoSettingsLocal.album_id}
							placeholder=""
							class="w-full px-3 py-2 bg-muted rounded-lg text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary"
						/>
						<p class="text-xs text-muted-foreground mt-1">Optional album ID to organize uploaded images</p>
					</div>

					<!-- Test Connection -->
					<div class="py-4 border-b border-border/50">
						<div class="flex items-center justify-between">
							<div>
								<div class="font-medium text-foreground">Test Connection</div>
								<div class="text-sm text-muted-foreground">Verify your Chevereto server is reachable</div>
							</div>
							<button
								on:click={handleTestChevereto}
								disabled={cheveretoTesting || !cheveretoSettingsLocal.domain || !cheveretoSettingsLocal.api_key}
								class="px-4 py-2 text-sm bg-muted hover:bg-muted/80 rounded-lg transition-colors duration-200 disabled:opacity-50 flex items-center gap-2"
							>
								{#if cheveretoTesting}
									<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
										<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
										<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
									</svg>
									Testing...
								{:else}
									Test
								{/if}
							</button>
						</div>
						{#if cheveretoTestResult}
							<div class="mt-3 p-3 rounded-lg text-sm {cheveretoTestResult.success ? 'bg-green-500/10 text-green-600' : 'bg-destructive/10 text-destructive'}">
								{cheveretoTestResult.message}
							</div>
						{/if}
					</div>

					<!-- Enable Toggle -->
					<div class="py-4 border-b border-border/50">
						<div class="flex items-center justify-between gap-4">
							<div class="min-w-0 flex-1">
								<div class="font-medium text-foreground">Enable Chevereto</div>
								<div class="text-sm text-muted-foreground">
									{#if !canEnableChevereto}
										Fill Domain and API Key above to enable
									{:else if cheveretoSettingsLocal.enabled}
										Images will be uploaded to Chevereto
									{:else}
										Enable to use Chevereto for image hosting
									{/if}
								</div>
							</div>
							<button
								on:click={() => { if (canEnableChevereto) cheveretoSettingsLocal.enabled = !cheveretoSettingsLocal.enabled; }}
								disabled={!canEnableChevereto && !cheveretoSettingsLocal.enabled}
								class="relative inline-flex h-6 w-11 flex-shrink-0 items-center rounded-full transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 {cheveretoSettingsLocal.enabled ? 'bg-primary' : 'bg-muted'} {!canEnableChevereto && !cheveretoSettingsLocal.enabled ? 'opacity-50 cursor-not-allowed' : ''}"
							>
								<span
									class="inline-block h-4 w-4 transform rounded-full bg-white transition-transform duration-200 {cheveretoSettingsLocal.enabled ? 'translate-x-6' : 'translate-x-1'}"
								/>
							</button>
						</div>
						<p class="text-xs text-muted-foreground mt-3">
							Note: When Chevereto is enabled, images will be uploaded directly to your Chevereto instance and managed there. Image links in diary entries will point to Chevereto URLs. The built-in media library will no longer track these images, and they will not be included in data exports.
						</p>
					</div>

					<!-- Save Button -->
					<div class="pt-4 flex items-center gap-3">
						<button
							on:click={handleSaveCheveretoSettings}
							disabled={cheveretoSaving || !cheveretoSettingsChanged}
							class="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
						>
							{#if cheveretoSaving}
								<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
								</svg>
								Saving...
							{:else}
								Save Image Upload Settings
							{/if}
						</button>
						{#if cheveretoSuccess}
							<span class="text-sm text-green-600 flex items-center gap-1 animate-fade-in">
								<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
								</svg>
								Saved
							</span>
						{/if}
					</div>
				</div>

				<!-- Sync & Cache Section -->
				<div id="sync-cache" class="bg-card rounded-xl shadow-sm border border-border/50 p-6 animate-fade-in scroll-mt-16">
					<h2 class="text-lg font-semibold text-foreground mb-4">Sync & Cache</h2>
					<p class="text-sm text-muted-foreground mb-6">
						Manage offline sync and local cache settings. Your diary entries are automatically saved locally and synced when online.
					</p>
					<SyncSettings />
				</div>

				<!-- Data Management Section -->
				<div id="data-management" class="bg-card rounded-xl shadow-sm border border-border/50 p-6 animate-fade-in scroll-mt-16">
					<h2 class="text-lg font-semibold text-foreground mb-4">Data Management</h2>
					<p class="text-sm text-muted-foreground mb-6">
						Import and export your diary data. To avoid large export files, you can export data in segments by date range.
					</p>

					<!-- Export -->
					<div class="py-4 border-b border-border/50">
						<div class="flex items-center justify-between mb-1">
							<div class="font-medium text-foreground">Export</div>
							<button
								on:click={() => showExportOptions = !showExportOptions}
								class="text-xs text-primary hover:underline"
							>
								{showExportOptions ? 'Hide Options' : 'Show Options'}
							</button>
						</div>
						<div class="text-sm text-muted-foreground mb-3">Download your diary data as a ZIP file</div>

						{#if showExportOptions}
							<div class="mb-4 p-4 bg-muted/50 rounded-lg space-y-4">
								<div class="text-xs text-amber-600 bg-amber-500/10 p-2 rounded">
									To avoid large export files, consider exporting data in segments by selecting a specific date range.
								</div>

								<!-- Date Range -->
								<div>
									<label class="block text-sm font-medium text-foreground mb-2">Date Range</label>
									<select
										bind:value={exportOptions.date_range}
										class="w-full px-3 py-2 bg-background rounded-lg text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-primary border border-border/50"
									>
										<option value="1m">Past 1 month</option>
										<option value="3m">Past 3 months</option>
										<option value="6m">Past 6 months</option>
										<option value="1y">Past 1 year</option>
										<option value="all">All time</option>
										<option value="custom">Custom range</option>
									</select>
								</div>

								{#if exportOptions.date_range === 'custom'}
									<div class="grid grid-cols-2 gap-3">
										<div>
											<label class="block text-xs text-muted-foreground mb-1">Start Date</label>
											<input
												type="date"
												bind:value={customStartDate}
												class="w-full px-3 py-2 bg-background rounded-lg text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-primary border border-border/50"
											/>
										</div>
										<div>
											<label class="block text-xs text-muted-foreground mb-1">End Date</label>
											<input
												type="date"
												bind:value={customEndDate}
												class="w-full px-3 py-2 bg-background rounded-lg text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-primary border border-border/50"
											/>
										</div>
									</div>
								{/if}

								<!-- Content Types -->
								<div>
									<label class="block text-sm font-medium text-foreground mb-2">Content to Export</label>
									<div class="space-y-2">
										<label class="flex items-center gap-2 cursor-pointer">
											<input type="checkbox" bind:checked={exportOptions.include_diaries} class="rounded" />
											<span class="text-sm text-foreground">Diaries</span>
										</label>
										<label class="flex items-center gap-2 cursor-pointer">
											<input type="checkbox" bind:checked={exportOptions.include_media} class="rounded" />
											<span class="text-sm text-foreground">Media files</span>
										</label>
										<label class="flex items-center gap-2 cursor-pointer">
											<input type="checkbox" bind:checked={exportOptions.include_conversations} class="rounded" />
											<span class="text-sm text-foreground">AI conversations</span>
										</label>
									</div>
								</div>
							</div>
						{/if}

						{#if exportError}
							<div class="mb-3 p-3 bg-destructive/10 text-destructive rounded-lg text-sm">
								{exportError}
							</div>
						{/if}

						<button
							on:click={handleExport}
							disabled={exporting}
							class="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors duration-200 disabled:opacity-50 flex items-center gap-2"
						>
							{#if exporting}
								<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
								</svg>
								Exporting...
							{:else}
								<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
								</svg>
								Export Data
							{/if}
						</button>

						{#if exportStats}
							<div class="mt-3 p-3 bg-muted rounded-lg text-sm">
								<div class="font-medium text-foreground mb-2">Export Complete</div>
								<div class="text-xs text-muted-foreground mb-2">
									Period: {exportStats.start_date} to {exportStats.end_date}
								</div>
								<div class="space-y-2 text-muted-foreground">
									<div class="flex justify-between">
										<span>Diaries:</span>
										<span>
											<span class="text-foreground font-medium">{exportStats.diaries.actual_exported}</span>
											<span class="text-xs">/ {exportStats.diaries.should_export} selected / {exportStats.diaries.total_in_system} total</span>
										</span>
									</div>
									<div class="flex justify-between">
										<span>Media:</span>
										<span>
											<span class="text-foreground font-medium">{exportStats.media.actual_exported}</span>
											<span class="text-xs">/ {exportStats.media.should_export} selected / {exportStats.media.total_in_system} total</span>
										</span>
									</div>
									<div class="flex justify-between">
										<span>Conversations:</span>
										<span>
											<span class="text-foreground font-medium">{exportStats.conversations.actual_exported}</span>
											<span class="text-xs">/ {exportStats.conversations.should_export} selected / {exportStats.conversations.total_in_system} total</span>
											<span class="text-xs">({exportStats.messages} messages)</span>
										</span>
									</div>
								</div>
								{#if exportStats.failed_items && exportStats.failed_items.length > 0}
									<div class="mt-3 pt-2 border-t border-border/50">
										<div class="font-medium text-destructive mb-1">Failed Items:</div>
										<div class="text-xs space-y-1 max-h-24 overflow-y-auto">
											{#each exportStats.failed_items as item}
												<div class="text-muted-foreground">
													<span class="text-destructive">[{item.type}]</span> {item.id}: {item.reason}
												</div>
											{/each}
										</div>
									</div>
								{/if}
							</div>
						{/if}
					</div>

					<!-- Import -->
					<div class="py-4">
						<div class="font-medium text-foreground mb-1">Import</div>
						<div class="text-sm text-muted-foreground mb-3">Restore diary data from a previously exported ZIP file. Diaries with an existing date will be skipped.</div>

						{#if importError}
							<div class="mb-3 p-3 bg-destructive/10 text-destructive rounded-lg text-sm">
								{importError}
							</div>
						{/if}

						<div class="flex items-center gap-3 flex-wrap">
							<label class="px-4 py-2 text-sm bg-muted hover:bg-muted/80 rounded-lg transition-colors duration-200 cursor-pointer">
								<span>{importFile ? importFile.name : 'Choose File'}</span>
								<input
									type="file"
									accept=".zip"
									class="hidden"
									on:change={handleImportFileChange}
								/>
							</label>
							<button
								on:click={handleImport}
								disabled={importing || !importFile}
								class="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors duration-200 disabled:opacity-50 flex items-center gap-2"
							>
								{#if importing}
									<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
										<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
										<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
									</svg>
									Importing...
								{:else}
									<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0l-4 4m4-4v12" />
									</svg>
									Import
								{/if}
							</button>
						</div>

						{#if importStats}
							<div class="mt-3 p-3 bg-muted rounded-lg text-sm">
								<div class="font-medium text-foreground mb-2">Import Complete</div>
								<div class="space-y-1 text-muted-foreground">
									<div>
										Diaries:
										<span class="text-green-600 font-medium">{importStats.diaries.imported} imported</span>
										{#if importStats.diaries.skipped > 0}
											, <span class="text-amber-600 font-medium">{importStats.diaries.skipped} skipped</span>
										{/if}
										{#if importStats.diaries.failed > 0}
											, <span class="text-destructive font-medium">{importStats.diaries.failed} failed</span>
										{/if}
										<span class="text-muted-foreground">({importStats.diaries.total} total)</span>
									</div>
									<div>
										Media:
										<span class="text-green-600 font-medium">{importStats.media.imported} imported</span>
										{#if importStats.media.skipped > 0}
											, <span class="text-amber-600 font-medium">{importStats.media.skipped} skipped</span>
										{/if}
										{#if importStats.media.failed > 0}
											, <span class="text-destructive font-medium">{importStats.media.failed} failed</span>
										{/if}
										<span class="text-muted-foreground">({importStats.media.total} total)</span>
									</div>
									<div>
										AI conversations:
										<span class="text-green-600 font-medium">{importStats.conversations.imported} imported</span>
										{#if importStats.conversations.skipped > 0}
											, <span class="text-orange-500 font-medium">{importStats.conversations.skipped} skipped</span>
										{/if}
										{#if importStats.conversations.failed > 0}
											, <span class="text-destructive font-medium">{importStats.conversations.failed} failed</span>
										{/if}
										<span class="text-muted-foreground">({importStats.conversations.total} total)</span>
									</div>
								</div>
							</div>
						{/if}
					</div>
				</div>
			</div>
		{/if}
	</main>

			<!-- Desktop TOC Sidebar -->
			{#if showDesktopToc}
				<aside class="hidden lg:block w-56 flex-shrink-0">
					<div class="sticky top-16 animate-slide-in-right">
						<div class="bg-card/50 rounded-xl border border-border/50 p-4">
							<SettingsToc />
						</div>
					</div>
				</aside>
			{/if}
		</div>
	</div>

	<Footer maxWidth="6xl" tagline="Manage your settings" />
</div>

<!-- Mobile Drawer -->
{#if showMobileToc}
	<!-- Backdrop -->
	<button
		class="fixed inset-0 bg-black/40 backdrop-blur-sm z-40 lg:hidden"
		on:click={() => showMobileToc = false}
		aria-label="Close menu"
	></button>

	<!-- Drawer Panel -->
	<div class="fixed inset-y-0 left-0 w-72 bg-card border-r border-border shadow-2xl z-50 lg:hidden animate-slide-in-left">
		<!-- Drawer Header -->
		<div class="flex items-center justify-between px-5 py-4 border-b border-border/50">
			<div class="flex items-center gap-2">
				<img src="/logo.png" alt="Diarum" class="w-6 h-6" />
				<span class="font-semibold text-foreground">Settings</span>
			</div>
			<button
				on:click={() => showMobileToc = false}
				class="p-2 hover:bg-muted rounded-lg transition-colors"
				aria-label="Close"
			>
				<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
				</svg>
			</button>
		</div>

		<!-- Drawer Content -->
		<div class="flex flex-col h-[calc(100%-57px)]">
			<!-- Actions Section -->
			<div class="px-3 py-3">
				<div class="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider mb-2 px-1">
					Quick Actions
				</div>
				<div class="space-y-0.5">
					<a
						href="/diary"
						class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
						on:click={() => showMobileToc = false}
					>
						<div class="p-1.5 rounded-md bg-green-500/10 text-green-500 group-hover:bg-green-500/20 transition-colors">
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
									d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
							</svg>
						</div>
						<div class="min-w-0">
							<div class="text-xs font-medium text-foreground">Calendar</div>
							<div class="text-[10px] text-muted-foreground truncate">View all diary entries</div>
						</div>
					</a>

					<a
						href="/assistant"
						class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
						on:click={() => showMobileToc = false}
					>
						<div class="p-1.5 rounded-md bg-primary/10 text-primary group-hover:bg-primary/20 transition-colors">
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<rect x="4" y="6" width="16" height="12" rx="2" stroke-width="2"/>
								<circle cx="9" cy="11" r="1.5" fill="currentColor"/>
								<circle cx="15" cy="11" r="1.5" fill="currentColor"/>
							</svg>
						</div>
						<div class="min-w-0">
							<div class="text-xs font-medium text-foreground">AI Assistant</div>
							<div class="text-[10px] text-muted-foreground truncate">Chat with AI about your diary</div>
						</div>
					</a>

					<a
						href="/search"
						class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
						on:click={() => showMobileToc = false}
					>
						<div class="p-1.5 rounded-md bg-blue-500/10 text-blue-500 group-hover:bg-blue-500/20 transition-colors">
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
							</svg>
						</div>
						<div class="min-w-0">
							<div class="text-xs font-medium text-foreground">Search</div>
							<div class="text-[10px] text-muted-foreground truncate">Find diary entries</div>
						</div>
					</a>

					<a
						href="/media"
						class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-muted/70 transition-all duration-200 group"
						on:click={() => showMobileToc = false}
					>
						<div class="p-1.5 rounded-md bg-purple-500/10 text-purple-500 group-hover:bg-purple-500/20 transition-colors">
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
							</svg>
						</div>
						<div class="min-w-0">
							<div class="text-xs font-medium text-foreground">Media</div>
							<div class="text-[10px] text-muted-foreground truncate">Browse photos & files</div>
						</div>
					</a>
				</div>
			</div>

			<!-- Divider -->
			<div class="mx-3 border-t border-border/50"></div>

			<!-- TOC Section -->
			<div class="flex-1 overflow-y-auto px-3 py-3">
				<SettingsToc onNavigate={() => showMobileToc = false} />
			</div>
		</div>
	</div>
{/if}
