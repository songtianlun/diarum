import { pb } from './client';

export type ImageUploadProvider = 'local' | 's3' | 'chevereto';

export interface ImageUploadSettings {
    provider: ImageUploadProvider;
    local: {
        path: string;
    };
    s3: {
        bucket: string;
        region: string;
        endpoint: string;
        access_key: string;
        secret: string;
        force_path_style: boolean;
    };
    chevereto: {
        domain: string;
        api_key: string;
        album_id: string;
    };
}

export const defaultImageUploadSettings: ImageUploadSettings = {
    provider: 'local',
    local: {
        path: ''
    },
    s3: {
        bucket: '',
        region: '',
        endpoint: '',
        access_key: '',
        secret: '',
        force_path_style: false
    },
    chevereto: {
        domain: '',
        api_key: '',
        album_id: ''
    }
};

export async function getImageUploadSettings(): Promise<ImageUploadSettings> {
    try {
        const response = await fetch('/api/v1/image-upload/settings', {
            headers: {
                'Authorization': `Bearer ${pb.authStore.token}`
            }
        });

        if (!response.ok) {
            throw new Error('Failed to get image upload settings');
        }

        return await response.json();
    } catch (error) {
        console.error('Error fetching image upload settings:', error);
        return structuredClone(defaultImageUploadSettings);
    }
}

export async function saveImageUploadSettings(settings: ImageUploadSettings): Promise<{ success: boolean; settings?: ImageUploadSettings }> {
    const response = await fetch('/api/v1/image-upload/settings', {
        method: 'PUT',
        headers: {
            'Authorization': `Bearer ${pb.authStore.token}`,
            'Content-Type': 'application/json'
        },
        body: JSON.stringify(settings)
    });

    if (!response.ok) {
        const data = await response.json().catch(() => ({}));
        throw new Error(data.message || 'Failed to save image upload settings');
    }

    return await response.json();
}

export async function testCheveretoConnection(domain: string, apiKey: string): Promise<{ success: boolean; message: string }> {
    const response = await fetch('/api/v1/chevereto/test', {
        method: 'POST',
        headers: {
            'Authorization': `Bearer ${pb.authStore.token}`,
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({ domain, api_key: apiKey })
    });

    if (!response.ok) {
        const data = await response.json().catch(() => ({}));
        throw new Error(data.message || 'Failed to test Chevereto connection');
    }

    return await response.json();
}