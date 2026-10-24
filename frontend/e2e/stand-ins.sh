# The stand-ins an E2E fixture runs in place of a real CLI. Sourced.
#
# e2e_stand_ins_first <dir>[:<dir>...]
#   Puts the directories first on PATH, in order, then checks that every
#   executable in them is the one PATH resolves, and fails, naming it, when
#   one is not. An empty list leaves PATH alone. Call it after the runtime
#   selection, which puts the selected SDK's bin — where an npm-installed
#   claude or codex lives — ahead of everything.
e2e_stand_ins_first() {
	[ -n "$1" ] || return 0
	PATH="$1:$PATH"
	export PATH
	e2e_stand_ins_ifs=$IFS
	IFS=:
	for e2e_stand_ins_dir in $1; do
		for e2e_stand_ins_file in "$e2e_stand_ins_dir"/*; do
			[ -f "$e2e_stand_ins_file" ] && [ -x "$e2e_stand_ins_file" ] || continue
			e2e_stand_ins_name=${e2e_stand_ins_file##*/}
			e2e_stand_ins_found=$(command -v "$e2e_stand_ins_name")
			if [ "$e2e_stand_ins_found" != "$e2e_stand_ins_file" ]; then
				IFS=$e2e_stand_ins_ifs
				echo "e2e: $e2e_stand_ins_name resolves to $e2e_stand_ins_found, not the stand-in $e2e_stand_ins_file" >&2
				return 1
			fi
		done
	done
	IFS=$e2e_stand_ins_ifs
}
