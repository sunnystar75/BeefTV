// Bound SSE inactivity on real text/tool progress, without aborting the official
// agent's signal or adding a retry loop.
//
// 为什么不改用 pi 自己的空闲超时（钉选 1.2.0）：
// - pi-ai 把 timeoutMs 交给 OpenAI SDK 的 `timeout`（pi-ai/dist/api/openai-completions.js:194），
//   而 SDK 在 fetch 解析出响应头后立刻 clearTimeout（openai/client.js:996 setTimeout、
//   :1023 finally clearTimeout）——只覆盖到响应头，不约束流正文的空闲。
// - pi-coding-agent 另有 undici bodyTimeout/headersTimeout 级别的 httpIdleTimeoutMs
//   （dist/core/http-dispatcher.js:77-81，docs/settings.md:125），但它只在 CLI/RPC 入口安装
//   （cli/setup.js、rpc-entry.js、modes/interactive/interactive-mode.js:1562），本宿主的
//   Durable 路径不会安装它；即便安装，undici bodyTimeout 按「每收到一段 body 就重置」，
//   SSE 心跳（`: ping`）同样算一段，识别不出「只有心跳、没有内容」。
// 因此保留本实现：只有实际文本/工具进展才重置计时（Durable 路径经 globalThis.fetch 包装生效）。
// finish_reason / [DONE] 不是新的进展。beefapi 一类兼容服务会在 finish_reason 之后
// 不发 [DONE]、只留着连接或重复终态帧；若把终态当成进展，空闲计时会被一直续上，
// Durable wait 不返回，界面就停在「处理中」。终态只开一段短宽限，宽限内没有新文本
// 或工具参数就结束本次流，让官方解析器收尾。
//
// 思考字段和纯空白也不是回答进展。enterprise.beefapi.com 是 new-api 网关，
// deepseek-v4.1-flash / gemini-3.8-flash / glm-5.3 会在计费后持续推
// reasoning_content（或只有空白的 content），连接不关、也不给 finish_reason / [DONE]。
// 界面只转发 text_delta，若把这些片段当成进展，95 秒空闲和回合停滞回退都会被续上。
// 回答文本或工具参数才顺延空闲计时。另有一个不随增量重置的截止时间，避免
// 看起来像回答的心跳一直占着这一次流。
export const DEFAULT_MODEL_STREAM_IDLE_MS = 95000;
export const DEFAULT_STREAM_TERMINAL_GRACE_MS = 5000;
export const DEFAULT_MODEL_STREAM_DEADLINE_MS = 20 * 60 * 1000;

