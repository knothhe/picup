#!/usr/bin/env python3
"""Run Go with the pinned, temporary MyGo updater recovery patch."""
import subprocess
import sys

sys.dont_write_bytecode = True
from mygo_patch import PatchError, go_command, patched_environment, reject_conflicting_flags


def main():
    try:
        reject_conflicting_flags(sys.argv[1:])
        with patched_environment() as environment:
            return subprocess.run([go_command(environment), *sys.argv[1:]], env=environment).returncode
    except (PatchError, OSError) as error:
        print(f'ERROR: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    status = main()
    # Preserve subprocess termination by signal using the conventional shell
    # status, rather than letting Python wrap a negative exit into an unrelated code.
    sys.exit(status if status >= 0 else 128 - status)
