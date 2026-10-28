"""Import an explicit CI identity into a temporary keychain, build, and clean up.

This entry point never creates certificates or changes the default keychain.
"""
import argparse
import base64
from contextlib import contextmanager
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import subprocess
import sys
import tempfile


PRIVATE_VARIABLES = ('MACOS_SIGNING_CERTIFICATE', 'MACOS_SIGNING_CERTIFICATE_PASSWORD',
                     'MACOS_NOTARY_KEY_BASE64', 'MACOS_NOTARY_KEY_ID', 'MACOS_NOTARY_ISSUER')
PIN_PATH = Path(__file__).resolve().parent.parent / 'config/macos-signing.json'


def repository_certificate_pin():
    """Read the distribution identity from reviewed source, never runner variables."""
    try:
        data = json.loads(PIN_PATH.read_text(encoding='utf-8'))
    except OSError:
        raise ValueError('CI Apple signing requires a readable, committed '
                         'config/macos-signing.json with the existing distribution '
                         'certificate SHA-256 pin.') from None
    except ValueError:
        raise ValueError('Invalid config/macos-signing.json: expected a JSON object.') from None
    if (not isinstance(data, dict) or set(data) != {'schema', 'certificateSHA256'}
            or type(data.get('schema')) is not int or data['schema'] != 1
            or not isinstance(data.get('certificateSHA256'), str)
            or not re.fullmatch(r'[a-f0-9]{64}', data['certificateSHA256'])):
        raise ValueError('Invalid config/macos-signing.json: expected only schema: 1 '
                         'and certificateSHA256 as 64 lowercase hexadecimal characters.')
    return data['certificateSHA256']


def run(arguments, capture=False):
    environment = {key:value for key,value in os.environ.items()
                   if key not in PRIVATE_VARIABLES and key != 'MYGO_UPDATER_PRIVATE_KEY'}
    result = subprocess.run(arguments, env=environment, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        # security/notarytool may include supplied credentials in diagnostics.
        raise ValueError(f'{Path(arguments[0]).name} failed ({result.returncode}); signing setup stopped.')
    return result.stdout.strip() if capture else None


@contextmanager
def signing_environment(environment=None):
    environment = dict(os.environ if environment is None else environment)
    certificate = environment.get('MACOS_SIGNING_CERTIFICATE', '')
    password = environment.get('MACOS_SIGNING_CERTIFICATE_PASSWORD', '')
    pin = environment.get('PICUP_MACOS_CERTIFICATE_SHA256', '').lower()
    if not certificate or not password or not re.fullmatch(r'[a-f0-9]{64}', pin):
        raise ValueError('CI Apple signing requires certificate, password and its public SHA-256 pin.')
    try:
        decoded = base64.b64decode(certificate, validate=True)
    except ValueError:
        raise ValueError('Invalid Base64 signing certificate.') from None
    if not decoded or len(decoded)>1024*1024:
        raise ValueError('Invalid signing certificate size.')
    notary_fields = [environment.get(key,'') for key in PRIVATE_VARIABLES[2:]]
    if any(notary_fields) and not all(notary_fields):
        raise ValueError('Notarization requires API key, key ID and issuer together.')
    original = shlex.split(run(['/usr/bin/security','list-keychains','-d','user'], capture=True))
    cleanup_errors = []
    with tempfile.TemporaryDirectory(prefix='picup-signing-',dir=environment.get('RUNNER_TEMP')) as temporary:
        directory = Path(temporary)
        directory.chmod(0o700)
        keychain = directory / 'signing.keychain-db'
        created = False
        # Register cleanup before writing private material or creating a keychain.
        try:
            p12 = directory / 'identity.p12'
            descriptor=os.open(p12,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
            with os.fdopen(descriptor,'wb') as stream:stream.write(decoded)
            keychain_password=secrets.token_urlsafe(32)
            run(['/usr/bin/security','create-keychain','-p',keychain_password,str(keychain)])
            created=True
            run(['/usr/bin/security','set-keychain-settings','-lut','21600',str(keychain)])
            run(['/usr/bin/security','unlock-keychain','-p',keychain_password,str(keychain)])
            run(['/usr/bin/security','import',str(p12),'-k',str(keychain),'-f','pkcs12','-P',password,'-T','/usr/bin/codesign'])
            run(['/usr/bin/security','set-key-partition-list','-S','apple-tool:,apple:,codesign:','-s','-k',keychain_password,str(keychain)])
            listing=run(['/usr/bin/security','find-identity','-p','codesigning',str(keychain)],capture=True)
            matches=set(re.findall(r'\b([0-9A-Fa-f]{40})\s+"([^"\n]+)"',listing))
            if len(matches)!=1:
                raise ValueError('CI keychain must contain exactly one codesigning identity.')
            identity,name=next(iter(matches))
            if not name.startswith('Developer ID Application:'):
                raise ValueError('CI Apple distribution requires a Developer ID Application certificate.')
            run(['/usr/bin/security','list-keychains','-d','user','-s',str(keychain),*original])
            child={key:value for key,value in environment.items() if key not in PRIVATE_VARIABLES}
            child['PICUP_MACOS_SIGNING_IDENTITY']=identity.upper()
            child['PICUP_MACOS_CERTIFICATE_SHA256']=pin
            if all(notary_fields):
                try:api_key=base64.b64decode(notary_fields[0],validate=True)
                except ValueError:raise ValueError('Invalid Base64 notary API key.') from None
                keyfile=directory/'notary.p8'
                descriptor=os.open(keyfile,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
                with os.fdopen(descriptor,'wb') as stream:stream.write(api_key)
                profile='picup-ci-notary'
                run(['/usr/bin/xcrun','notarytool','store-credentials',profile,'--key',str(keyfile),
                     '--key-id',notary_fields[1],'--issuer',notary_fields[2],'--keychain',str(keychain)])
                child['PICUP_MACOS_NOTARY_PROFILE']=profile
                child['PICUP_MACOS_NOTARY_KEYCHAIN']=str(keychain)
            yield child
        finally:
            try:run(['/usr/bin/security','list-keychains','-d','user','-s',*original])
            except (ValueError,OSError):cleanup_errors.append('restore keychain search list')
            if created or keychain.exists():
                try:run(['/usr/bin/security','delete-keychain',str(keychain)])
                except (ValueError,OSError):cleanup_errors.append('delete temporary keychain')
            # TemporaryDirectory independently removes P12/P8 and private files.
            if cleanup_errors:
                raise ValueError('CI signing cleanup failed: '+', '.join(cleanup_errors))


def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command',nargs=argparse.REMAINDER)
    arguments=parser.parse_args(argv).command
    if arguments[:1]==['--']:arguments=arguments[1:]
    if not arguments:parser.error('a build command is required after --')
    try:
        environment = dict(os.environ)
        environment['PICUP_MACOS_CERTIFICATE_SHA256'] = repository_certificate_pin()
        with signing_environment(environment) as child:
            return subprocess.run(arguments,env=child).returncode
    except (ValueError,OSError) as error:
        print(f'CI signing: {error}',file=sys.stderr)
        return 1


if __name__=='__main__':
    sys.exit(main())