function protocolFor(input, options) {
  try {
    const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url);
    if ((options.method || input.method || 'GET').toUpperCase() !== 'POST') return null;
    if (typeof options.body !== 'string') return null;
    const body = JSON.parse(options.body);
    if (body.stream !== true) return null;
    if (/\/chat\/completions\/?$/.test(url.pathname) && Array.isArray(body.messages)) return 'completions';
    if (/\/responses\/?$/.test(url.pathname) && body.input !== undefined) return 'responses';
    if (/\/messages\/?$/.test(url.pathname) && Array.isArray(body.messages)) return 'anthropic';
  } catch { /* Unknown request formats retain their provider's behavior. */ }
  return null;
}
const text = value => typeof value === 'string' && value.length > 0;
// Zero-width and ordinary whitespace are not answer text. new-api keepalives use them.
const answerText = value => {
  if (typeof value === 'string') return value.replace(/[\s\u200b\u200c\u200d\ufeff\u2060]/g, '').length > 0;
  if (!Array.isArray(value)) return false;
  return value.some(part => typeof part === 'string' ? answerText(part) : part && typeof part === 'object' && (answerText(part.text) || answerText(part.content) || answerText(part.refusal)));
};
function combine(progress, terminal) {
  if (progress && terminal) return 'progress-terminal';
  if (progress) return 'progress';
  if (terminal) return 'terminal';
  return 'ignore';
}
function completionKind(event) {
  let progress = false, reasoning = false, terminal = false;
  for (const choice of Array.isArray(event.choices) ? event.choices : []) {
    if (!choice || typeof choice !== 'object') continue;
    const delta = choice.delta || {};
    const tools = Array.isArray(delta.tool_calls) ? delta.tool_calls : [];
    if (answerText(delta.content) || tools.some(tool => tool && (answerText(tool.id) || answerText(tool.function?.name) || answerText(tool.function?.arguments) || answerText(tool.custom?.name) || answerText(tool.custom?.input)))) progress = true;
    else if (answerText(delta.reasoning_content) || answerText(delta.reasoning) || answerText(delta.reasoning_text)) reasoning = true;
    if (choice.finish_reason) terminal = true;
  }
  if (progress || terminal) return combine(progress, terminal);
  return reasoning ? 'reasoning' : 'ignore';
}
function responsesKind(event) {
  if (['response.completed', 'response.failed', 'response.incomplete'].includes(event.type)) return 'terminal';
  if (['response.output_text.delta', 'response.reasoning_text.delta', 'response.reasoning_summary_text.delta', 'response.function_call_arguments.delta', 'response.custom_tool_call_input.delta'].includes(event.type)) return text(event.delta) ? 'progress' : 'ignore';
  if (event.type === 'response.function_call_arguments.done') return text(event.arguments) ? 'progress' : 'ignore';
  if (event.type === 'response.custom_tool_call_input.done') return text(event.input) ? 'progress' : 'ignore';
  if (event.type === 'response.output_item.done' && event.item?.type === 'message') {
    return (Array.isArray(event.item.content) ? event.item.content : []).some(part => part && (text(part.text) || text(part.refusal))) ? 'progress' : 'ignore';
  }
  return event.type === 'response.output_item.added' && ['function_call', 'custom_tool_call'].includes(event.item?.type) && (text(event.item.name) || text(event.item.call_id)) ? 'progress' : 'ignore';
}
function anthropicKind(event) {
  if (event.type === 'message_stop') return 'terminal';
  if (event.type === 'message_delta') return event.delta?.stop_reason ? 'terminal' : 'ignore';
  if (event.type === 'content_block_start') {
    const block = event.content_block || {};
    if (text(block.text) || text(block.thinking) || text(block.data) || (block.type === 'tool_use' && (text(block.name) || text(block.id)))) return 'progress';
    return 'ignore';
  }
  if (event.type !== 'content_block_delta') return 'ignore';
  // Thinking text and its closing signature are model progress, as text is.
  return text(event.delta?.text) || text(event.delta?.thinking) || text(event.delta?.signature) || text(event.delta?.partial_json) ? 'progress' : 'ignore';
}
// progress resets the long idle timer. terminal starts the short close grace.
// progress-terminal is a final chunk that still carried new text or tool arguments.
export function classifyModelStreamEvent(protocol, data) {
  if (data === '[DONE]') return 'terminal';
  let event; try { event = JSON.parse(data); } catch { return 'ignore'; }
  if (!event || typeof event !== 'object') return 'ignore';
  if (protocol === 'completions') return completionKind(event);
  if (protocol === 'responses') return responsesKind(event);
  return anthropicKind(event);
}

// Observe complete SSE records, forwarding the original bytes unchanged. A
// malformed or oversized record cannot keep the stream alive indefinitely.
function observer(protocol, onKind) {
  const decoder = new TextDecoder(); let line = '', data = [], bytes = 0, oversized = false, previousCR = false;
  const finishLine = () => {
    if (line === '') {
      if (!oversized) onKind(classifyModelStreamEvent(protocol, data.join('\n')));
      data = []; bytes = 0; oversized = false;
    } else if (!oversized && line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''));
    line = '';
  };
  return chunk => {
    for (const char of decoder.decode(chunk, {stream: true})) {
      if (char === '\n' && previousCR) { previousCR = false; continue; }
      previousCR = char === '\r';
      if (char === '\r' || char === '\n') { finishLine(); continue; }
      if (++bytes > 1024 * 1024) { oversized = true; data = []; line = ''; }
      else if (!oversized) line += char;
    }
  };
}

