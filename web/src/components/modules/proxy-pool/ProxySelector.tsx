'use client';

import { useTranslations } from 'next-intl';
import type { ProxyMode } from '@/api/endpoints/proxy-pool';
import { useProxyConfigurationList, useTestProxyConfiguration } from '@/api/endpoints/proxy-pool';
import { ExternalLink, Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/common/Toast';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import { useProxyPoolDialogStore } from './dialog-store';

type ProxyValue = {
    proxy_mode: ProxyMode;
    proxy_config_id?: number | null;
    proxy_url?: string;
};

const CUSTOM_PROXY_TEST_URL = 'https://api.openai.com/v1/models';

// 与后端 model.NormalizeProxyURL 保持一致：只接受 http/https/socks/socks5 且必须带主机。
export function isValidProxyURL(value: string) {
    try {
        const parsed = new URL(value.trim());
        return ['http:', 'https:', 'socks:', 'socks5:'].includes(parsed.protocol.toLowerCase()) && parsed.hostname !== '';
    } catch {
        return false;
    }
}

// 返回自定义代理地址的校验错误对应的翻译 key，非自定义模式或地址合法时返回 null。
export function customProxyURLError(value: ProxyValue): 'custom.required' | 'custom.invalid' | null {
    if (value.proxy_mode !== 'custom') return null;
    const url = value.proxy_url?.trim() ?? '';
    if (!url) return 'custom.required';
    return isValidProxyURL(url) ? null : 'custom.invalid';
}

type ProxySelectorProps = {
    value: ProxyValue;
    onChange: (value: ProxyValue) => void;
    allowInherit?: boolean;
    allowCustom?: boolean;
    disabled?: boolean;
    className?: string;
};

export function ProxySelector({ value, onChange, allowInherit = false, allowCustom = false, disabled = false, className }: ProxySelectorProps) {
    const t = useTranslations('proxyPool');
    const { data: proxies = [], isLoading } = useProxyConfigurationList();
    const testProxy = useTestProxyConfiguration();
    const openProxyPool = useProxyPoolDialogStore((state) => state.open);
    const selectedProxy = proxies.find((item) => item.id === value.proxy_config_id) ?? null;
    const enabledProxies = proxies.filter((item) => item.enabled || item.id === value.proxy_config_id);
    const mode = value.proxy_mode || (allowInherit ? 'inherit' : 'direct');

    const modes: ProxyMode[] = [
        ...(allowInherit ? (['inherit'] as ProxyMode[]) : []),
        'direct',
        'system',
        'pool',
        ...(allowCustom ? (['custom'] as ProxyMode[]) : []),
    ];
    const customURL = value.proxy_url ?? '';
    const customError = customURL.trim() ? customProxyURLError({ ...value, proxy_mode: mode }) : null;

    function testCustomProxy() {
        testProxy.mutate(
            { proxy_url: customURL.trim(), url: CUSTOM_PROXY_TEST_URL },
            {
                onSuccess: (result) => {
                    if (result.success) {
                        toast.success(t('dialog.testSuccess', { statusCode: result.status_code, durationMs: result.duration_ms }));
                    } else {
                        toast.error(t('dialog.testFailed'), { description: result.message });
                    }
                },
                onError: (err) => toast.error(err instanceof Error ? err.message : t('dialog.operationFailed')),
            }
        );
    }
    return (
        <div className={className}>
            <div className="grid gap-2 md:grid-cols-8">
                <div className={allowInherit ? 'space-y-2 md:col-span-3' : 'space-y-2 md:col-span-2'}>
                    <label className="text-sm font-medium text-card-foreground">{t('mode.label')}</label>
                    <Select
                        value={mode}
                        disabled={disabled}
                        onValueChange={(nextMode) => {
                            const proxy_mode = nextMode as ProxyMode;
                            onChange({
                                proxy_mode,
                                proxy_config_id: proxy_mode === 'pool' ? value.proxy_config_id ?? null : null,
                                proxy_url: proxy_mode === 'custom' ? customURL : '',
                            });
                        }}
                    >
                        <SelectTrigger className="w-full rounded-xl">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent className="rounded-xl">
                            {modes.map((item) => (
                                <SelectItem key={item} className="rounded-xl" value={item}>
                                    {t(`mode.${item}`)}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </div>

                {mode === 'pool' ? (
                    <div className={allowInherit ? 'space-y-2 md:col-span-5' : 'space-y-2 md:col-span-6'}>
                        <label className="text-sm font-medium text-card-foreground">{t('name')}</label>
                        <div className="flex items-center gap-2">
                            {enabledProxies.length > 0 ? (
                                <Select
                                    value={value.proxy_config_id ? String(value.proxy_config_id) : ''}
                                    disabled={disabled || isLoading}
                                    onValueChange={(proxyId) => onChange({ proxy_mode: 'pool', proxy_config_id: Number(proxyId), proxy_url: '' })}
                                >
                                    <SelectTrigger className="min-w-0 flex-1 rounded-xl">
                                        <SelectValue placeholder={t('selectConfig')} />
                                    </SelectTrigger>
                                    <SelectContent className="rounded-xl">
                                        {enabledProxies.map((proxy) => (
                                            <SelectItem key={proxy.id} className="rounded-xl" value={String(proxy.id)} disabled={!proxy.enabled}>
                                                {proxy.name}{!proxy.enabled ? t('disabledSuffix') : ''}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            ) : (
                                <div className="min-w-0 flex-1 truncate rounded-xl border border-border/70 bg-muted/20 px-3 py-2 text-sm text-muted-foreground">
                                    {proxies.length === 0 ? t('empty') : t('noEnabled')}
                                </div>
                            )}
                            <Tooltip side="top">
                                <TooltipTrigger asChild>
                                    <Button
                                        type="button"
                                        size="icon-sm"
                                        variant="ghost"
                                        className="shrink-0 rounded-xl text-muted-foreground hover:text-foreground"
                                        onClick={() => openProxyPool(value.proxy_config_id ?? null)}
                                        aria-label={t('manage')}
                                    >
                                        <ExternalLink className="size-4" />
                                    </Button>
                                </TooltipTrigger>
                                <TooltipContent>{t('manage')}</TooltipContent>
                            </Tooltip>
                        </div>
                        {selectedProxy && !selectedProxy.enabled ? (
                            <div className="rounded-xl border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
                                {t('disabledSelected')}
                            </div>
                        ) : null}
                    </div>
                ) : null}

                {mode === 'custom' ? (
                    <div className={allowInherit ? 'space-y-2 md:col-span-5' : 'space-y-2 md:col-span-6'}>
                        <label className="text-sm font-medium text-card-foreground">{t('custom.url')}</label>
                        <div className="flex items-center gap-2">
                            <Input
                                value={customURL}
                                disabled={disabled}
                                onChange={(event) => onChange({ proxy_mode: 'custom', proxy_config_id: null, proxy_url: event.target.value })}
                                placeholder={t('custom.placeholder')}
                                className={customError ? 'min-w-0 flex-1 rounded-xl border-destructive/50 focus-visible:ring-destructive/30' : 'min-w-0 flex-1 rounded-xl'}
                                autoComplete="off"
                                spellCheck={false}
                            />
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="shrink-0 rounded-xl"
                                disabled={disabled || testProxy.isPending || !customURL.trim() || !!customError}
                                onClick={testCustomProxy}
                            >
                                {testProxy.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
                                {t('dialog.test')}
                            </Button>
                        </div>
                        {customError ? (
                            <p className="text-xs text-destructive">{t(customError)}</p>
                        ) : (
                            <p className="text-xs text-muted-foreground">{t('custom.hint')}</p>
                        )}
                    </div>
                ) : null}
            </div>
        </div>
    );
}
