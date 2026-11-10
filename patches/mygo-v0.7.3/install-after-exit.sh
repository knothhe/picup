# Adapted from github.com/egoist/mygo v0.7.3 updater.go, MIT licensed.
# Copyright (c) 2026 MyGo contributors. See LICENSE in this directory.
# Keep MyGo's pid/target/new/work/open-arguments protocol.
if [ "$#" -lt 4 ]; then
    printf '%s\n' 'Missing update installation arguments.' >&2
    exit 2
fi
pid=$1 target=$2 new=$3 work=$4
shift 4

present() {
    [ -e "$1" ] || [ -L "$1" ]
}

directory_identity() {
    # Production is macOS; the GNU form also lets isolated shell fixtures run
    # on Linux. Never infer successful installation from path existence alone.
    if [ "$(uname -s)" = Darwin ]; then
        /usr/bin/stat -f '%d:%i' "$1" 2>/dev/null
    else
        /usr/bin/stat -c '%d:%i' "$1" 2>/dev/null
    fi
}

quote_path() {
    printf "'"
    printf '%s' "$1" | sed "s/'/'\\\\''/g"
    printf "'"
}

preserve_failure() {
    trap '' INT TERM
    old_location=
    old_nested_in_target=
    candidate_location=$new
    if [ -n "$old_identity" ]; then
        if [ "$(directory_identity "$work/old")" = "$old_identity" ]; then
            old_location=$work/old
        elif [ "$(directory_identity "$work/old/${target##*/}")" = "$old_identity" ]; then
            old_location=$work/old/${target##*/}
        elif [ "$(directory_identity "$target/old")" = "$old_identity" ]; then
            old_location=$target/old
            old_nested_in_target=1
        fi
    fi
    if [ -n "$new_identity" ] && [ "$(directory_identity "$new")" != "$new_identity" ] &&
       [ "$(directory_identity "$target/${new##*/}")" = "$new_identity" ]; then
        candidate_location=$target/${new##*/}
    fi
    {
        printf 'The update could not finish: %s\n\n' "$1"
        printf 'Original application: %s\n' "$target"
        if [ -n "$old_location" ]; then
            printf 'Old application backup: %s\n' "$old_location"
        else
            printf '%s\n' 'No verified old backup was found; inspect the original application path before moving anything.'
        fi
        printf 'Downloaded candidate: %s\n\n' "$candidate_location"
        printf '%s\n' 'Keep this directory until the original application has been recovered.'
        printf '%s\n\n' 'Resolve the filesystem error first. If the original path is occupied, inspect it and move that application aside before recovering. Do not overwrite an unknown installation.'
        if [ -n "$old_nested_in_target" ]; then
            printf '%s\n' 'The verified old application is nested inside the occupied target. Move it to a separate recovery location before moving that target aside, then restore it to the original application path.'
        elif [ -n "$old_location" ]; then
            printf 'Recovery command (only when the target is absent):\n'
            printf 'if [ ! -e '; quote_path "$target"
            printf ' ] && [ ! -L '; quote_path "$target"
            printf ' ]; then mv -n '; quote_path "$old_location"
            printf ' '; quote_path "$target"; printf '; fi\n'
        fi
    } > "$work/RECOVERY.txt"
    record_status=$?
    printf 'Update failed: %s\nRecovery files retained at: %s\n' "$1" "$work" >&2
    if [ "$record_status" -ne 0 ]; then
        printf '%s\n' 'Could not write RECOVERY.txt; retain the directory and its old application backup.' >&2
    fi
    exit 1
}

# Do not clean recovery files from signal or exit traps.
trap '' HUP
trap 'preserve_failure "Installation was interrupted."' INT TERM
while kill -0 "$pid" 2>/dev/null; do sleep 0.05; done

# BSD mv can place a source inside an existing destination directory. Refuse
# occupied paths before each move, including an unknown target that appeared
# after the running application exited.
if present "$work/old"; then
    preserve_failure 'The old application backup path is already occupied.'
fi
old_identity=$(directory_identity "$target")
new_identity=$(directory_identity "$new")
if [ -z "$old_identity" ] || [ -z "$new_identity" ]; then
    preserve_failure 'Could not identify the installed application or downloaded candidate.'
fi
if ! mv -n "$target" "$work/old" || present "$target" || ! present "$work/old" || [ "$(directory_identity "$work/old")" != "$old_identity" ]; then
    preserve_failure 'Could not move the installed application into its backup.'
fi

status=0
if present "$target" || ! mv -n "$new" "$target" || present "$new" || ! present "$target" || [ "$(directory_identity "$target")" != "$new_identity" ]; then
    if present "$target" || ! mv -n "$work/old" "$target" || present "$work/old" || ! present "$target" || [ "$(directory_identity "$target")" != "$old_identity" ]; then
        preserve_failure 'Could not install the new application or restore the old application.'
    fi
    printf '%s\n' 'Update failed; the old application was restored.' >&2
    status=1
fi

relaunch=
if [ -e "$work/relaunch" ]; then relaunch=1; fi
if ! rm -rf "$work"; then
    preserve_failure 'The application is in place, but staging cleanup failed.'
fi
if [ -n "$relaunch" ]; then exec /usr/bin/open "$@"; fi
exit "$status"
