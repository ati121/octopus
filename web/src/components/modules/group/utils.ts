import type { LLMChannel } from '@/api/endpoints/model';
import { CHANNEL_TYPE_SHORT_LABELS } from '@/api/endpoints/channel';
import { GroupMode } from '@/api/endpoints/group';

export const MODE_LABELS: Record<GroupMode, string> = {
    [GroupMode.RoundRobin]: 'roundRobin',
    [GroupMode.Random]: 'random',
    [GroupMode.Failover]: 'failover',
    [GroupMode.Weighted]: 'weighted',
} as const;

export function normalizeKey(value: string) {
    return value.trim().toLowerCase();
}

export function modelChannelKey(channelId: number, modelName: string) {
    return `${channelId}-${modelName}`;
}

export function memberKey(member: Pick<LLMChannel, 'channel_id' | 'name'>) {
    return modelChannelKey(member.channel_id, member.name);
}

/**
 * 分组成员的来源：站点渠道显示「站点/分组-协议」，不显示账号；其余渠道显示「渠道名 · 协议」。
 * 协议取该模型实际使用的渠道类型，模型单独设置的类型优先。
 */
export function memberSourceLabel(member: LLMChannel) {
    const protocol = CHANNEL_TYPE_SHORT_LABELS[member.channel_type];
    if (member.site_id != null) {
        const siteName = member.site_name?.trim();
        const groupName = member.site_group_name?.trim();
        return siteName && groupName ? `${siteName}/${groupName}-${protocol}` : member.channel_name;
    }
    return [member.channel_name, protocol].filter(Boolean).join(' · ');
}

export function matchesGroupName(modelName: string, groupKey: string) {
    if (!groupKey) return false;
    return modelName.toLowerCase().includes(groupKey);
}

export function buildChannelNameByModelKey(modelChannels: LLMChannel[]) {
    const map = new Map<string, string>();
    modelChannels.forEach((mc) => {
        map.set(modelChannelKey(mc.channel_id, mc.name), mc.channel_name);
    });
    return map;
}


