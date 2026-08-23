import {
    OpenAI,
    Alibaba,
    BAAI,
    Claude,
    Gemini,
    DeepSeek,
    Mistral,
    Qwen,
    Meta,
    Ollama,
    Groq,
    Cohere,
    Perplexity,
    Zhipu,
    Yi,
    Kimi,
    Minimax,
    Doubao,
    Hunyuan,
    Spark,
    Wenxin,
    Nvidia,
    Azure,
    Aws,
    Together,
    Fireworks,
    Replicate,
    HuggingFace,
    Grok,
    Google,
    Cerebras,
    SambaNova,
    Cloudflare,
    OpenRouter,
    Volcengine,
    SiliconCloud,
    Novita,
    InternLM,
    Stepfun,
    Gemma,
    Microsoft,
    KwaiKAT,
    LongCat,
    ModelIcon,
    Kolors,
    SenseNova,
} from '@lobehub/icons';
import { modelMappings } from '@lobehub/icons/es/features/modelConfig';
import { memo, type ComponentProps } from 'react';

type AvatarComponent = typeof OpenAI.Avatar;

type ModelIconConfig = {
    prefixes: string[];
    Avatar: AvatarComponent;
    color: string;
};

type ModelAvatarProps = ComponentProps<typeof OpenAI.Avatar>;

/** Official organization avatars for model families not bundled by LobeHub. */
function createOfficialOrgAvatar(path: string, fallbackColor: string) {
    return memo(function OfficialOrgAvatar({ className, shape = 'circle', size = 18, style }: ModelAvatarProps) {
        return (
            <span
                aria-hidden="true"
                className={className}
                style={{
                    display: 'inline-block',
                    flex: 'none',
                    width: size,
                    height: size,
                    borderRadius: shape === 'circle' ? '50%' : Math.floor(size * 0.1),
                    backgroundColor: fallbackColor,
                    backgroundImage: `url("${path}")`,
                    backgroundPosition: 'center',
                    backgroundRepeat: 'no-repeat',
                    backgroundSize: 'cover',
                    ...style,
                }}
            />
        );
    });
}

const OFFICIAL_ORG_AVATARS = {
    agnes: createOfficialOrgAvatar('/model-icons/agnes.png', '#3248AF'),
    mimo: createOfficialOrgAvatar('/model-icons/mimo.png', '#FF6900'),
    muse: createOfficialOrgAvatar('/model-icons/muse.png', '#087FF5'),
    paddlepaddle: createOfficialOrgAvatar('/model-icons/paddlepaddle.png', '#2D9CDB'),
    openmoss: createOfficialOrgAvatar('/model-icons/openmoss.png', '#334155'),
    inclusionai: createOfficialOrgAvatar('/model-icons/inclusionai.png', '#111827'),
    nexagi: createOfficialOrgAvatar('/model-icons/nexagi.png', '#0F766E'),
    teleai: createOfficialOrgAvatar('/model-icons/teleai.png', '#2563EB'),
} satisfies Record<string, AvatarComponent>;

/**
 * Provider configurations with prefixes, Avatar components, and brand colors
 * Similar to Go's Provider array in internal/price/price.go
 */
