import { pb } from './client';

export interface CheveretoSettings {
	enabled: boolean;
	domain: string;
	api_key: string;
	album_id: string;
}

export async function getCheveretoSettings(): Promise<CheveretoSettings> {
	try {
		const response = await fetch('/api/chevereto/settings', {
			headers: {
				'Authorization': `Bearer ${pb.authStore.token}`
			}
		});

		if (!response.ok) {
			throw new Error('Failed to get Chevereto settings');
		}

		return await response.json();
	} catch (error) {
		console.error('Error fetching Chevereto settings:', error);
		return {
			enabled: false,
			domain: '',
			api_key: '',
			album_id: ''
		};
	}
}

export async function saveCheveretoSettings(settings: CheveretoSettings): Promise<{ success: boolean }> {
	const response = await fetch('/api/chevereto/settings', {
		method: 'PUT',
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify(settings)
	});

	if (!response.ok) {
		const data = await response.json();
		throw new Error(data.message || 'Failed to save Chevereto settings');
	}

	return await response.json();
}

export async function testCheveretoConnection(domain: string, apiKey: string): Promise<{ success: boolean; message: string }> {
	const response = await fetch('/api/chevereto/test', {
		method: 'POST',
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ domain, api_key: apiKey })
	});

	if (!response.ok) {
		const data = await response.json();
		throw new Error(data.message || 'Failed to test connection');
	}

	return await response.json();
}

export async function uploadToChevereto(file: File): Promise<{ url: string }> {
	const formData = new FormData();
	formData.append('source', file);

	const response = await fetch('/api/chevereto/upload', {
		method: 'POST',
		headers: {
			'Authorization': `Bearer ${pb.authStore.token}`
		},
		body: formData
	});

	if (!response.ok) {
		const data = await response.json().catch(() => ({}));
		throw new Error(data?.message || 'Upload to Chevereto failed');
	}

	const data = await response.json();
	if (!data?.url) {
		throw new Error('No image URL in Chevereto response');
	}

	return { url: data.url };
}
