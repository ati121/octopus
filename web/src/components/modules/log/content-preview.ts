export type LogContentKind = 'request' | 'response';

function record(value: unknown): Record<string, unknown> | undefined {
    return value !== null && typeof value === 'object' && !Array.isArray(value)
        ? value as Record<string, unknown>
        : undefined;
}

// 只提取对话文本，不把思考、工具参数、图片 Base64 等当作最终消息。
function messageText(value: unknown): string {
    if (typeof value === 'string') return value;
    if (!Array.isArray(value)) return '';
    return value.map((part) => {
        const block = record(part);
        if (!block || block.thought === true) return '';
        if (block.type === 'refusal' && typeof block.refusal === 'string') return block.refusal;
        if (block.type !== undefined && !['text', 'input_text', 'output_text'].includes(String(block.type))) return '';
        return typeof block.text === 'string' ? block.text : '';
    }).filter(Boolean).join('\n\n');
}

function lastMessage(messages: unknown[], roles: string[]): Record<string, unknown> | undefined {
    for (let i = messages.length - 1; i >= 0; i--) {
        const message = record(messages[i]);
        if (message && roles.includes(String(message.role))) return message;
    }
    return undefined;
}

/** 默认预览最近一条用户输入 / 最后一条模型输出；完整载荷由折叠树保留。 */
export function getLogContentPreview(value: unknown, kind: LogContentKind): string {
    if (typeof value === 'string') return value;
    const body = record(value);
    if (!body) return '';

    if (kind === 'request') {
        if (Array.isArray(body.messages)) {
            return messageText(lastMessage(body.messages, ['user'])?.content);
        }
        if (typeof body.input === 'string') return body.input;
        if (Array.isArray(body.input)) {
            return messageText(lastMessage(body.input, ['user'])?.content);
        }
        if (Array.isArray(body.contents)) {
            return messageText(lastMessage(body.contents, ['user'])?.parts);
        }
        return typeof body.prompt === 'string' ? body.prompt : '';
    }

    if (Array.isArray(body.choices) && body.choices.length > 0) {
        const choice = record(body.choices[body.choices.length - 1]);
        const message = record(choice?.message) ?? record(choice?.delta);
        return messageText(message?.content) || messageText(message?.refusal) || messageText(choice?.text);
    }
    if (Array.isArray(body.output)) {
        return messageText(lastMessage(body.output, ['assistant'])?.content);
    }
    if (typeof body.output_text === 'string') return body.output_text;
    if (Array.isArray(body.candidates) && body.candidates.length > 0) {
        const candidate = record(body.candidates[body.candidates.length - 1]);
        return messageText(record(candidate?.content)?.parts);
    }
    return messageText(body.content);
}

// 按 UTF-16 边界分页时，避免把 emoji 等字符的代理对截成两半。
export function textBoundary(text: string, offset: number): number {
    if (offset > 0 && offset < text.length && /[\uDC00-\uDFFF]/.test(text[offset]) && /[\uD800-\uDBFF]/.test(text[offset - 1])) {
        return offset - 1;
    }
    return offset;
}
