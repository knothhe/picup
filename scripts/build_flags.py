"""Keep Picup's compile-time data namespace consistent across Go/MyGo builds."""
import os
import re
import sys


class BuildFlagsError(ValueError):
    pass


def go_flag_words(value):
    # Match cmd/internal/quoted.Split: only quotes around a whole field count;
    # quoted fields have no shell unescaping. Preserve the original GOFLAGS for
    # every option other than the final merged -tags= flag appended below.
    words = []
    while value:
        value = value.lstrip(' \t\r\n')
        if not value:
            break
        if value[0] in "\"'":
            quote = value[0]
            end = value.find(quote, 1)
            if end < 0:
                raise BuildFlagsError('GOFLAGS contains an unterminated quoted field')
            words.append(value[1:end])
            value = value[end + 1:]
        else:
            match = re.search(r'[ \t\r\n]', value)
            end = match.start() if match else len(value)
            words.append(value[:end])
            value = value[end:]
    return words


def build_environment(tags='', formal=False):
    environment = dict(os.environ)
    environment.pop('MYGO_UPDATER_PRIVATE_KEY', None)
    target_os = environment.get('GOOS') or ('darwin' if sys.platform == 'darwin' else sys.platform)
    if target_os == 'darwin':
        if environment.get('CGO_ENABLED', '1') != '1':
            raise BuildFlagsError('macOS Picup builds require CGO_ENABLED=1 for R2 Keychain storage; '
                                  'unset CGO_ENABLED or set it to 1, and install Xcode Command Line Tools')
        # MyGo otherwise chooses CGO_ENABLED=0 even on a native Mac. Both CLI
        # and GUI must use the SDK's native Keychain implementation.
        environment['CGO_ENABLED'] = '1'
    raw = environment.get('GOFLAGS', '')
    for name in ('ClientID', 'RedirectURL'):
        raw = raw.replace('github.com/knothhe/picup/internal/cloudflare.' + name + '=',
                          'github.com/knothhe/picup/internal/r2oauth.' + name + '=')
    # Select flags from the explicit build environment. Also set an empty value
    # so an implicit per-user `go env -w GOFLAGS` cannot change the data channel.
    environment['GOFLAGS'] = raw
    selected = []
    for word in go_flag_words(raw):
        name, separator, value = word.lstrip('-').partition('=')
        if name == 'tags':
            if not separator:
                raise BuildFlagsError('GOFLAGS must use -tags=VALUE')
            selected.extend(re.split(r'[, \t\r\n]+', value))
    selected.extend(re.split(r'[, \t\r\n]+', tags))
    selected = list(dict.fromkeys(tag for tag in selected if tag))
    if any(not re.fullmatch(r'[A-Za-z0-9_.]+', tag) for tag in selected):
        raise BuildFlagsError('Build tags must contain letters, numbers, dots or underscores')
    if formal and 'picup_dev' in selected:
        raise BuildFlagsError('Formal Picup packages must not use picup_dev; remove it from GOFLAGS')
    if selected:
        environment['GOFLAGS'] = raw.rstrip() + (' ' if raw.strip() else '') + '-tags=' + ','.join(selected)
    return environment
