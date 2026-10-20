import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  shouldStartNextProcess,
  startProcess,
  stopProcess,
  waitForExit,
} from './server-process.mjs';

const repoRoot = fileURLToPath(new URL('../', import.meta.url));
const temporaryDirectory = await mkdtemp(path.join(tmpdir(), 'videoshare-e2e-'));
const executableName = process.platform === 'win32' ? 'e2e-server.exe' : 'e2e-server';
const executablePath = path.join(temporaryDirectory, executableName);
const childEnvironment = {
  ...process.env,
  GOPROXY: 'off',
  GOTOOLCHAIN: 'local',
  GOSUMDB: 'off',
  VIDEO_SHARE_E2E: '1',
  TMP: temporaryDirectory,
  TEMP: temporaryDirectory,
  TMPDIR: temporaryDirectory,
};

let activeProcess;
let cancellationSignal;
let stopPromise;

function signalExitCode(signal, result, serverPhase = false) {
  if (serverPhase && signal === 'SIGTERM' && result?.code === 0 && result.signal === null) return 0;
  return signal === 'SIGINT' ? 130 : 143;
}

function requestStop(signal) {
  if (cancellationSignal) return;
  cancellationSignal = signal;
  if (!activeProcess) return;
  stopPromise = stopProcess(activeProcess, {
    closeStdin: Boolean(activeProcess.child.stdin && !activeProcess.child.stdin.destroyed),
    signal,
  }).catch((error) => {
    console.error('failed to stop fixture child:', error);
    process.exitCode = 1;
  });
}

process.on('SIGINT', () => requestStop('SIGINT'));
process.on('SIGTERM', () => requestStop('SIGTERM'));

try {
  const compile = startProcess(
    'go',
    ['test', '-c', '-tags=playwright', '-o', executablePath, './internal/app'],
    {
      cwd: repoRoot,
      env: childEnvironment,
      stdio: 'inherit',
    },
  );
  activeProcess = compile;
  const compileResult = await waitForExit(compile);
  if (activeProcess === compile) activeProcess = undefined;

  if (!shouldStartNextProcess(Boolean(cancellationSignal), compileResult)) {
    process.exitCode = cancellationSignal
      ? signalExitCode(cancellationSignal, compileResult)
      : compileResult.code ?? 1;
  } else {
    const server = startProcess(
      executablePath,
      ['-test.run=^TestPlaywrightServer$', '-test.timeout=10m'],
      {
        cwd: repoRoot,
        env: childEnvironment,
        stdio: ['pipe', 'inherit', 'inherit'],
      },
    );
    activeProcess = server;
    const serverResult = await waitForExit(server);
    if (activeProcess === server) activeProcess = undefined;
    process.exitCode = cancellationSignal
      ? signalExitCode(cancellationSignal, serverResult, true)
      : serverResult.code ?? 1;
  }
} catch (error) {
  if (cancellationSignal) {
    process.exitCode = signalExitCode(cancellationSignal);
  } else {
    console.error(error);
    process.exitCode = 1;
  }
} finally {
  if (activeProcess) {
    try {
      await stopProcess(activeProcess, {
        closeStdin: Boolean(activeProcess.child.stdin && !activeProcess.child.stdin.destroyed),
      });
    } catch (error) {
      console.error('failed to stop fixture child:', error);
      process.exitCode = 1;
    }
  }
  if (stopPromise) await stopPromise;

  try {
    await rm(temporaryDirectory, {
      recursive: true,
      force: true,
      maxRetries: 5,
      retryDelay: 200,
    });
  } catch (error) {
    console.error(`failed to remove fixture temp directory ${temporaryDirectory}:`, error);
    process.exitCode = 1;
  }
}
