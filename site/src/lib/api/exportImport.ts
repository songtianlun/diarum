import { pb } from './client';

// Export request options
export interface ExportOptions {
	date_range: '1m' | '3m' | '6m' | '1y' | 'all' | 'custom';
	start_date?: string;
	end_date?: string;
	include_diaries: boolean;
	include_media: boolean;
	include_conversations: boolean;
}

// Export count detail for each data type
export interface ExportCountDetail {
	total_in_system: number;
	should_export: number;
	actual_exported: number;
}

// Failed export item
export interface ExportFailedItem {
	type: string;
	id: string;
	reason: string;
}

export interface ExportStats {
	date_range_type: string;
	start_date: string;
	end_date: string;
	diaries: ExportCountDetail;
	media: ExportCountDetail;
	conversations: ExportCountDetail;
	messages: number;
	failed_items?: ExportFailedItem[];
}

export interface ImportCounters {
	total: number;
	imported: number;
	skipped: number;
	failed: number;
}

export interface ImportStats {
	diaries: ImportCounters;
	media: ImportCounters;
	conversations: ImportCounters;
}

/**
 * Export diary data as a ZIP file with optional filters.
 * Triggers a browser download and returns export stats from the response header.
 */
export async function exportDiaries(options?: ExportOptions): Promise<ExportStats> {
	// Default options
	const exportOptions: ExportOptions = options || {
		date_range: '3m',
		include_diaries: true,
		include_media: true,
		include_conversations: true
	};

	const response = await fetch('/api/export', {
		method: 'POST',
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify(exportOptions)
	});

	if (!response.ok) {
		const text = await response.text();
		throw new Error(text || 'Export failed');
	}

	// Parse stats header
	const statsRaw = response.headers.get('X-Export-Stats');
	const defaultStats: ExportStats = {
		date_range_type: exportOptions.date_range,
		start_date: '',
		end_date: '',
		diaries: { total_in_system: 0, should_export: 0, actual_exported: 0 },
		media: { total_in_system: 0, should_export: 0, actual_exported: 0 },
		conversations: { total_in_system: 0, should_export: 0, actual_exported: 0 },
		messages: 0
	};
	const stats: ExportStats = statsRaw ? JSON.parse(statsRaw) : defaultStats;

	// Trigger browser download
	const blob = await response.blob();
	const url = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = url;

	// Generate filename with timestamp suffix
	const now = new Date();
	const timestamp = now.getFullYear().toString() +
		(now.getMonth() + 1).toString().padStart(2, '0') +
		now.getDate().toString().padStart(2, '0') +
		now.getHours().toString().padStart(2, '0') +
		now.getMinutes().toString().padStart(2, '0') +
		now.getSeconds().toString().padStart(2, '0');
	a.download = `diarum_export_${timestamp}.zip`;

	document.body.appendChild(a);
	a.click();
	document.body.removeChild(a);
	URL.revokeObjectURL(url);

	return stats;
}

/**
 * Import diary data from a previously exported ZIP file.
 */
export async function importDiaries(file: File): Promise<ImportStats> {
	const formData = new FormData();
	formData.append('file', file);

	const response = await fetch('/api/import', {
		method: 'POST',
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`
		},
		body: formData
	});

	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new Error((data as any).message || 'Import failed');
	}

	return await response.json();
}
