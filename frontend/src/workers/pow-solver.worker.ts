import { argon2id } from 'hash-wasm';

import type { POWChallenge } from '../types/api';

type StartWorkerMessage = {
  type: 'start';
  job_id: string;
  challenge: POWChallenge;
};

type StopWorkerMessage = {
  type: 'stop';
  job_id?: string;
};

type IncomingWorkerMessage = StartWorkerMessage | StopWorkerMessage;

let activeJobID = '';
let stopRequested = false;

self.onmessage = (event: MessageEvent<IncomingWorkerMessage>) => {
  const message = event.data;
  if (message.type === 'stop') {
    if (!message.job_id || message.job_id === activeJobID) {
      stopRequested = true;
    }
    return;
  }

  if (message.type === 'start') {
    activeJobID = message.job_id;
    stopRequested = false;
    void solveChallenge(message.job_id, message.challenge);
  }
};

async function solveChallenge(jobID: string, challenge: POWChallenge): Promise<void> {
  try {
    const salt = hexToBytes(challenge.salt_hex);
    const startedAt = performance.now();
    let attempts = 0;
    let bestLeadingZeroBits = 0;
    let lastProgressAt = startedAt;

    for (let nonceValue = 0; ; nonceValue += 1) {
      if (stopRequested || jobID !== activeJobID) {
        postMessage({
          type: 'stopped',
          job_id: jobID,
          attempts,
          elapsed_ms: Math.round(performance.now() - startedAt),
        });
        return;
      }

      const nonce = String(nonceValue);
      const hashBytes = await argon2id({
        password: `${challenge.challenge_token}:${nonce}`,
        salt,
        parallelism: challenge.argon2_parallelism,
        iterations: challenge.argon2_iterations,
        memorySize: challenge.argon2_memory_kib,
        hashLength: challenge.argon2_hash_length,
        outputType: 'binary',
      });

      attempts += 1;
      const leadingZeroBits = countLeadingZeroBits(hashBytes);
      if (leadingZeroBits > bestLeadingZeroBits) {
        bestLeadingZeroBits = leadingZeroBits;
      }

      if (leadingZeroBits >= challenge.difficulty) {
        postMessage({
          type: 'solved',
          job_id: jobID,
          nonce,
          attempts,
          elapsed_ms: Math.round(performance.now() - startedAt),
          leading_zero_bits: leadingZeroBits,
        });
        return;
      }

      const now = performance.now();
      if (attempts === 1 || now-lastProgressAt >= 250) {
        lastProgressAt = now;
        postMessage({
          type: 'progress',
          job_id: jobID,
          attempts,
          elapsed_ms: Math.round(now - startedAt),
          best_leading_zero_bits: bestLeadingZeroBits,
        });
      }
    }
  } catch (error) {
    const message = error instanceof Error && error.message.trim() !== '' ? error.message : 'worker 解题失败';
    postMessage({
      type: 'error',
      job_id: jobID,
      message,
    });
  }
}

function hexToBytes(value: string): Uint8Array {
  const normalized = value.trim().toLowerCase();
  if (normalized.length === 0 || normalized.length % 2 !== 0) {
    throw new Error('invalid hex string');
  }

  const bytes = new Uint8Array(normalized.length / 2);
  for (let index = 0; index < normalized.length; index += 2) {
    bytes[index / 2] = Number.parseInt(normalized.slice(index, index + 2), 16);
  }
  return bytes;
}

function countLeadingZeroBits(bytes: Uint8Array): number {
  let total = 0;
  for (const value of bytes) {
    if (value === 0) {
      total += 8;
      continue;
    }
    total += Math.clz32(value) - 24;
    break;
  }
  return total;
}
