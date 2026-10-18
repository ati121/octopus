import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Check, X } from 'lucide-react';
import { CHANNEL_TYPE_SHORT_LABELS, ChannelType } from '@/api/endpoints/channel';
import { Badge } from '@/components/ui/badge';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { cn } from '@/lib/utils';

/** 渠道类型选项，渠道类型下拉与模型级类型选择共用 */
export const CHANNEL_TYPE_OPTIONS = [
    { value: ChannelType.OpenAIChat, labelKey: 'typeOpenAIChat' },
    { value: ChannelType.OpenAIResponse, labelKey: 'typeOpenAIResponse' },
    { value: ChannelType.Anthropic, labelKey: 'typeAnthropic' },
    { value: ChannelType.Gemini, labelKey: 'typeGemini' },
    { value: ChannelType.Volcengine, labelKey: 'typeVolcengine' },
    { value: ChannelType.OpenAIEmbedding, labelKey: 'typeOpenAIEmbedding' },
    { value: ChannelType.Codex, labelKey: 'typeCodex' },
    { value: ChannelType.Rerank, labelKey: 'typeRerank' },
    { value: ChannelType.SystemOne, labelKey: 'typeSystemOne' },
    { value: ChannelType.Decisions, labelKey: 'typeDecisions' },
] as const;

function channelTypeLabelKey(type: ChannelType) {
    return CHANNEL_TYPE_OPTIONS.find((option) => option.value === type)?.labelKey ?? 'typeOpenAIChat';
}

/**
 * 模型单独设置渠道类型后的强调色：外圈边框、底色和协议短名使用同一色相，
 * 便于模型较多时一眼分辨哪些模型覆盖了渠道类型、各自走什么协议。
 */
const CHANNEL_TYPE_ACCENTS: Record<ChannelType, { border: string; tint: string; text: string }> = {
    [ChannelType.OpenAIChat]: {
        border: 'border-sky-500/70',
        tint: 'bg-sky-500/10',
        text: 'text-sky-700 dark:text-sky-300',
    },
    [ChannelType.OpenAIResponse]: {
        border: 'border-violet-500/70',
        tint: 'bg-violet-500/10',
        text: 'text-violet-700 dark:text-violet-300',
    },
    [ChannelType.Anthropic]: {
        border: 'border-orange-500/70',
        tint: 'bg-orange-500/10',
        text: 'text-orange-700 dark:text-orange-300',
    },
    [ChannelType.Gemini]: {
        border: 'border-indigo-500/70',
        tint: 'bg-indigo-500/10',
        text: 'text-indigo-700 dark:text-indigo-300',
    },
    [ChannelType.Volcengine]: {
        border: 'border-rose-500/70',
        tint: 'bg-rose-500/10',
        text: 'text-rose-700 dark:text-rose-300',
    },
    [ChannelType.OpenAIEmbedding]: {
        border: 'border-teal-500/70',
        tint: 'bg-teal-500/10',
        text: 'text-teal-700 dark:text-teal-300',
    },
    [ChannelType.Codex]: {
        border: 'border-fuchsia-500/70',
        tint: 'bg-fuchsia-500/10',
        text: 'text-fuchsia-700 dark:text-fuchsia-300',
    },
    [ChannelType.Rerank]: {
        border: 'border-lime-600/70',
        tint: 'bg-lime-500/10',
        text: 'text-lime-700 dark:text-lime-300',
    },
    [ChannelType.SystemOne]: {
        border: 'border-amber-500/70',
        tint: 'bg-amber-500/10',
        text: 'text-amber-700 dark:text-amber-300',
    },
    [ChannelType.Decisions]: {
        border: 'border-pink-500/70',
        tint: 'bg-pink-500/10',
        text: 'text-pink-700 dark:text-pink-300',
    },
};

interface ModelBadgeProps {
    model: string;
    custom: boolean;
    channelType: ChannelType;
    modelType?: ChannelType;
    onModelTypeChange: (type: ChannelType | undefined) => void;
    onRemove: () => void;
}

