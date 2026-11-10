"""Build against an isolated, patched copy of MyGo v0.7.3 for DMG updates.

Upstream MyGo v0.7.3 publishes and installs tar.gz updates. Picup ships one
macOS format, the DMG, so the private copy (a) signs the final DMG bytes in
place and points update-<target>.json at it, (b) downloads update.dmg and asks
the application's UpdateSource to Extract it, and (c) uses the reviewed
install-after-exit helper in patches/mygo-v0.7.3. The module cache is never
modified, and drift from the pinned source hashes fails closed.
"""
from __future__ import annotations

from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import subprocess
import shutil
import tempfile


ROOT = Path(__file__).resolve().parents[1]
MODULE = 'github.com/egoist/mygo'
VERSION = 'v0.7.3'
UPDATER_SHA256 = '590c0e03913ec94c1e29c478ae62d348f5805da7d54f7c8dfb6a89b02b778219'
HELPER_PATH = ROOT / 'patches/mygo-v0.7.3/install-after-exit.sh'
UPDATES_SHA256 = '49d47cc07e0e593abbdf9ec8bdd57bbefb6c08d14306ef79c62d030e13c96ddf'
ORIGINAL_DOWNLOAD = 'archive := filepath.Join(work, "update.tar.gz")'
ORIGINAL_EXTRACT = '''err = update.ExtractArchive(f, unpacked)
	f.Close()'''
DMG_EXTRACT = '''// The application source owns the authenticated DMG extraction contract.
	if len(sources) == 0 { f.Close(); return errors.New("DMG updates require an application extraction source") }
	extractor, ok := sources[0].(interface { Extract(context.Context, string, string, string) error })
	if !ok { f.Close(); return errors.New("DMG updates require an application extraction source") }
	f.Close()
	err = extractor.Extract(ctx, m.URL, archive, unpacked)'''
ORIGINAL_WRITE = '''name := c.slug() + "-" + c.Version + "-" + target + ".tar.gz"
	archive := filepath.Join(stage, name)
	size, sig, err := writeSigned(archive, key, func(w io.Writer) error {
		return update.WriteArchive(w, stage, entries)
	})'''
DMG_WRITE = '''name := dmgFileName(c, strings.TrimPrefix(target, "darwin-"))
	archive := filepath.Join(stage, name)
	// Sign the final DMG in place, after signing/notarization/stapling.
	f, err := os.Open(archive)
	if err != nil { return nil, err }
	sum := sha256.New()
	size, err := io.Copy(sum, f)
	closeErr := f.Close()
	if err != nil { return nil, err }
	if closeErr != nil { return nil, closeErr }
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(key, sum.Sum(nil)))'''


def patch_dmg(updater, updates):
    # Both source files are pinned before any transformation. Exact blocks keep
    # failures closed if the selected dependency is changed.
    replacements = [
        (ORIGINAL_DOWNLOAD, 'archive := filepath.Join(work, "update.dmg")'),
        (ORIGINAL_EXTRACT, DMG_EXTRACT),
        ('up.current, target, progress)', 'up.current, target, progress, up.source)'),
        ('progress func(downloaded, total int64)) error {\n\tpub, err := update.ParsePublicKey(key)',
         'progress func(downloaded, total int64), sources ...UpdateSource) error {\n\tpub, err := update.ParsePublicKey(key)'),
    ]
    for before, after in replacements:
        if updater.count(before.encode()) != 1:
            raise PatchError('The pinned DMG updater block is not uniquely identifiable.')
        updater = updater.replace(before.encode(), after.encode(), 1)
    if updates.count(ORIGINAL_WRITE.encode()) != 1:
        raise PatchError('The pinned DMG signing block is not uniquely identifiable.')
    updates = updates.replace(ORIGINAL_WRITE.encode(), DMG_WRITE.encode(), 1)
    updates = updates.replace(b'files := []string{archive}', b'files := []string{}', 1)
    return updater, updates

