const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

(async () => {
  const binary = process.env.APPTRAIL_TEST_BINARY || path.resolve('apptrail-service-check.exe');
  const platform = process.platform;
  if (platform === 'darwin' && spawnSync('launchctl', ['print', `gui/${process.getuid()}`]).status !== 0) {
    console.log('Native LaunchAgent test skipped: this runner has no logged-in GUI domain.');
    return;
  }
  const unit = platform === 'darwin'
    ? path.join(os.homedir(), 'Library', 'LaunchAgents', 'io.github.samishal1998.apptrail.plist')
    : path.join(process.env.XDG_CONFIG_HOME || path.join(os.homedir(), '.config'), 'systemd', 'user', 'apptrail.service');
  if (platform === 'win32') {
    const existing = spawnSync('sc.exe', ['query', 'Apptrail']);
    assert.match(existing.stdout.toString(), /1060/, 'Refusing to touch an existing or inaccessible Windows Service');
  } else {
    assert.equal(fs.existsSync(unit), false, 'Refusing to touch an existing user service');
  }
  const data = fs.mkdtempSync(path.join(os.tmpdir(), 'apptrail service % $ & '));
  const url = 'http://127.0.0.1:19173/api/session';
  let occupied = false;
  try { await fetch(url, { signal: AbortSignal.timeout(1000) }); occupied = true; } catch {}
  assert.equal(occupied, false, 'Service test port is already occupied');
  const run = action => {
    const args = ['service', action];
    if (action === 'install') args.push('-addr', '127.0.0.1:19173', '-data', data);
    const result = spawnSync(binary, args, { encoding: 'utf8', timeout: 65000 });
    assert.equal(result.status, 0, `${action}: ${result.stdout}\n${result.stderr}\n${result.error || ''}`);
  };
  const ready = async () => {
    for (let i = 0; i < 60; i++) {
      try { const r = await fetch(url, { signal: AbortSignal.timeout(1000) }); if (r.ok) return r.json(); } catch {}
      await new Promise(resolve => setTimeout(resolve, 500));
    }
    throw new Error('Native service never became ready');
  };
  try {
    run('install');
    assert.equal((await ready()).setup_required, true);
    const token = fs.readFileSync(path.join(data, 'setup-token'), 'utf8');
    run('status');
    run('stop');
    let stopped = false;
    for (let i = 0; i < 90; i++) {
      try { await fetch(url, { signal: AbortSignal.timeout(1000) }); } catch { stopped = true; break; }
      await new Promise(resolve => setTimeout(resolve, 500));
    }
    assert.equal(stopped, true, 'Service did not stop listening');
    run('start'); await ready();
    run('restart'); await ready();
    assert.equal(fs.readFileSync(path.join(data, 'setup-token'), 'utf8'), token, 'Restart changed the data directory');
    console.log(`Native ${platform} service install/status/stop/start/restart passed; paths with spaces, %, $, and & were preserved.`);
  } finally {
    const installed = platform === 'win32' ? spawnSync('sc.exe', ['query', 'Apptrail']).status === 0 : fs.existsSync(unit);
    if (installed) {
      const hadDatabase = fs.existsSync(path.join(data, 'apptrail.db'));
      run('uninstall');
      if (hadDatabase) assert.equal(fs.existsSync(path.join(data, 'apptrail.db')), true, 'Uninstall removed user data');
      if (platform !== 'win32') assert.equal(fs.existsSync(unit), false, 'Service file was not removed');
    }
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
