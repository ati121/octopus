import assert from 'node:assert/strict';
import test from 'node:test';
import { getLogContentPreview, textBoundary } from '../src/components/modules/log/content-preview.ts';

test('最近的用户输入不被系统提示、工具结果或更早的对话替代', () => {
    assert.equal(getLogContentPreview({ messages: [
        { role: 'system', content: 'system' },
        { role: 'user', content: 'old question' },
        { role: 'assistant', content: 'old answer' },
        { role: 'user', content: 'latest question' },
        { role: 'assistant', tool_calls: [{ function: { arguments: 'private args' } }] },
        { role: 'tool', content: 'tool result' },
    ] }, 'request'), 'latest question');
});

test('最新用户消息仅有图片时不误显示上一条用户输入', () => {
    assert.equal(getLogContentPreview({ messages: [
        { role: 'user', content: 'old question' },
        { role: 'user', content: [{ type: 'image_url', image_url: { url: 'data:image/png;base64,hidden' } }] },
    ] }, 'request'), '');
});

test('Responses 输入字符串与多块输入', () => {
    assert.equal(getLogContentPreview({ input: 'prompt' }, 'request'), 'prompt');
    assert.equal(getLogContentPreview({ input: [
        { role: 'user', content: [{ type: 'input_text', text: 'first' }, { type: 'input_image', image_url: 'hidden' }, { type: 'input_text', text: 'second' }] },
        { type: 'function_call_output', output: 'tool result' },
    ] }, 'request'), 'first\n\nsecond');
});

test('Chat / 统一日志响应选择最后一项输出，忽略 reasoning 与工具参数', () => {
    assert.equal(getLogContentPreview({ choices: [
        { message: { content: 'old' } },
        { message: { content: 'answer', reasoning_content: 'hidden', tool_calls: [{ function: { arguments: 'hidden' } }] } },
    ] }, 'response'), 'answer');
    assert.equal(getLogContentPreview({ choices: [{ message: { content: null, refusal: 'declined' } }] }, 'response'), 'declined');
    assert.equal(getLogContentPreview({ choices: [{ message: { tool_calls: [{}] } }] }, 'response'), '');
});

test('Responses 最后一条模型消息不被工具调用或思考覆盖', () => {
    assert.equal(getLogContentPreview({ output: [
        { role: 'assistant', type: 'message', content: [{ type: 'output_text', text: 'old' }] },
        { role: 'assistant', type: 'message', content: [{ type: 'output_text', text: 'final' }] },
        { type: 'function_call', arguments: 'hidden' },
    ] }, 'response'), 'final');
});

test('Anthropic 与 Gemini 保留可见文本块，不显示思考或附件', () => {
    assert.equal(getLogContentPreview({ content: [{ type: 'thinking', thinking: 'hidden' }, { type: 'text', text: 'answer' }] }, 'response'), 'answer');
    assert.equal(getLogContentPreview({ contents: [{ role: 'user', parts: [{ text: 'question' }, { inlineData: { data: 'hidden' } }] }] }, 'request'), 'question');
    assert.equal(getLogContentPreview({ candidates: [{ content: { parts: [{ text: 'hidden', thought: true }, { text: 'answer' }] } }] }, 'response'), 'answer');
});

test('无对话文本的数据保留在详情中，摘要不倾倒整个对象', () => {
    for (const value of [null, true, 42, [], { data: [0.1, 0.2] }, { unknown: 'secret' }]) {
        assert.equal(getLogContentPreview(value, 'request'), '');
        assert.equal(getLogContentPreview(value, 'response'), '');
    }
    assert.equal(getLogContentPreview('plain text', 'request'), 'plain text');
    assert.equal(getLogContentPreview({ prompt: 'draw a cat' }, 'request'), 'draw a cat');
});

test('分页接缝保留完整 Unicode 与全部文本，包含末页', () => {
    const original = 'a'.repeat(7999) + '🙂' + 'b'.repeat(7999) + '🙂END';
    const pages = Array.from({ length: Math.ceil(original.length / 8000) }, (_, page) => original.slice(
        textBoundary(original, page * 8000),
        textBoundary(original, Math.min(original.length, (page + 1) * 8000)),
    ));
    assert.equal(pages.join(''), original);
    for (const page of pages) assert.equal(page.isWellFormed(), true);
    assert.match(pages.at(-1), /END$/);
});
