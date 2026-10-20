import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {
  shouldStartNextProcess,
  startProcess,
  stopProcess,
  waitForExit,
} from './server-process.mjs';

test('waitForExit reports normal completion', async () => {
  const processHandle = startProcess(
    process.execPath,
    ['-e', 'process.exit(0)'],
    { stdio: 'ignore' },
  );
  assert.deepEqual(await waitForExit(processHandle, 5000), { code: 0, signal: null });
});

test('waitForExit rejects a spawn error', async () => {
  const missingExecutable = path.join(os.tmpdir(), `missing-${randomUUID()}.exe`);
  const processHandle = startProcess(missingExecutable, [], { stdio: 'ignore' });
  await assert.rejects(waitForExit(processHandle, 5000), { code: 'ENOENT' });
});

test('stopProcess closes stdin for a cooperative child', async () => {
  const processHandle = startProcess(
    process.execPath,
    ['-e', 'process.stdin.resume(); process.stdin.on("end", () => process.exit(0))'],
    { stdio: ['pipe', 'ignore', 'ignore'] },
  );
  assert.deepEqual(
    await stopProcess(processHandle, { closeStdin: true, gracefulTimeoutMs: 1000 }),
    { code: 0, signal: null },
  );
});

test('stopProcess terminates a child that ignores stdin EOF within a bound', async () => {
  const processHandle = startProcess(
    process.execPath,
    ['-e', 'process.stdin.resume(); setInterval(() => {}, 1000)'],
    { stdio: ['pipe', 'ignore', 'ignore'] },
  );
  const startedAt = Date.now();
  const result = await stopProcess(processHandle, {
    closeStdin: true,
    gracefulTimeoutMs: 50,
    terminateTimeoutMs: 1000,
    killTimeoutMs: 1000,
  });
  assert.ok(Date.now() - startedAt < 5000);
  assert.ok(result.code === null || result.code === 0);
  if (result.code === null) assert.ok(['SIGTERM', 'SIGKILL'].includes(result.signal));
});

test('stopProcess does not accept an exit status without a close event', async () => {
  const processHandle = {
    child: { exitCode: 0, signalCode: null },
    closed: new Promise(() => {}),
  };
  await assert.rejects(
    stopProcess(processHandle, { terminateTimeoutMs: 20 }),
    /close event was not observed/,
  );
});

test('cancelled or failed compilation cannot start the server process', () => {
  assert.equal(shouldStartNextProcess(true, { code: 0, signal: null }), false);
  assert.equal(shouldStartNextProcess(false, { code: 1, signal: null }), false);
  assert.equal(shouldStartNextProcess(false, { code: null, signal: 'SIGTERM' }), false);
  assert.equal(shouldStartNextProcess(false, { code: 0, signal: null }), true);
});
