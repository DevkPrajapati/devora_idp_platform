import { describe, expect, it, vi } from 'vitest';
import { streamPodLogs } from './logstream';

function envelope(flag: number, payload: string): Uint8Array {
  const json = new TextEncoder().encode(payload);
  const frame = new Uint8Array(5 + json.length);
  frame[0] = flag;
  new DataView(frame.buffer).setUint32(1, json.length, false);
  frame.set(json, 5);
  return frame;
}

describe('streamPodLogs', () => {
  it('parses Connect log frames split across many small chunks', async () => {
    const message = envelope(
      0,
      JSON.stringify({
        podName: 'api-1',
        timestamp: '2026-08-28T06:00:00Z',
        message: 'listening on :8000',
      }),
    );
    const end = envelope(0x02, '{}');
    const all = new Uint8Array(message.length + end.length);
    all.set(message);
    all.set(end, message.length);

    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        for (let offset = 0; offset < all.length; ) {
          const n = Math.min(8, all.length - offset);
          controller.enqueue(all.slice(offset, offset + n));
          offset += n;
        }
        controller.close();
      },
    });

    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        body: stream,
      }),
    );

    const lines: string[] = [];
    for await (const line of streamPodLogs({ namespace: 'ns', podName: 'api-1', follow: false })) {
      lines.push(line.message);
    }
    expect(lines).toEqual(['listening on :8000']);
  });
});