const MODEL_ICON_PATTERNS: ModelIconConfig[] = [
    // Official model organizations without a bundled LobeHub icon.
    // Agnes AI gateway / first-party models (agnes-ai.com).
    { prefixes: ['agnes', 'pavo', 'echo'], Avatar: OFFICIAL_ORG_AVATARS.agnes, color: '#3248AF' },
    // Xiaomi MiMo series.
    { prefixes: ['mimo', 'xiaomi'], Avatar: OFFICIAL_ORG_AVATARS.mimo, color: '#FF6900' },
    // Meta Muse Spark series. Keep this before the generic iFlytek Spark rule:
    // `muse-spark-*` is a Meta model, not an iFlytek Spark model.
    { prefixes: ['muse'], Avatar: OFFICIAL_ORG_AVATARS.muse, color: '#087FF5' },
    { prefixes: ['paddlepaddle'], Avatar: OFFICIAL_ORG_AVATARS.paddlepaddle, color: '#2D9CDB' },
    { prefixes: ['fnlp', 'moss'], Avatar: OFFICIAL_ORG_AVATARS.openmoss, color: '#334155' },
    { prefixes: ['inclusionai', 'ling-'], Avatar: OFFICIAL_ORG_AVATARS.inclusionai, color: '#111827' },
    { prefixes: ['nex-agi', 'nex-'], Avatar: OFFICIAL_ORG_AVATARS.nexagi, color: '#0F766E' },
    { prefixes: ['teleai', 'telespeech'], Avatar: OFFICIAL_ORG_AVATARS.teleai, color: '#2563EB' },
    // FunAudioLLM, Tongyi-MAI and Wan-AI are Alibaba/Tongyi model families.
    { prefixes: ['funaudiollm', 'tongyi-mai', 'wan-ai', 'wan2'], Avatar: Alibaba.Avatar, color: '#1677FF' },
    // OpenAI - GPT series
    { prefixes: ['gpt-', 'o1', 'o3', 'o4', 'chatgpt', 'text-embedding', 'dall-e', 'openai'], Avatar: OpenAI.Avatar, color: '#10A37F' },
    // Anthropic - Claude series
    { prefixes: ['claude', 'anthropic'], Avatar: Claude.Avatar, color: '#D7765A' },
    // Google - Gemini series
    { prefixes: ['gemini'], Avatar: Gemini.Avatar, color: '#4285F4' },
    { prefixes: ['gemma'], Avatar: Gemma.Avatar, color: '#4285F4' },
    { prefixes: ['palm', 'google'], Avatar: Google.Avatar, color: '#4285F4' },
    // DeepSeek series
    { prefixes: ['deepseek'], Avatar: DeepSeek.Avatar, color: '#4D6BFE' },
    // xAI - Grok series
    { prefixes: ['grok', 'xai'], Avatar: Grok.Avatar, color: '#000000' },
    // Alibaba - Qwen series
    { prefixes: ['qwen', 'qwq', 'alibaba'], Avatar: Qwen.Avatar, color: '#6B4EFF' },
    // Zhipu - GLM series
    { prefixes: ['glm', 'chatglm', 'zhipu', 'z-ai'], Avatar: Zhipu.Avatar, color: '#3C5BFC' },
    // MiniMax series
    { prefixes: ['minimax', 'abab'], Avatar: Minimax.Avatar, color: '#1A1A2E' },
    // Moonshot/Kimi series
    { prefixes: ['moonshot', 'kimi'], Avatar: Kimi.Avatar, color: '#000000' },
    // Mistral series
    { prefixes: ['mistral', 'mixtral', 'codestral', 'pixtral'], Avatar: Mistral.Avatar, color: '#F7D046' },
    // Meta - Llama series
    { prefixes: ['llama', 'meta-llama', 'meta'], Avatar: Meta.Avatar, color: '#0668E1' },
    // ByteDance - Doubao series
    { prefixes: ['doubao', 'skylark', 'bytedance'], Avatar: Doubao.Avatar, color: '#00D6C2' },
    // Yi series
    { prefixes: ['yi-', '01-ai'], Avatar: Yi.Avatar, color: '#1B1464' },
    // Tencent - Hunyuan
    { prefixes: ['hunyuan', 'hy3'], Avatar: Hunyuan.Avatar, color: '#0052D9' },
    // iFlytek - Spark
    { prefixes: ['spark'], Avatar: Spark.Avatar, color: '#0078FF' },
    // Baidu - ERNIE/Wenxin
    { prefixes: ['ernie', 'wenxin', 'baidu'], Avatar: Wenxin.Avatar, color: '#2932E1' },
    // InternLM
    { prefixes: ['internlm'], Avatar: InternLM.Avatar, color: '#2F54EB' },
    // Stepfun
    { prefixes: ['stepfun', 'step-'], Avatar: Stepfun.Avatar, color: '#5B5CFF' },
    // Cloud providers
    { prefixes: ['nvidia', 'nemotron'], Avatar: Nvidia.Avatar, color: '#76B900' },
    { prefixes: ['azure'], Avatar: Azure.Avatar, color: '#0078D4' },
    { prefixes: ['aws', 'amazon', 'bedrock'], Avatar: Aws.Avatar, color: '#FF9900' },
    { prefixes: ['volcengine'], Avatar: Volcengine.Avatar, color: '#3370FF' },
    { prefixes: ['siliconflow'], Avatar: SiliconCloud.Avatar, color: '#7C3AED' },
    // Audio / media models
    { prefixes: ['cosyvoice', 'sensevoice'], Avatar: Alibaba.Avatar, color: '#1677FF' },
    { prefixes: ['kolors', 'kwai-kolors'], Avatar: Kolors.Avatar, color: '#FF6B35' },
    { prefixes: ['sensenova', 'sensechat'], Avatar: SenseNova.Avatar, color: '#5B5CFF' },
    { prefixes: ['longcat'], Avatar: LongCat.Avatar, color: '#111827' },
    { prefixes: ['baai'], Avatar: BAAI.Avatar, color: '#2563EB' },
    // Inference providers
    { prefixes: ['groq'], Avatar: Groq.Avatar, color: '#F55036' },
    { prefixes: ['together'], Avatar: Together.Avatar, color: '#0F6FFF' },
    { prefixes: ['fireworks'], Avatar: Fireworks.Avatar, color: '#FF6B00' },
    { prefixes: ['replicate'], Avatar: Replicate.Avatar, color: '#000000' },
    { prefixes: ['ollama'], Avatar: Ollama.Avatar, color: '#FFFFFF' },
    { prefixes: ['openrouter'], Avatar: OpenRouter.Avatar, color: '#6366F1' },
    { prefixes: ['cloudflare'], Avatar: Cloudflare.Avatar, color: '#F38020' },
    { prefixes: ['cerebras'], Avatar: Cerebras.Avatar, color: '#FF5722' },
    { prefixes: ['sambanova'], Avatar: SambaNova.Avatar, color: '#FF6B00' },
    { prefixes: ['novita'], Avatar: Novita.Avatar, color: '#7C3AED' },
    { prefixes: ['huggingface', 'hf'], Avatar: HuggingFace.Avatar, color: '#FFD21E' },
    // Other models
    { prefixes: ['cohere', 'command'], Avatar: Cohere.Avatar, color: '#39594D' },
    { prefixes: ['perplexity'], Avatar: Perplexity.Avatar, color: '#20B8CD' },
    { prefixes: ['phi-'], Avatar: Microsoft.Avatar, color: '#00BCF2' },
    { prefixes: ['kat'], Avatar: KwaiKAT.Avatar, color: '#1969FC' },
];

