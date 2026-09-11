const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { createHash } = require('node:crypto');
const { spawnSync } = require('node:child_process');

test('installer verifies releases, handles platforms, and preserves existing installations on failure', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'apptrail-installer-test-'));
  try {
    const tools = path.join(root, 'tools');
    const assets = path.join(root, 'assets');
    const dest = path.join(root, 'install with spaces');
    fs.mkdirSync(tools); fs.mkdirSync(assets); fs.mkdirSync(dest);
    fs.writeFileSync(path.join(assets, 'apptrail'), '#!/bin/sh\nprintf "Apptrail v1.2.3\\n"\n', { mode: 0o755 });
    const manifest = [];
    for (const platform of ['linux_amd64', 'linux_arm64', 'darwin_amd64', 'darwin_arm64']) {
      const name = `apptrail_v1.2.3_${platform}.tar.gz`;
      const tar = spawnSync('tar', ['-czf', path.join(assets, name), '-C', assets, 'apptrail']);
      assert.equal(tar.status, 0, tar.stderr?.toString());
      manifest.push(`${createHash('sha256').update(fs.readFileSync(path.join(assets, name))).digest('hex')}  ${name}`);
    }
    fs.writeFileSync(path.join(assets, 'SHA256SUMS'), manifest.join('\n') + '\n');
    fs.writeFileSync(path.join(tools, 'uname'), '#!/bin/sh\nif [ "$1" = -s ]; then printf "%s\\n" "$TEST_OS"; else printf "%s\\n" "$TEST_ARCH"; fi\n', { mode: 0o755 });
    fs.writeFileSync(path.join(tools, 'curl'), `#!${process.execPath}
const fs=require('node:fs'),path=require('node:path');
const a=process.argv.slice(2),url=a.at(-1);
if(!url.startsWith('https://github.com/samishal1998/apptrail/releases/'))process.exit(2);
if(url.endsWith('/latest')){process.stdout.write('https://github.com/samishal1998/apptrail/releases/tag/v1.2.3');process.exit(0)}
const file=path.join(process.env.TEST_ASSETS,url.split('/').at(-1));
if(!fs.existsSync(file))process.exit(22);
fs.copyFileSync(file,a[a.indexOf('--output')+1]);
`, { mode: 0o755 });
    const installer = fs.readFileSync(path.join(__dirname, '..', 'install.sh'));
    function run(args = [], env = {}) {
      return spawnSync('sh', ['-s', '--', '--dir', dest, ...args], {
        input: installer, encoding: 'utf8',
        env: { ...process.env, PATH: `${tools}:${process.env.PATH}`, TEST_ASSETS: assets, TEST_OS: 'Linux', TEST_ARCH: 'x86_64', ...env },
      });
    }
    for (const [system, arch] of [['Linux', 'x86_64'], ['Linux', 'aarch64'], ['Darwin', 'x86_64'], ['Darwin', 'arm64']]) {
      const result = run(['--version', 'v1.2.3'], { TEST_OS: system, TEST_ARCH: arch });
      assert.equal(result.status, 0, result.stderr);
      assert.equal(spawnSync(path.join(dest, 'apptrail'), ['--version'], { encoding: 'utf8' }).stdout.trim(), 'Apptrail v1.2.3');
    }
    assert.equal(run().status, 0, 'latest release resolution failed');
    const before = fs.readFileSync(path.join(dest, 'apptrail'));
    fs.appendFileSync(path.join(assets, 'apptrail_v1.2.3_linux_amd64.tar.gz'), 'tampered');
    const badChecksum = run(['--version', 'v1.2.3']);
    assert.notEqual(badChecksum.status, 0);
    assert.match(badChecksum.stderr, /Checksum mismatch/);
    assert.deepEqual(fs.readFileSync(path.join(dest, 'apptrail')), before);
    assert.notEqual(run(['--version', '../../bad']).status, 0, 'invalid version accepted');
    assert.notEqual(run(['--version', 'v9.9.9']).status, 0, 'missing asset accepted');
    assert.notEqual(run(['--version', 'v1.2.3'], { TEST_ARCH: 'armv7l' }).status, 0, 'unsupported architecture accepted');
    assert.notEqual(run(['--unknown']).status, 0, 'unknown argument accepted');
    fs.unlinkSync(path.join(dest, 'apptrail'));
    fs.symlinkSync(path.join(assets, 'apptrail'), path.join(dest, 'apptrail'));
    assert.notEqual(run(['--version', 'v1.2.3']).status, 0, 'destination symlink accepted');
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});
