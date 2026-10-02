import { useQuery } from '@tanstack/react-query';
import { apiClient } from '../client';
import type { ChannelType } from './channel';

// 模型发现与路由使用的渠道关联信息。
export interface LLMChannel {
    name: string;
    enabled: boolean;
    channel_id: number;
    channel_name: string;
    site_id?: number | null;
    site_account_id?: number | null;
    site_group_key?: string;
    site_group_name?: string;
    site_name?: string;
    site_account_name?: string;
    channel_type: ChannelType;
}

export function useModelChannelList() {
    return useQuery({
        queryKey: ['models', 'channel'],
        queryFn: () => apiClient.get<LLMChannel[]>('/api/v1/model/channel'),
        refetchInterval: 30000,
    });
}