/**
 * 已选模型徽标：点击模型名可单独指定渠道类型，优先于渠道本身的类型；
 * 选「跟随渠道类型」则清除单独设置。
 * 设置了单独类型的模型用该协议的强调色描边，未设置的保持中性的自动/自定义样式。
 */
export function ModelBadge({ model, custom, channelType, modelType, onModelTypeChange, onRemove }: ModelBadgeProps) {
    const t = useTranslations('channel.form');
    const [open, setOpen] = useState(false);

    const handleSelect = (type: ChannelType | undefined) => {
        onModelTypeChange(type);
        setOpen(false);
    };

    const accent = modelType === undefined ? undefined : CHANNEL_TYPE_ACCENTS[modelType];

    return (
        <Badge
            variant={custom ? 'default' : 'secondary'}
            className={cn(
                'gap-1',
                custom ? 'bg-primary text-primary-foreground hover:bg-primary/90' : 'bg-muted hover:bg-muted/80',
                accent && 'border-2',
                accent?.border,
                accent && !custom && [accent.tint, accent.text],
            )}
            title={modelType === undefined ? undefined : t(channelTypeLabelKey(modelType))}
        >
            <Popover open={open} onOpenChange={setOpen}>
                <PopoverTrigger asChild>
                    <button
                        type="button"
                        className="inline-flex items-center gap-1 rounded-sm cursor-pointer hover:underline decoration-dotted underline-offset-2 focus:outline-none focus:ring-1 focus:ring-ring"
                    >
                        {model}
                        {modelType !== undefined && accent && (
                            <span
                                className={cn(
                                    'rounded-full bg-background/85 px-1.5 text-[10px] leading-4 font-semibold',
                                    accent.text,
                                )}
                            >
                                {CHANNEL_TYPE_SHORT_LABELS[modelType]}
                            </span>
                        )}
                    </button>
                </PopoverTrigger>
                <PopoverContent align="start" className="w-56 rounded-2xl border border-border/60 bg-card p-1.5 shadow-xl">
                    <p className="truncate px-2 pt-1 pb-1.5 text-xs font-medium text-muted-foreground" title={model}>
                        {model}
                    </p>
                    <TypeOption
                        selected={modelType === undefined}
                        onSelect={() => handleSelect(undefined)}
                        description={t(channelTypeLabelKey(channelType))}
                    >
                        {t('modelTypeFollow')}
                    </TypeOption>
                    <div className="my-1 h-px bg-border" />
                    {CHANNEL_TYPE_OPTIONS.map((option) => (
                        <TypeOption
                            key={option.value}
                            selected={modelType === option.value}
                            onSelect={() => handleSelect(option.value)}
                        >
                            {t(option.labelKey)}
                        </TypeOption>
                    ))}
                </PopoverContent>
            </Popover>
            <button
                type="button"
                onClick={onRemove}
                className="ml-1 rounded-sm opacity-70 hover:opacity-100 focus:outline-none focus:ring-1 focus:ring-ring"
            >
                <X className="h-3 w-3" />
            </button>
        </Badge>
    );
}

interface TypeOptionProps {
    selected: boolean;
    onSelect: () => void;
    /** 选项下方的补充说明（单独一行，避免与主文案挤在一行被截断） */
    description?: React.ReactNode;
    children: React.ReactNode;
}

function TypeOption({ selected, onSelect, description, children }: TypeOptionProps) {
    return (
        <button
            type="button"
            onClick={onSelect}
            className={cn(
                'flex w-full items-center justify-between gap-2 rounded-xl px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted',
                selected && 'font-medium',
            )}
        >
            <span className="flex min-w-0 flex-col">
                <span className="truncate">{children}</span>
                {description && (
                    <span className="truncate text-xs font-normal text-muted-foreground">{description}</span>
                )}
            </span>
            {selected && <Check className="size-4 shrink-0" />}
        </button>
    );
}