ORIGINAL_HELPER = '''pid=$1 target=$2 new=$3 work=$4
shift 4
trap '' HUP
while kill -0 "$pid" 2>/dev/null; do sleep 0.05; done
if mv "$target" "$work/old"; then
\tmv "$new" "$target" || mv "$work/old" "$target"
fi
relaunch=
if [ -e "$work/relaunch" ]; then relaunch=1; fi
rm -rf "$work"
if [ -n "$relaunch" ]; then exec /usr/bin/open "$@"; fi
'''


class PatchError(RuntimeError):
    pass


def go_command(env=None):
    return (os.environ if env is None else env).get('GO') or 'go'


def _go_output(arguments, env):
    try:
        result = subprocess.run([go_command(env), *arguments], env=env, text=True, capture_output=True)
    except OSError as error:
        raise PatchError('Cannot run the Go updater patch preflight.') from error
    if result.returncode:
        raise PatchError(f'Go updater patch preflight failed (exit {result.returncode}).')
    return result.stdout.strip()


def _split_flags(value):
    # Match cmd/internal/quoted.Split: quotes only delimit an entire field,
    # and there is no shell expansion or unescaping inside quoted fields.
    fields = []
    while value:
        value = value.lstrip(' \t\r\n')
        if not value:
            break
        if value[0] in ('\'', '"'):
            quote, rest = value[0], value[1:]
            end = rest.find(quote)
            if end < 0:
                raise PatchError('Unterminated quoted GOFLAGS field.')
            fields.append(rest[:end])
            value = rest[end + 1:]
        else:
            end = next((index for index, char in enumerate(value) if char in ' \t\r\n'), len(value))
            fields.append(value[:end])
            value = value[end:]
    return fields


def _quote_flag(value):
    if not any(char in value for char in ' \t\r\n\'"'):
        return value
    if "'" not in value:
        return "'" + value + "'"
    if '"' not in value:
        return '"' + value + '"'
    raise PatchError('GOFLAGS argument contains both quote types and cannot be represented by Go.')


def reject_conflicting_flags(flags):
    for flag in flags:
        if flag == '--':
            break
        if flag.startswith('-') and flag.lstrip('-').split('=', 1)[0] in ('overlay', 'modfile'):
            raise PatchError('An existing Go overlay or alternate modfile conflicts with the required MyGo updater patch.')


def _read_json(arguments, environment, description):
    try:
        value = json.loads(_go_output(arguments, environment))
    except (ValueError, TypeError) as error:
        raise PatchError(f'Go returned invalid {description}.') from error
    if not isinstance(value, dict):
        raise PatchError(f'Go returned invalid {description}.')
    return value


def _temporary_workspace(workspace, mirror, environment):
    original = _go_output(['env', 'GOWORK'], environment)
    edits = []
    if original and original != 'off':
        original = Path(original)
        if not original.is_absolute():
            raise PatchError('The original Go workspace path is not absolute.')
        data = _read_json(['work', 'edit', '-json', str(original)], environment, 'workspace metadata')
        workspace.write_bytes(original.read_bytes())
        base = original.parent
        for use in data.get('Use') or []:
            path = use['DiskPath']
            absolute = os.path.abspath(base / path)
            edits.extend(['-dropuse=' + path, '-use=' + absolute])
        for replacement in data.get('Replace') or []:
            new = replacement['New']
            if not new.get('Version') and not Path(new['Path']).is_absolute():
                old = replacement['Old']
                left = old['Path'] + ('@' + old['Version'] if old.get('Version') else '')
                edits.append('-replace=' + left + '=' + os.path.abspath(base / new['Path']))
    else:
        main = _read_json(['list', '-m', '-json'], environment, 'main module metadata')
        path, version = main.get('Dir'), main.get('GoVersion')
        if not isinstance(path, str) or not Path(path).is_absolute() or not isinstance(version, str):
            raise PatchError('The main module has no absolute directory or Go version.')
        workspace.write_text('go ' + version + '\n')
        edits.append('-use=' + path)
    edits.append('-replace=' + MODULE + '=' + str(mirror))
    _go_output(['work', 'edit', *edits, str(workspace)], environment)


