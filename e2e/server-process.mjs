import { spawn } from 'node:child_process';

export function startProcess(command, args, options) {
  const child = spawn(command, args, options);
  const closed = new Promise((resolve, reject) => {
    child.once('error', reject);
    child.once('close', (code, signal) => resolve({ code, signal }));
  });
  child.stdin?.on('error', () => {});
  return { child, closed };
}

export async function waitForExit(processHandle, timeoutMs = Infinity) {
  if (!Number.isFinite(timeoutMs)) return processHandle.closed;

  let timer;
  const timeout = new Promise((resolve) => {
    timer = setTimeout(() => resolve(null), timeoutMs);
  });
  try {
    return await Promise.race([processHandle.closed, timeout]);
  } finally {
    clearTimeout(timer);
  }
}

export async function stopProcess(processHandle, {
  closeStdin = false,
  signal = 'SIGTERM',
  gracefulTimeoutMs = 5000,
  terminateTimeoutMs = 2000,
  killTimeoutMs = 2000,
} = {}) {
  const { child } = processHandle;
  if (child.exitCode !== null || child.signalCode !== null) {
    const result = await waitForExit(processHandle, terminateTimeoutMs);
    if (!result) throw new Error('child exit status was set but close event was not observed');
    return result;
  }

  if (closeStdin && child.stdin && !child.stdin.destroyed) {
    child.stdin.end();
  } else {
    child.kill(signal);
  }

  let result = await waitForExit(processHandle, gracefulTimeoutMs);
  if (result) return result;

  if (child.exitCode === null && child.signalCode === null) child.kill('SIGTERM');
  result = await waitForExit(processHandle, terminateTimeoutMs);
  if (result) return result;

  if (child.exitCode === null && child.signalCode === null) child.kill('SIGKILL');
  result = await waitForExit(processHandle, killTimeoutMs);
  if (!result) throw new Error('child process did not exit after termination');
  return result;
}

export function shouldStartNextProcess(cancelled, result) {
  return !cancelled && result.code === 0 && result.signal === null;
}