export async function fetchModelWithProgressIdle(input, options = {}, {fetch: transport = globalThis.fetch, timeoutMs = DEFAULT_MODEL_STREAM_IDLE_MS, graceMs = DEFAULT_STREAM_TERMINAL_GRACE_MS, deadlineMs = DEFAULT_MODEL_STREAM_DEADLINE_MS, onActivity} = {}) {
  const protocol = protocolFor(input, options);
  if (!protocol) return transport(input, options);
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) throw new Error('Invalid model stream idle timeout');
  if (!Number.isFinite(graceMs) || graceMs < 0) throw new Error('Invalid model stream terminal grace');
  if (!Number.isFinite(deadlineMs) || deadlineMs <= 0) throw new Error('Invalid model stream deadline');
  const local = new AbortController();
  const signals = [options.signal, input instanceof Request ? input.signal : null].filter(Boolean);
  const caller = signals.length ? AbortSignal.any(signals) : null;
  const response = await transport(input, {...options, signal: caller ? AbortSignal.any([caller, local.signal]) : local.signal});
  // stream:true responses are watched even when the proxy omits text/event-stream.
  // A body that finishes is forwarded unchanged; only a body that never ends is cut.
  if (!response.ok || !response.body) return response;
  const reader = response.body.getReader(); let timer, grace, deadline, graceArmed = false, finished = false, controller;
  const cleanup = () => { clearTimeout(timer); clearTimeout(grace); clearTimeout(deadline); caller?.removeEventListener('abort', onAbort); };
  const terminate = error => {
    if (finished) return;
    finished = true; cleanup(); controller.error(error);
    local.abort(error); void reader.cancel(error).catch(() => {});
  };
  // Close after a terminal frame so the official parser can finish without [DONE].
  // Do not abort the caller: this is completion, not a failed request.
  const complete = () => {
    if (finished) return;
    finished = true; cleanup();
    try { controller.close(); } catch { /* already closed after the terminal bytes */ }
    void reader.cancel().catch(() => {});
  };
  const onAbort = () => terminate(caller.reason || new DOMException('Aborted', 'AbortError'));
  const arm = () => {
    graceArmed = false; clearTimeout(grace); clearTimeout(timer); onActivity?.();
    timer = setTimeout(() => terminate(new Error(`Model stream idle timeout: no text or tool progress for ${timeoutMs} ms`)), timeoutMs);
    timer.unref?.();
  };
  const armGrace = () => {
    // Repeated finish_reason or [DONE] must not keep postponing the close.
    if (graceArmed) return;
    graceArmed = true; clearTimeout(timer); clearTimeout(deadline); clearTimeout(grace);
    grace = setTimeout(() => complete(), graceMs);
    grace.unref?.();
  };
  // Answer text and tool arguments reset the idle timer. Reasoning-only and
  // whitespace do not. The deadline never moves, so a drip of new-looking
  // bytes still ends the body.
  const armDeadline = () => {
    deadline = setTimeout(() => terminate(new Error(`Model stream deadline: no text or tool progress remained unfinished for ${deadlineMs} ms`)), deadlineMs);
    deadline.unref?.();
  };
  const observe = observer(protocol, kind => {
    if (kind === 'progress') arm();
    else if (kind === 'progress-terminal') { onActivity?.(); armGrace(); }
    else if (kind === 'terminal') armGrace();
  });
  const body = new ReadableStream({
    start(value) { controller = value; caller?.addEventListener('abort', onAbort, {once: true}); if (caller?.aborted) onAbort(); else { arm(); armDeadline(); } },
    async pull() {
      try {
        const next = await reader.read(); if (finished) return;
        if (next.done) { finished = true; cleanup(); controller.close(); return; }
        controller.enqueue(next.value);
        if (!finished) observe(next.value);
      } catch (error) { if (!finished) terminate(error); }
    },
    cancel(reason) { if (finished) return; finished = true; cleanup(); const cancelled = reader.cancel(reason).catch(() => {}); local.abort(reason); return cancelled; },
  });
  return new Response(body, {status: response.status, statusText: response.statusText, headers: response.headers});
}