@contextmanager
def patched_environment(env=None):
    environment = dict(os.environ if env is None else env)
    configured = environment['GOFLAGS'] if 'GOFLAGS' in environment else _go_output(['env', 'GOFLAGS'], environment)
    flags = _split_flags(configured)
    reject_conflicting_flags(flags)
    environment['GOFLAGS'] = ' '.join(_quote_flag(flag) for flag in flags)
    module = _read_json(['list', '-m', '-json', MODULE], environment, 'MyGo module metadata')
    if not isinstance(module, dict) or module.get('Path') != MODULE or module.get('Version') != VERSION or module.get('Replace') is not None:
        raise PatchError('The updater patch requires the original, unreplaced MyGo v0.7.3 module.')
    directory = module.get('Dir')
    if not directory:
        downloaded = _read_json(['mod', 'download', '-json', MODULE + '@' + VERSION], environment, 'downloaded MyGo module metadata')
        if not isinstance(downloaded, dict) or downloaded.get('Path') != MODULE or downloaded.get('Version') != VERSION or downloaded.get('Replace') is not None:
            raise PatchError('Downloaded MyGo metadata differs from the selected v0.7.3 module.')
        directory = downloaded.get('Dir')
    if not isinstance(directory, str) or not Path(directory).is_absolute():
        raise PatchError('MyGo module has no absolute source directory.')
    original = Path(directory) / 'updater.go'
    try:
        source = original.read_bytes()
        helper = HELPER_PATH.read_bytes()
        updates = (original.parent / 'cmd/mygo/updates.go').read_bytes()
    except OSError as error:
        raise PatchError('Cannot read MyGo source or the reviewed updater helper patch.') from error
    if hashlib.sha256(source).hexdigest() != UPDATER_SHA256:
        raise PatchError('MyGo updater source differs from its pinned v0.7.3 hash; review the patch before building.')
    if hashlib.sha256(updates).hexdigest() != UPDATES_SHA256:
        raise PatchError('MyGo update packaging source differs from its pinned v0.7.3 hash.')
    old_block = b'const installAfterExit = `' + ORIGINAL_HELPER.encode() + b'`'
    if source.count(old_block) != 1:
        raise PatchError('The pinned MyGo helper block is not uniquely identifiable.')
    if not helper or b'`' in helper or b'\x00' in helper or not helper.endswith(b'\n'):
        raise PatchError('The updater helper patch is not a newline-terminated Go raw string.')
    replacement = b'const installAfterExit = `' + helper + b'`'
    patched = source.replace(old_block, replacement, 1)
    patched, patched_updates = patch_dmg(patched, updates)
    try:
        with tempfile.TemporaryDirectory(prefix='picup-mygo-patch-') as temporary:
            directory = Path(temporary)
            mirror = directory / 'mygo'
            shutil.copytree(original.parent, mirror)
            # Module cache directories are read-only. The private copy must be
            # writable so TemporaryDirectory can always remove it afterwards.
            for parent, _, _ in os.walk(mirror):
                Path(parent).chmod(0o700)
            patched_source = mirror / 'updater.go'
            patched_source.chmod(0o600)
            patched_source.write_bytes(patched)
            packaging = mirror / 'cmd/mygo/updates.go'
            packaging.chmod(0o600)
            packaging.write_bytes(patched_updates)
            workspace = directory / 'go.work'
            _temporary_workspace(workspace, mirror, environment)
            environment['GOWORK'] = str(workspace)
            yield environment
    except OSError as error:
        raise PatchError('The temporary MyGo source copy could not be created, used, or removed.') from error