// Default configuration
// Models covered by the library still resolve its brand avatar; models unknown
// to both the prefix table and the library render the neutral LLM artwork
// instead of the library's plain gray default.
const UNKNOWN_MODEL_AVATAR = createOfficialOrgAvatar('/model-icons/llm-fallback.png', '#FFFFFF');

/** Mirror ModelIcon's own keyword matching so detection cannot drift from rendering. */
function lobeHubHasMapping(modelName: string): boolean {
    const model = modelName.toLowerCase();
    return modelMappings.some(({ keywords }) =>
        keywords.some((keyword) => new RegExp(keyword, 'i').test(model)),
    );
}

const fallbackAvatarCache = new Map<string, AvatarComponent>();

function getFallbackAvatar(modelName: string): AvatarComponent {
    if (!lobeHubHasMapping(modelName)) return UNKNOWN_MODEL_AVATAR;

    const cachedAvatar = fallbackAvatarCache.get(modelName);
    if (cachedAvatar) return cachedAvatar;

    const FallbackModelAvatar = memo(function FallbackModelAvatar({ className, shape, size, style }: ComponentProps<typeof OpenAI.Avatar>) {
        return <ModelIcon className={className} model={modelName} shape={shape} size={size} style={style} type="avatar" />;
    });
    fallbackAvatarCache.set(modelName, FallbackModelAvatar);
    return FallbackModelAvatar;
}

/**
 * Get the Avatar component and color for a given model name
 * @param modelName - The name of the model
 * @returns Object containing Avatar component and brand color
 */
export function getModelIcon(modelName: string): { Avatar: AvatarComponent; color: string } {
    // Extract the part after the first '/' if it exists
    // e.g., "qwen/gpt-5.2" -> "gpt-5.2"
    const lowerFullName = modelName.toLowerCase();
    const nameToMatch = modelName.includes('/') ? modelName.split('/')[1] : modelName;
    const lowerName = nameToMatch.toLowerCase();
    const providerSegments = lowerFullName.split('/').slice(0, -1);
    for (const { prefixes, Avatar, color } of MODEL_ICON_PATTERNS) {
        if (prefixes.some(prefix =>
            lowerName.startsWith(prefix) ||
            lowerFullName.startsWith(prefix) ||
            providerSegments.some((segment) => segment.startsWith(prefix))
        )) {
            return { Avatar, color };
        }
    }
    return { Avatar: getFallbackAvatar(modelName), color: '#64748B' };
}
