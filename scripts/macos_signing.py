"""Use an explicitly selected existing macOS identity; never create one."""
from dataclasses import dataclass
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tempfile


class SigningError(ValueError):
    pass


def command(*arguments, capture=False, diagnostics=False):
    environment = dict(os.environ)
    environment.pop('MYGO_UPDATER_PRIVATE_KEY', None)
    result = subprocess.run(arguments, env=environment, text=True,
                            stdout=subprocess.PIPE if capture else None,
                            stderr=subprocess.PIPE)
    if result.returncode:
        raise SigningError(f'{Path(arguments[0]).name} failed ({result.returncode}): '
                           + (result.stderr or '').strip())
    return ((result.stdout or '') + ('\n'+(result.stderr or '') if diagnostics else '')).strip()


@dataclass(frozen=True)
class Signing:
    identity_hash: str
    name: str
    profile: str = ''
    keychain: str = ''
    certificate_sha256: str = ''


def configuration(environment=None):
    environment = os.environ if environment is None else environment
    identity = environment.get('PICUP_MACOS_SIGNING_IDENTITY', '').strip()
    profile = environment.get('PICUP_MACOS_NOTARY_PROFILE', '').strip()
    keychain = environment.get('PICUP_MACOS_NOTARY_KEYCHAIN', '').strip()
    pin = environment.get('PICUP_MACOS_CERTIFICATE_SHA256', '').strip().lower()
    if not identity:
        if profile or keychain or pin:
            raise SigningError('Notary/profile/certificate pin requires PICUP_MACOS_SIGNING_IDENTITY.')
        if 'PICUP_MACOS_SIGNING_IDENTITY' in environment:
            raise SigningError('Explicit PICUP_MACOS_SIGNING_IDENTITY is empty.')
        return None
    if identity == '-':
        raise SigningError('Explicit signing requires a real identity; omit the variable for ad-hoc builds.')
    if pin and not re.fullmatch(r'[a-f0-9]{64}', pin):
        raise SigningError('PICUP_MACOS_CERTIFICATE_SHA256 must contain 64 hexadecimal characters.')
    listing = command('/usr/bin/security', 'find-identity', '-v', '-p', 'codesigning', capture=True)
    identities = set(re.findall(r'\b([0-9A-Fa-f]{40})\s+"([^"\n]+)"', listing))
    matches = [(digest.upper(), name) for digest, name in identities
               if digest.lower() == identity.lower() or name == identity]
    if len(matches) != 1:
        raise SigningError('Signing identity must match exactly one valid existing codesigning identity.')
    digest, name = matches[0]
    if profile and not name.startswith('Developer ID Application:'):
        raise SigningError('Apple notarization requires a Developer ID Application identity.')
    if keychain and not profile:
        raise SigningError('PICUP_MACOS_NOTARY_KEYCHAIN requires PICUP_MACOS_NOTARY_PROFILE.')
    return Signing(digest, name, profile, keychain, pin)


def verify_application(app, signing, executable='Picup', cli='picup'):
    """Verify signature integrity and the selected certificate on all owned code."""
    app = Path(app)
    command('/usr/bin/codesign', '--verify', '--deep', '--strict', str(app))
    targets = [app, app / 'Contents/MacOS' / executable, app / 'Contents/Resources' / cli]
    with tempfile.TemporaryDirectory(prefix='picup-signature-') as temporary:
        for index, target in enumerate(targets):
            prefix = str(Path(temporary) / f'certificate-{index}-')
            command('/usr/bin/codesign', '--display', '--extract-certificates', prefix, str(target))
            certificate = Path(prefix + '0')
            if not certificate.is_file():
                raise SigningError(f'{target.name}: missing signing certificate.')
            der = certificate.read_bytes()
            if hashlib.sha1(der).hexdigest().upper() != signing.identity_hash:
                raise SigningError(f'{target.name}: signed by a different identity.')
            if signing.certificate_sha256 and hashlib.sha256(der).hexdigest() != signing.certificate_sha256:
                raise SigningError(f'{target.name}: certificate does not match its SHA-256 pin.')
            verify_designated_requirement(target,signing,executable,index==2)
    if signing.profile:
        command('/usr/bin/xcrun', 'stapler', 'validate', str(app))
        command('/usr/sbin/spctl', '--assess', '--type', 'execute', str(app))


def verify_designated_requirement(target,signing,executable,is_cli=False):
    metadata=command('/usr/bin/codesign','--display','--verbose=4',str(target),capture=True,diagnostics=True)
    identifiers=re.findall(r'^Identifier=(.+)$',metadata,re.MULTILINE)
    bundle_id='com.knothhe.picup.dev' if executable=='Picup Dev' else 'com.knothhe.picup'
    allowed={'a.out','picup-dev' if executable=='Picup Dev' else 'picup',bundle_id+'.cli'} if is_cli else {bundle_id}
    if len(identifiers)!=1 or identifiers[0] not in allowed:
        raise SigningError(f'{target.name}: unexpected signed code identifier.')
    identifier=identifiers[0]
    displayed=command('/usr/bin/codesign','--display','--requirements','-',str(target),capture=True,diagnostics=True)
    matches=re.findall(r'^(?:#\s*)?designated => (.+)$',displayed,re.MULTILINE)
    if len(matches)!=1:
        raise SigningError(f'{target.name}: missing or ambiguous designated requirement.')
    normalize=lambda text:' '.join(re.sub(r'/\*.*?\*/','',text).split())
    requirement=normalize(matches[0])
    requirement=re.sub(r'H"([0-9a-fA-F]+)"',lambda match:'H"'+match[1].upper()+'"',requirement)
    prefix='identifier "'+identifier+'" and '
    if signing.name.startswith('Developer ID Application:'):
        team=re.search(r'\(([A-Z0-9]{10})\)$',signing.name)
        if not team:raise SigningError('Developer ID identity has no unambiguous Team ID.')
        expected={prefix+'anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] and '
                  'certificate leaf[field.1.2.840.113635.100.6.1.13] and certificate leaf[subject.OU] = "'+team[1]+'"'}
    else:
        expected={prefix+f'{anchor} H"{signing.identity_hash}"' for anchor in
                  ('anchor','certificate leaf =','certificate root =')}
    if requirement not in expected:
        raise SigningError(f'{target.name}: designated requirement is not the expected stable certificate identity.')
    command('/usr/bin/codesign','--verify','--strict','--test-requirement',
            f'identifier "{identifier}" and certificate leaf = H"{signing.identity_hash}"',str(target))
