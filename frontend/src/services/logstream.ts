import { auth } from '$stores/auth';

const API_BASE = import.meta.env.VITE_API_URL ?? (import.meta.env.DEV ? '/rpc' : 'http://localhost:8090');

export interface LogLine {
  podName: string;
  /** Kubelet timestamp, RFC 3339. Empty when the line had no parseable prefix. */
  timestamp: string;
  message: string;
}

export interface StreamPodLogsOptions {
  namespace: string;
  podName: string;
  container?: string;
  tailLines?: number;
  follow?: boolean;
  /** Aborting this signal closes the HTTP request and ends the stream. */
  signal?: AbortSignal;
}

/**
 * Thrown when the server ends the stream with an error frame. Carries the
 * Connect error code so callers can distinguish "pod is gone, stop retrying"
 * from "transient failure, reconnect".
 */
export class StreamError extends Error {
  constructor(
    message: string,
    readonly code: string,
  ) {
    super(message);
    this.name = 'StreamError';
  }
}

/**
 * Connect's streaming protocol frames every message as
 * `[1 flag byte][4-byte big-endian length][payload]`.
 *
 * Flag bit 0x02 marks the end-of-stream frame, whose payload is `{}` on success
 * or `{"error": {...}}` on failure. Everything else is a message frame carrying
 * one JSON-encoded LogLine.
 *
 * This is hand-rolled because the rest of the app talks to Connect with plain
 * `fetch` JSON POSTs rather than generated clients; pulling in the generated
 * client for one screen would leave two different transports in the codebase.
 */
const FLAG_END_STREAM = 0x02;
const HEADER_BYTES = 5;

/**
 * Streams log lines as they arrive. Yields each line; returns normally when the
 * server closes the stream; throws StreamError if the server ends with an error.
 *
 * Aborting `signal` ends iteration quietly — a user closing the log viewer is
 * not a failure.
 */
export async function* streamPodLogs(opts: StreamPodLogsOptions): AsyncGenerator<LogLine> {
  const headers = new Headers({
    'Content-Type': 'application/connect+json',
    'Connect-Protocol-Version': '1',
  });
  if (auth.isEnabled()) {
    const token = (await auth.ensureFreshToken()) ?? auth.getToken();
    if (token) headers.set('Authorization', `Bearer ${token}`);
  }

  const payload = new TextEncoder().encode(
    JSON.stringify({
      namespace: opts.namespace,
      podName: opts.podName,
      container: opts.container ?? '',
      tailLines: opts.tailLines ?? 0,
      follow: opts.follow ?? true,
    }),
  );

  // The request body is itself an enveloped frame.
  const body = new Uint8Array(HEADER_BYTES + payload.length);
  new DataView(body.buffer).setUint32(1, payload.length, false);
  body.set(payload, HEADER_BYTES);

  let response: Response;
  try {
    response = await fetch(`${API_BASE}/idp.v1.ClusterService/StreamPodLogs`, {
      method: 'POST',
      headers,
      body,
      signal: opts.signal,
    });
  } catch (err: any) {
    if (err?.name === 'AbortError') return;
    throw new StreamError(err?.message || 'Could not reach the log stream', 'unavailable');
  }

  if (!response.ok || !response.body) {
    // A non-2xx from Connect carries a plain JSON error, not a frame.
    const detail = await response.json().catch(() => ({}) as any);
    throw new StreamError(
      detail?.message || `Log stream failed: ${response.statusText}`,
      detail?.code || 'unknown',
    );
  }

  const reader = response.body.getReader();
  // Typed over ArrayBufferLike because that is what the stream reader yields;
  // the default Uint8Array<ArrayBuffer> would reject those chunks.
  let buffer: ByteChunk = new Uint8Array(0);

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) return;

      buffer = concat(buffer, value);

      // One chunk can hold several frames, or half of one — keep consuming
      // while a whole frame is available.
      while (buffer.length >= HEADER_BYTES) {
        const flags = buffer[0];
        const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);
        const length = view.getUint32(1, false);
        if (buffer.length < HEADER_BYTES + length) break;

        const frame = buffer.subarray(HEADER_BYTES, HEADER_BYTES + length);
        buffer = buffer.slice(HEADER_BYTES + length);

        const text = new TextDecoder().decode(frame);

        if ((flags & FLAG_END_STREAM) !== 0) {
          const trailer = text ? JSON.parse(text) : {};
          if (trailer.error) {
            throw new StreamError(
              trailer.error.message || 'Log stream ended with an error',
              trailer.error.code || 'unknown',
            );
          }
          return;
        }

        const raw = JSON.parse(text);
        yield {
          podName: raw.podName || '',
          timestamp: raw.timestamp || '',
          message: raw.message ?? '',
        };
      }
    }
  } catch (err: any) {
    // Abort is the normal way a viewer closes; anything else propagates.
    if (err?.name === 'AbortError') return;
    throw err;
  } finally {
    // Cancelling lets the browser tear down the connection instead of leaving
    // it open after the consumer stops iterating.
    reader.cancel().catch(() => {});
  }
}

/** Bytes as delivered by a ReadableStream reader. */
type ByteChunk = Uint8Array<ArrayBufferLike>;

function concat(a: ByteChunk, b: ByteChunk): ByteChunk {
  if (a.length === 0) return b;
  const out = new Uint8Array(a.length + b.length);
  out.set(a, 0);
  out.set(b, a.length);
  return out;
}

/** Renders a kubelet timestamp as a local wall-clock time for display. */
export function formatLogTimestamp(timestamp: string): string {
  if (!timestamp) return '';
  const parsed = new Date(timestamp);
  if (Number.isNaN(parsed.getTime())) return timestamp;
  return (
    parsed.toLocaleTimeString(undefined, { hour12: false }) +
    '.' +
    String(parsed.getMilliseconds()).padStart(3, '0')
  );
}
