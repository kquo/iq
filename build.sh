#!/usr/bin/env bash
# build.sh — self-contained build / release / release-prep tooling.
#
# All logic lives here as focused functions behind a guarded main.
# Targets Bash 3.2+ (macOS system bash): no associative arrays, mapfile,
# ${var^^}, or &>>.
#
# Dispatch:
#   ./build.sh [target ...] [-v|--verbose]      build + validate
#   ./build.sh prep [flags] vX.Y.Z "message"    stage a release
#   ./build.sh vX.Y.Z "message"                 run the release
set -euo pipefail

_rel_tag_for_recovery=''
_staticcheck_path=''
_git_err=''
_prep_warning=''
_prep_cl_err=''
_prep_bump_err=''
_prep_ie_err=''
_prep_vtargets=''
_prep_ctargets=''
_prep_owned_dir=''
_build_owned_dir=''
_build_install_temps=()

# ── color ────────────────────────────────────────────────────────────────────
# A sequence is emitted only when color is both enabled (NO_COLOR unset, TERM != dumb, stdout a TTY) and 256-color capable (COLORTERM
# truecolor/24bit, or TERM containing 256color). Computed once. The TTY signal
# is injectable via GOVNA_FORCE_TTY (1/0) for tests, since no PTY is used.
_color_init() {
  _color_on=1
  [ -n "${NO_COLOR:-}" ] && _color_on=0
  [ "${TERM:-}" = "dumb" ] && _color_on=0
  if [ -n "${GOVNA_FORCE_TTY:-}" ]; then
    [ "${GOVNA_FORCE_TTY}" = "1" ] || _color_on=0
  elif [ ! -t 1 ]; then
    _color_on=0
  fi
  _color256=0
  case "${COLORTERM:-}" in truecolor | 24bit) _color256=1 ;; esac
  case "${TERM:-}" in *256color*) _color256=1 ;; esac
  return 0
}

_wrap() { # $1=sgr-code $2=text
  if [ "$_color_on" = 1 ] && [ "$_color256" = 1 ]; then
    printf '\033[%sm%s\033[0m' "$1" "$2"
  else
    printf '%s' "$2"
  fi
}

yel7() { _wrap '38;5;227' "$1"; }
yel5() { _wrap '38;5;220' "$1"; }
grn3() { _wrap '38;5;34' "$1"; }
grn5() { _wrap '38;5;46' "$1"; }
gra5() { _wrap '38;5;245' "$1"; }
cya4() { _wrap '38;5;44' "$1"; }
red3() { _wrap '38;5;124' "$1"; }
whi5() { _wrap '38;5;231' "$1"; }

# bold rewrites every inner reset so the attribute survives nested color, then
# wraps. Quoted pattern => literal match (no glob).
bold() {
  if [ "$_color_on" = 1 ] && [ "$_color256" = 1 ]; then
    local reset bold1
    reset=$(printf '\033[0m')
    bold1=$(printf '\033[1m')
    local s=${1//"$reset"/"$reset$bold1"}
    printf '\033[1m%s\033[0m' "$s"
  else
    printf '%s' "$1"
  fi
}

# ── usage formatting (mirrors color.FormatUsage) ─────────────────────────────
# _emit_usage_line FLAG DESC — flag column padded to 38, else two spaces.
_emit_usage_line() {
  local flag="$1" desc="$2"
  local col=$((2 + ${#flag}))
  local pad
  if [ "$col" -lt 38 ]; then
    pad=$(printf '%*s' $((38 - col)) '')
  else
    pad='  '
  fi
  printf '  %s%s%s\n' "$flag" "$pad" "$desc"
}

# ── shared run helpers (mirror buildtool) ────────────────────────────────────
# run_streaming COLORFN cmd args... — echo "    <colored cmd>" then run it live.
# On failure, emit the tool's own context "<cmd...> failed: exit status N" to
# stderr (mirrors buildtool runStreaming; only the outer `go run` noise is
# dropped, never the tool's context) and return 1.
run_streaming() {
  local colorfn="$1"
  shift
  printf '    %s\n' "$("$colorfn" "$*")"
  local cmd_str="$*" rc=0
  "$@" || rc=$?
  if [ "$rc" -ne 0 ]; then
    printf '%s failed: exit status %d\n' "$cmd_str" "$rc" >&2
    return 1
  fi
}

# run_streaming_in_dir DIR COLORFN cmd... — like run_streaming with "(in DIR)".
run_streaming_in_dir() {
  local dir="$1" colorfn="$2"
  shift 2
  printf '    %s (in %s)\n' "$("$colorfn" "$*")" "$dir"
  local cmd_str="$*" rc=0
  "$@" || rc=$?
  if [ "$rc" -ne 0 ]; then
    printf '%s failed: exit status %d\n' "$cmd_str" "$rc" >&2
    return 1
  fi
}

# write_indented — indent each line by four spaces; red-highlight FAIL lines.
write_indented() {
  local line
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
    *FAIL*) printf '    %s\n' "$(red3 "$line")" ;;
    *) printf '    %s\n' "$line" ;;
    esac
  done
}

# _is_blank STR -> 0 when STR is empty or only whitespace.
_is_blank() { [ -z "$(printf '%s' "$1" | tr -d '[:space:]')" ]; }

# _trim STR -> STR with leading/trailing whitespace removed (≈ strings.TrimSpace).
_trim() {
  local s="$1"
  s="${s#"${s%%[![:space:]]*}"}"
  s="${s%"${s##*[![:space:]]}"}"
  printf '%s' "$s"
}

# _byte_len STR -> number of bytes (Go len() counts bytes, not runes).
_byte_len() { LC_ALL=C printf '%s' "$1" | LC_ALL=C wc -c | tr -d ' '; }

_shell_quote() { # $1=single shell argument
  local value="$1"
  value=${value//\'/\'\\\'\'}
  printf "'%s'" "$value"
}

_validate_release_message() { # $1=prefix $2=message
  local prefix="$1" message="$2" mlen
  if [ -z "$message" ]; then
    printf '%s: message must be non-empty\n' "$prefix" >&2
    return 1
  fi
  mlen=$(_byte_len "$message")
  if [ "$mlen" -gt 80 ]; then
    printf '%s: message must be 80 bytes or fewer (got %d)\n' "$prefix" "$mlen" >&2
    return 1
  fi
  case "$message" in
  *'|'*)
    printf '%s: message must fit one Markdown table cell and cannot contain |\n' "$prefix" >&2
    return 1
    ;;
  *$'\n'* | *$'\r'*)
    printf '%s: message must be one line\n' "$prefix" >&2
    return 1
    ;;
  esac
}

# _go_quote STR -> Go %q rendering (strconv.Quote), byte-safe. Escapes \ and ",
# the named control escapes \a \b \t \n \v \f \r, and any other control byte
# (<0x20) or DEL (0x7f) as \xHH; printable ASCII and high bytes pass through.
# Processed in LC_ALL=C so iteration is per byte. NUL cannot occur in a CLI
# argument, so byte 0 need not be handled.
#
# Bounded output-parity exception: invalid-UTF-8 high bytes
# (e.g. 0xff) and non-printable Unicode runes (e.g. U+200B) are passed through
# rather than \xHH/\uXXXX-escaped, because full strconv.Quote Unicode
# classification is not portable in Bash 3.2. This affects only the %q *display*
# surfaces (release-message line, commit-plan line, error messages); the raw
# message bytes handed to `git commit -m` are never quoted and are unchanged.
_go_quote() {
  printf '%s' "$1" | LC_ALL=C awk '
    BEGIN { for (i = 1; i < 256; i++) ord[sprintf("%c", i)] = i; printf "\"" }
    { if (NR > 1) printf "\\n"
      n = length($0)
      for (i = 1; i <= n; i++) {
        c = substr($0, i, 1); b = ord[c]
        if (c == "\\") printf "\\\\"
        else if (c == "\"") printf "\\\""
        else if (b == 7) printf "\\a"
        else if (b == 8) printf "\\b"
        else if (b == 9) printf "\\t"
        else if (b == 10) printf "\\n"
        else if (b == 11) printf "\\v"
        else if (b == 12) printf "\\f"
        else if (b == 13) printf "\\r"
        else if (b < 32 || b == 127) printf "\\x%02x", b
        else printf "%s", c
      }
    }
    END { printf "\"" }
  '
}

# ════════════════════════════════════════════════════════════════════════════
# Build pipeline
# ════════════════════════════════════════════════════════════════════════════

build_usage() {
  printf '%s %s\n' "$(bold "$(whi5 'Usage:')")" 'build [target ...] [-v|--verbose]'
  _emit_usage_line '-v, --verbose' 'run go test in verbose mode'
  _emit_usage_line '-h, -?, --help' 'show this help'
  printf '\n%s\n' 'When targets are specified, validation (vet, fmt, test, staticcheck) runs
only against those cmd packages. To validate the full repo, run with no targets.'
}

_failure() { printf '%s\n' "$1" >&2; }

_cleanup_build_owned() {
  local cleanup_failed=0 path
  for path in "${_build_install_temps[@]+"${_build_install_temps[@]}"}"; do
    [ -n "$path" ] || continue
    if { [ -e "$path" ] || [ -L "$path" ]; } && ! rm -f -- "$path"; then
      printf 'build: cleanup failed for owned path %s\n' "$path" >&2
      cleanup_failed=1
    fi
  done
  _build_install_temps=()
  if [ -n "${_build_owned_dir:-}" ] && [ -d "$_build_owned_dir" ] && ! rm -rf -- "$_build_owned_dir"; then
    printf 'build: cleanup failed for owned path %s\n' "$_build_owned_dir" >&2
    cleanup_failed=1
  fi
  _build_owned_dir=''
  return "$cleanup_failed"
}

_build_signal_exit() { # $1=conventional signal exit status
  local status="$1"
  trap - EXIT HUP INT TERM
  if ! _cleanup_build_owned; then :; fi
  exit "$status"
}

# build_run VERBOSE TARGETS... — targets empty => whole repo.
build_run() {
  local verbose="$1"
  shift
  local targets=("$@")

  local discovered_targets=() install_targets=() install_versions=()
  local d target candidate found available
  for d in cmd/*/; do
    [ -f "${d}main.go" ] || continue
    discovered_targets+=("$(basename "$d")")
  done
  if [ "${#discovered_targets[@]}" -gt 0 ]; then
    local discovered_list
    discovered_list=$(printf '%s\n' "${discovered_targets[@]}" | LC_ALL=C sort)
    discovered_targets=()
    while IFS= read -r candidate || [ -n "$candidate" ]; do
      [ -n "$candidate" ] && discovered_targets+=("$candidate")
    done <<EOF
$discovered_list
EOF
  fi

  if [ "${#targets[@]}" -eq 0 ]; then
    install_targets=("${discovered_targets[@]+"${discovered_targets[@]}"}")
  else
    available='(none)'
    [ "${#discovered_targets[@]}" -eq 0 ] || available="${discovered_targets[*]}"
    for target in "${targets[@]}"; do
      case "$target" in
      '' | */* | *\\*)
        printf 'invalid target %s; use one discovered command name without path components\n' "$(_go_quote "$target")" >&2
        return 2
        ;;
      esac
      found=0
      for candidate in "${discovered_targets[@]+"${discovered_targets[@]}"}"; do
        if [ "$candidate" = "$target" ]; then found=1; break; fi
      done
      if [ "$found" -ne 1 ]; then
        printf 'unknown target %s; available targets: %s\n' "$(_go_quote "$target")" "$available" >&2
        return 2
      fi
      for candidate in "${install_targets[@]+"${install_targets[@]}"}"; do
        if [ "$candidate" = "$target" ]; then
          printf 'duplicate target %s; list each target once\n' "$(_go_quote "$target")" >&2
          return 2
        fi
      done
      install_targets+=("$target")
    done
    local selected_list
    selected_list=$(printf '%s\n' "${install_targets[@]}" | LC_ALL=C sort)
    install_targets=()
    while IFS= read -r candidate || [ -n "$candidate" ]; do
      [ -n "$candidate" ] && install_targets+=("$candidate")
    done <<EOF
$selected_list
EOF
  fi

  if [ "${#install_targets[@]}" -gt 0 ]; then
    printf '\n%s\n' "$(yel7 '==> Validate programVersion declarations')"
    local ver decls
    for target in "${install_targets[@]+"${install_targets[@]}"}"; do
      ver=$(_extract_program_version "cmd/$target/main.go")
      if [ -z "$ver" ]; then
        printf 'cmd/%s/main.go must declare a non-empty const programVersion string literal\n' "$target" >&2
        return 1
      fi
      decls=$(_count_program_version_declarations "cmd/$target/main.go")
      if [ "$decls" -ne 1 ]; then
        printf 'cmd/%s/main.go must declare exactly one programVersion value; found %s declarations\n' \
          "$target" "$decls" >&2
        return 1
      fi
      if ! _is_strict_stable_semver "$ver"; then
        printf 'cmd/%s/main.go declares invalid utility version %s; use MAJOR.MINOR.PATCH with no leading zeroes, prerelease, or build metadata\n' \
          "$target" "$(_go_quote "$ver")" >&2
        return 1
      fi
      install_versions+=("$ver")
      printf '    %s: programVersion = %s\n' "$(cya4 "cmd/$target")" "$(grn3 "\"$ver\"")"
    done
  fi

  _build_install_temps=()
  _build_owned_dir=$(mktemp -d "${TMPDIR:-/tmp}/govna-go-build.XXXXXX") || {
    printf 'build: create owned temporary directory failed\n' >&2
    return 1
  }
  trap '_cleanup_build_owned' EXIT
  trap '_build_signal_exit 129' HUP
  trap '_build_signal_exit 130' INT
  trap '_build_signal_exit 143' TERM

  local module_path bin_dir ext
  module_path=$(go list -m -f '{{.Path}}')
  bin_dir="$(go env GOPATH)/bin"
  ext="$(go env GOEXE)"

  local scopes=()
  if [ "${#targets[@]}" -eq 0 ]; then
    scopes=('./...')
  else
    for target in "${install_targets[@]+"${install_targets[@]}"}"; do scopes+=("./cmd/$target"); done
  fi

  printf '%s\n' "$(yel7 '==> Check markdown for nested fence issues')"
  local findings
  findings=$(mdcheck .)
  if [ -n "$findings" ]; then
    printf '%s\n' "$findings" | write_indented
    printf 'mdcheck found nested-fence issue(s)\n' >&2
    return 1
  fi
  printf '    %s\n' 'No nested-fence issues found.'

  # Test-naming lint — silent on pass; see _lint_test_naming and _collect_test_files.
  local _lint_files=() _lint_path
  [ -f tests/run.sh ] && _lint_files+=('tests/run.sh')
  while IFS= read -r -d '' _lint_path; do
    _lint_files+=("$_lint_path")
  done < <(_collect_test_files '.')
  if [ "${#_lint_files[@]}" -gt 0 ]; then
    _lint_test_naming "${_lint_files[@]}" || {
      printf 'test naming violations found (see above)\n' >&2
      return 1
    }
  fi

  printf '\n%s\n' "$(yel7 '==> Update go.mod to reflect actual dependencies')"
  run_streaming grn3 go mod tidy

  printf '\n%s\n' "$(yel7 '==> Format Go code according to standard rules')"
  local fmt_out
  fmt_out=$(go fmt "${scopes[@]}" 2>&1) || true
  if _is_blank "$fmt_out"; then
    printf '    %s\n' 'No formatting changes needed.'
  else
    printf '%s\n' "$fmt_out" | write_indented
    printf 'go fmt found files that need formatting\n' >&2
    return 1
  fi

  printf '\n%s\n' "$(yel7 '==> Automatically fix code for API/language changes')"
  local fix_out
  fix_out=$(go fix "${scopes[@]}" 2>&1) || true
  if _is_blank "$fix_out"; then
    printf '    %s\n' 'No fixes applied.'
  else
    printf '%s\n' "$fix_out" | write_indented
  fi

  printf '\n%s\n' "$(yel7 '==> Check code for potential issues')"
  local vet_out vet_rc=0
  vet_out=$(go vet "${scopes[@]}" 2>&1) || vet_rc=$?
  if [ "$vet_rc" -ne 0 ]; then
    printf '%s\n' "$vet_out" | write_indented
    printf 'go vet found issues\n' >&2
    return 1
  elif ! _is_blank "$vet_out"; then
    printf '%s\n' "$vet_out" | write_indented
  else
    printf '    %s\n' 'No issues found by go vet.'
  fi

  local cover_path
  cover_path=$(mktemp "$_build_owned_dir/build-cover.XXXXXX") || {
    printf 'build: create coverage output failed\n' >&2
    return 1
  }

  printf '\n%s\n' "$(yel7 '==> Run tests for all packages in the repository')"
  local test_args=(test)
  [ "$verbose" = 1 ] && test_args+=(-v)
  test_args+=("-coverprofile=$cover_path")
  test_args+=("${scopes[@]}")
  run_streaming grn3 go "${test_args[@]}"
  _print_coverage_summary "$cover_path" "$module_path"

  printf '\n%s\n' "$(yel7 '==> Ensure staticcheck is available')"
  _ensure_staticcheck "$bin_dir" "$ext"

  printf '\n%s\n' "$(yel7 '==> Analyze Go code for potential issues')"
  local sc_out sc_rc=0
  sc_out=$("$_staticcheck_path" "${scopes[@]}" 2>&1) || sc_rc=$?
  if [ "$sc_rc" -ne 0 ]; then
    printf '%s\n' "$sc_out" | write_indented
    printf 'staticcheck found issues\n' >&2
    return 1
  elif ! _is_blank "$sc_out"; then
    printf '%s\n' "$sc_out" | write_indented
  else
    printf '    %s\n' 'No issues found by staticcheck.'
  fi

  if [ "${#targets[@]}" -eq 0 ]; then
    printf '\n%s\n' "$(yel7 '==> Building all utilities')"
  else
    printf '\n%s %s\n' "$(yel7 '==> Building specific utilities:')" "$(grn3 "${install_targets[*]}")"
  fi

  local output_path compiled_path index=0
  for target in "${install_targets[@]+"${install_targets[@]}"}"; do
    output_path="$bin_dir/$target$ext"
    if [ -L "$output_path" ] || { [ -e "$output_path" ] && [ ! -f "$output_path" ]; }; then
      printf 'utility %s: installed destination must be absent or a regular file: %s\n' "$target" "$output_path" >&2
      return 1
    fi
  done

  for target in "${install_targets[@]+"${install_targets[@]}"}"; do
    compiled_path="$_build_owned_dir/$target$ext"
    printf '\n%s %s\n' "$(yel7 '==> Building')" "$(grn3 "$target")"
    run_streaming grn3 go build -o "$compiled_path" -ldflags '-s -w' "./cmd/$target"
  done

  if [ "${#install_targets[@]}" -gt 0 ]; then
    printf '\n%s\n' "$(yel7 '==> Validate compiled utility versions')"
  fi
  index=0
  for target in "${install_targets[@]+"${install_targets[@]}"}"; do
    compiled_path="$_build_owned_dir/$target$ext"
    _validate_utility_version_output "$compiled_path" "$target" "${install_versions[$index]}" || return 1
    index=$((index + 1))
  done

  if [ "${#install_targets[@]}" -gt 0 ]; then
    printf '\n%s\n' "$(yel7 '==> Install validated utilities')"
  fi
  for target in "${install_targets[@]+"${install_targets[@]}"}"; do
    compiled_path="$_build_owned_dir/$target$ext"
    output_path="$bin_dir/$target$ext"
    _install_validated_utility "$compiled_path" "$output_path" "$target" || return 1
  done

  local next_tag
  if next_tag=$(_next_patch_tag) && [ -n "$next_tag" ]; then
    printf '\n%s\n\n    ./build.sh %s %s\n' \
      "$(yel7 '==> To release, run:')" "$(grn3 "$next_tag")" "$(gra5 '"<release message>"')"
  fi
  if ! _cleanup_build_owned; then
    trap - EXIT HUP INT TERM
    return 1
  fi
  trap - EXIT HUP INT TERM
}

_print_coverage_summary() { # $1=cover_path $2=module_path
  local cover_path="$1" module_path="$2"
  local func_out total
  func_out=$(go tool cover "-func=$cover_path")
  total=$(printf '%s\n' "$func_out" | awk '/^total:/{print $NF}')
  [ -z "$total" ] && return 0
  local pct tenths styled
  pct=$(_domain_coverage "$cover_path" "$module_path/internal/")
  local text="domain coverage: ${pct}%"
  tenths=$(printf '%s' "$pct" | awk '{printf "%d", $1*10}')
  if [ "$tenths" -ge 750 ]; then
    styled=$(grn3 "$text")
  elif [ "$tenths" -ge 500 ]; then
    styled=$(yel7 "$text")
  else
    styled=$(red3 "$text")
  fi
  printf '    %s  %s\n' "$styled" "$(gra5 "(total: $total)")"
}

_domain_coverage() { # $1=cover_path $2=prefix -> NN.N
  awk -v prefix="$2" '
    /^mode:/ { next }
    { if (NF != 3) next
      if (index($1, prefix) != 1) next
      total += $2
      if ($3+0 > 0) covered += $2
    }
    END {
      if (total == 0) { printf "0.0"; exit }
      printf "%.1f", covered/total*100
    }' "$1"
}

_extract_program_version() { # $1=main.go path -> version or empty
  [ -f "$1" ] || { printf ''; return 0; }
  local v
  v=$(awk '
    match($0, /^[ \t]*const[ \t]+programVersion[ \t]*(string[ \t]*)?=[ \t]*"[^"]*"/) {
      s = substr($0, RSTART, RLENGTH); sub(/^[^"]*"/, "", s); sub(/".*/, "", s)
      print s; exit
    }' "$1")
  if [ -n "$v" ]; then printf '%s' "$v"; return 0; fi
  v=$(awk '
    /const[ \t]*\(/ { ingroup=1 }
    ingroup && match($0, /programVersion[ \t]*(string[ \t]*)?=[ \t]*"[^"]*"/) {
      s = substr($0, RSTART, RLENGTH); sub(/^[^"]*"/, "", s); sub(/".*/, "", s)
      print s; exit
    }
    ingroup && /\)/ { ingroup=0 }' "$1")
  printf '%s' "$v"
}

_extract_template_version() { # $1=version.go path -> version or empty
  [ -f "$1" ] || { printf ''; return 0; }
  LC_ALL=C awk '
    $0 ~ "^[[:space:]]*const[[:space:]]+TemplateVersion[[:space:]]*=[[:space:]]*\"[^\"]*\"[[:space:]]*$" {
      count++; value=$0; sub(/^.*=[[:space:]]*"/, "", value); sub(/"[[:space:]]*$/, "", value)
    }
    $0 ~ "^[[:space:]]*const[[:space:]]+TemplateVersion([^A-Za-z0-9_]|$)" { mentions++ }
    END { if (count == 1 && mentions == 1 && value != "") print value }
  ' "$1"
}

_is_strict_stable_semver() { # $1=version -> success when MAJOR.MINOR.PATCH
  printf '%s' "$1" | LC_ALL=C grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
}

_validate_utility_version_output() { # $1=binary $2=utility ID $3=declared version
  local binary="$1" utility_id="$2" declared="$3" probe_root probe_dir rc actual
  probe_root="${_build_owned_dir:-${TMPDIR:-/tmp}}"
  probe_dir=$(mktemp -d "$probe_root/govna-version.XXXXXX") || {
    printf 'utility %s: create version probe workspace: check temporary-directory permissions and retry\n' "$utility_id" >&2
    return 1
  }
  printf '%s\n' "$utility_id $declared" >"$probe_dir/expected"
  printf '%s v%s\n' "$utility_id" "$declared" >"$probe_dir/expected-v"
  rc=0
  "$binary" --version >"$probe_dir/stdout" 2>"$probe_dir/stderr" || rc=$?
  if [ "$rc" -ne 0 ]; then
    printf 'utility %s: --version failed with exit status %d; implement --version to print %s\n' \
      "$utility_id" "$rc" "$utility_id $declared" >&2
    rm -rf "$probe_dir"
    return 1
  fi
  if ! cmp -s "$probe_dir/expected" "$probe_dir/stdout" &&
    ! cmp -s "$probe_dir/expected-v" "$probe_dir/stdout"; then
    actual=$(LC_ALL=C tr '\n' ' ' <"$probe_dir/stdout")
    printf 'utility %s: --version output %s; expected exactly %s on stdout\n' \
      "$utility_id" "$(_go_quote "$actual")" "$(_go_quote "$utility_id $declared")" >&2
    rm -rf "$probe_dir"
    return 1
  fi
  if [ -s "$probe_dir/stderr" ]; then
    printf 'utility %s: --version wrote diagnostics to stderr; write only %s to stdout\n' \
      "$utility_id" "$utility_id $declared" >&2
    rm -rf "$probe_dir"
    return 1
  fi
  rm -rf "$probe_dir"
}

_install_validated_utility() { # $1=compiled $2=destination $3=utility ID
  local compiled="$1" output="$2" utility_id="$3" install_tmp temp_index
  if [ -L "$output" ] || { [ -e "$output" ] && [ ! -f "$output" ]; }; then
    printf 'utility %s: installed destination must be absent or a regular file: %s\n' "$utility_id" "$output" >&2
    return 1
  fi
  install_tmp=$(mktemp "$(dirname "$output")/.govna-install-$utility_id.XXXXXX") || {
    printf 'utility %s: create adjacent installation staging file failed: %s\n' "$utility_id" "$(dirname "$output")" >&2
    return 1
  }
  temp_index=${#_build_install_temps[@]}
  _build_install_temps[$temp_index]="$install_tmp"
  cp "$compiled" "$install_tmp" || { rm -f "$install_tmp"; return 1; }
  chmod 0755 "$install_tmp" || { rm -f "$install_tmp"; return 1; }
  mv -f "$install_tmp" "$output" || { rm -f "$install_tmp"; return 1; }
  _build_install_temps[$temp_index]=''
  printf '    installed: %s\n' "$(cya4 "$output")"
}

_ensure_staticcheck() { # $1=bin_dir $2=ext -> sets _staticcheck_path; stdout msgs
  # Parity exception (determinism): always install + invoke the pinned version,
  # never PATH's staticcheck, never @latest. go run is rejected (it appends an
  # exit-status line to failure output). Messages go to stdout (not stderr) to
  # match buildtool's step output; the result path is returned via a global so
  # this function is not run inside a command substitution.
  local bin_dir="$1" ext="$2"
  local pinned='honnef.co/go/tools/cmd/staticcheck@v0.8.1'
  printf '    installing: %s\n' "$(grn3 "$pinned")"
  run_streaming grn3 go install "$pinned"
  _staticcheck_path="$bin_dir/staticcheck$ext"
}

_next_patch_tag() {
  local tags
  tags=$(git tag --list 2>/dev/null) || return 1
  printf '%s\n' "$tags" | awk '
    /^v[0-9]+\.[0-9]+\.[0-9]+$/ {
      split(substr($0, 2), a, ".")
      mj=a[1]+0; mn=a[2]+0; pt=a[3]+0
      if (!found || mj>bj || (mj==bj && (mn>bn || (mn==bn && pt>bp)))) {
        bj=mj; bn=mn; bp=pt; found=1
      }
    }
    END { if (found) printf "v%d.%d.%d", bj, bn, bp+1 }'
}

# ── mdcheck (nested-fence scan; mirrors buildtool CheckNestedFences) ──────────
mdcheck() { # $1=dir -> findings on stdout (empty = clean)
  local dir="$1" files path rel
  files=$(_md_files "$dir")
  while IFS= read -r path || [ -n "$path" ]; do
    [ -n "$path" ] || continue
    [ -f "$path" ] || continue
    rel="${path#"$dir"/}"
    _scan_nested_fences "$rel" "$path"
  done <<EOF
$files
EOF
}

_md_files() { # $1=dir -> newline list of .md paths
  local dir="$1" out rel full
  if out=$(git -C "$dir" ls-files '*.md' 2>/dev/null); then
    while IFS= read -r rel || [ -n "$rel" ]; do
      [ -n "$rel" ] || continue
      full="$dir/$rel"
      if [ ! -e "$full" ]; then
        printf 'mdcheck: skipping tracked but missing file: %s\n' "$rel" >&2
        continue
      fi
      # Markdown fixtures intentionally contain violations exercised by tests.
      case "$rel" in tests/fixtures/*) continue ;; esac
      printf '%s\n' "$full"
    done <<EOF
$out
EOF
    return 0
  fi
  find "$dir" \( -name .git -o -name node_modules -o -name vendor \
    -o -path "$dir/tests/fixtures" \) -prune -o \
    -name '*.md' -type f -print
}

_scan_nested_fences() { # $1=relpath $2=file
  local rel="$1" file="$2"
  awk -v path="$rel" '
    function parse(line,   i,n,first,count,rest) {
      i=1; n=length(line)
      while (i<=n && (substr(line,i,1)==" " || substr(line,i,1)=="\t")) i++
      if (i>n) return 0
      first=substr(line,i,1)
      if (first!="`" && first!="~") return 0
      count=0
      while (i+count<=n && substr(line,i+count,1)==first) count++
      if (count<3) return 0
      rest=substr(line,i+count); sub(/[ \t]+$/,"",rest)
      if (first=="`" && index(rest,"`")>0) return 0
      _dc=first; _count=count; _info=(length(rest)>0)?1:0; return 1
    }
    BEGIN { delimchar=""; delimcount=0; opener=0 }
    {
      lineno=NR
      if (!parse($0)) next
      if (delimchar=="") { delimchar=_dc; delimcount=_count; opener=lineno; next }
      if (_dc==delimchar && _count>=delimcount && _info==0) {
        delimchar=""; delimcount=0; next
      }
      if (delimchar=="`" && delimcount==3 && _dc=="`" && _count==3 && _info==1) {
        printf "%s:%d: 3-backtick fence opened at line %d contains nested tagged fence; use 4+ backticks or ~~~ for the outer fence\n", path, lineno, opener
        delimchar=""; delimcount=0
      }
    }' "$file"
}

# ── test-naming lint (AT/AC-only subset) ─────────────────────────────────────
# _collect_test_files <root> — emits absolute *_test.go paths NUL-delimited.
# Primary: rg --files -0; fallback: find -print0.
# find fallback is an intentional exception to the Tool Use rule: rg is preferred
# but find provides Bash 3.2-portable discovery when rg is absent. The fallback
# scans a conservative superset (ignores .gitignore) vs rg.
_collect_test_files() {
  local root="$1" abs_root
  abs_root="$(cd "$root" && pwd)"
  if command -v rg >/dev/null 2>&1; then
    ( cd "$root" && rg --files -0 -g '*_test.go' 2>/dev/null ) | \
      while IFS= read -r -d '' rel; do
        printf '%s\0' "$abs_root/$rel"
      done
  else
    find "$abs_root" \( -name .git -o -name node_modules -o -name vendor \) -prune -o \
      -name '*_test.go' -type f -print0
  fi
}

_lint_regex_hits() { # $1=ERE; input on stdin -> numbered matching lines
  local pattern="$1"
  if command -v rg >/dev/null 2>&1; then
    rg -n "$pattern"
  else
    # Portability fallback: build validation must still work without ripgrep.
    grep -nE "$pattern"
  fi
}

# _lint_test_naming FILE... — AT/AC-only subset: flags _at/_ac identifier forms
# and _ok/_fail/t.Run label call sites beginning with AT or AC numbers.
# Class, Part, and Round numbers are not covered by this lint.
# Identifier-shaped fixture strings are a known false-positive: assemble them
# from string fragments so test source does not trigger this check.
_lint_test_naming() {
  local _found=0 _f
  # Quote chars for portable ERE pattern construction (avoids nested quoting).
  local _sq _dq _bq
  _sq="'"
  _dq='"'
  _bq='`'
  local _ident_pat='_[Aa][Tt][0-9]|_[Aa][Cc][0-9]'
  local _shell_lbl_pat="(^|[;&|()[:space:]])(_ok|_fail)[[:space:]]+[${_dq}${_sq}]([Aa][Tt]|[Aa][Cc])[0-9]"
  local _go_lbl_pat="(^|[({;[:space:]])t[.]Run[(][${_dq}${_bq}]([Aa][Tt]|[Aa][Cc])[0-9]"
  for _f in "$@"; do
    [ -f "$_f" ] || continue
    # Preprocess: remove comment-only lines; strip inline Historical: tails.
    local _pre
    _pre=$(sed '
      s/^[[:space:]]*#.*$//
      s/^[[:space:]]*\/\/.*$//
      s/^[[:space:]]*\/[*].*$//
      s/^[[:space:]]*[*].*$//
      s/[[:space:]]*#[[:space:]]*Historical:.*$//
      s/[[:space:]]*\/\/[[:space:]]*Historical:.*$//
      s/[[:space:]]*\/[*][[:space:]]*Historical:.*$//
    ' "$_f")
    local _hits
    # Identifier scan: _at<N> or _ac<N> (underscore-prefix form).
    _hits=$(printf '%s\n' "$_pre" | _lint_regex_hits "$_ident_pat" 2>/dev/null || true)
    if [ -n "$_hits" ]; then
      printf '%s\n' "$_hits" | while IFS= read -r _line; do
        printf '%s:%s\n' "$_f" "$_line"
      done
      _found=1
    fi
    # Label call-site scan: _ok/_fail (shell) and t.Run (Go).
    _hits=$(printf '%s\n' "$_pre" | \
      _lint_regex_hits "${_shell_lbl_pat}|${_go_lbl_pat}" 2>/dev/null || true)
    if [ -n "$_hits" ]; then
      printf '%s\n' "$_hits" | while IFS= read -r _line; do
        printf '%s:%s\n' "$_f" "$_line"
      done
      _found=1
    fi
  done
  return "$_found"
}

# ════════════════════════════════════════════════════════════════════════════
# Release path
# ════════════════════════════════════════════════════════════════════════════

rel_usage() {
  printf '%s %s\n' "$(bold "$(whi5 'Usage:')")" 'rel vX.Y.Z "release message"'
  _emit_usage_line '-h, -?, --help' 'show this help'
  printf '\n%s\n' 'Release message must be 80 bytes or fewer.'
}

# rel_run TAG MESSAGE
rel_run() {
  local tag="$1" message="$2" rc=0 cleanup_rc=0
  _build_install_temps=()
  _build_owned_dir=$(mktemp -d "${TMPDIR:-/tmp}/govna-go-release.XXXXXX") || {
    _failure 'release: create invocation-owned temporary directory failed'
    return 1
  }
  trap '_cleanup_build_owned' EXIT
  trap '_release_signal_exit 129' HUP
  trap '_release_signal_exit 130' INT
  trap '_release_signal_exit 143' TERM
  _rel_run_inner "$tag" "$message" || rc=$?
  _cleanup_build_owned || cleanup_rc=$?
  trap - EXIT HUP INT TERM
  [ "$rc" -ne 0 ] && return "$rc"
  return "$cleanup_rc"
}

_release_signal_exit() { # $1=conventional signal exit status
  local status="$1"
  trap - EXIT HUP INT TERM
  if ! _cleanup_build_owned; then :; fi
  exit "$status"
}

_rel_run_inner() {
  local tag="$1" message="$2" candidate_tree approved_head retry=0 status
  _rel_tag_for_recovery="$tag"

  _ensure_git_repo || return 1
  _release_validate_prepared "$tag" "$message" || return 1
  candidate_tree=$(_capture_worktree_tree "$_build_owned_dir/candidate.index") || return 1
  status=$(git status --porcelain --untracked-files=all 2>/dev/null) || {
    _failure 'release: inspect index and working tree failed'
    return 1
  }
  approved_head=$(git rev-parse HEAD 2>/dev/null) || {
    _failure 'release: resolve HEAD failed'
    return 1
  }
  if [ -z "$status" ]; then
    if [ "$(git show -s --format=%B HEAD 2>/dev/null)" != "$message" ]; then
      _failure 'release: clean tree is not a retry because the full HEAD commit message differs from the release message'
      return 1
    fi
    retry=1
  elif [ "$candidate_tree" = "$(git rev-parse 'HEAD^{tree}' 2>/dev/null)" ]; then
    _failure 'release: no candidate tree changes are available to commit'
    return 1
  fi

  printf '%s %s\n' "$(yel7 'release tag:')" "$(grn3 "$tag")"
  printf '%s %s\n' "$(yel7 'release message:')" "$(grn3 "$(_go_quote "$message")")"
  printf '%s %s\n' "$(yel7 'remote:')" "$(cya4 'origin')"
  if [ "$retry" -eq 1 ]; then
    printf '%s\n' "$(yel7 "$(printf '\nRetry commit files:')")"
    git diff-tree --no-commit-id --name-status -r HEAD || return 1
  else
    printf '%s\n' "$(yel7 "$(printf '\nCandidate tree files:')")"
    git diff --name-status HEAD "$candidate_tree" || return 1
  fi

  printf '%s\n' "$(yel7 "$(printf '\nrelease sequence:')")"
  if [ "$retry" -eq 1 ]; then
    printf '%s\n' '- reuse the displayed clean HEAD commit'
  else
    printf '%s\n' '- git add -A and verify the staged tree'
    printf -- '- git commit -m %s and verify the committed tree\n' "$(_go_quote "$message")"
  fi
  printf '%s\n' '- validate every utility declaration'
  printf '%s\n' '- compile each utility once from clean committed HEAD'
  printf '%s\n' '- validate every compiled utility version and provenance'
  printf '%s\n' '- atomically install every validated utility and recheck it'
  printf '%s\n' "- git tag $tag"
  printf '%s\n' "- git push origin $tag"
  printf '%s\n' '- git push origin'

  printf '%s' "$(yel7 'Review the file list and sequence above. Proceed with release? (y/N): ')"
  local answer=''
  IFS= read -r answer || true
  case "$answer" in
  y | Y) ;;
  *)
    printf 'release aborted\n' >&2
    return 1
    ;;
  esac

  local current_tree completed=''
  if [ "$retry" -eq 1 ]; then
    [ -z "$(git status --porcelain --untracked-files=all 2>/dev/null)" ] || {
      _failure 'release: retry tree changed after approval'
      return 1
    }
    [ "$(git rev-parse HEAD 2>/dev/null)" = "$approved_head" ] || {
      _failure 'release: retry HEAD changed after approval'
      return 1
    }
    [ "$(git show -s --format=%B HEAD 2>/dev/null)" = "$message" ] || {
      _failure 'release: retry commit message changed after approval'
      return 1
    }
    _release_validate_prepared "$tag" "$message" || return 1
    completed='git commit (retry)'
  else
    current_tree=$(_capture_worktree_tree "$_build_owned_dir/recheck.index") || return 1
    [ "$current_tree" = "$candidate_tree" ] || {
      _failure 'release: candidate tree changed after approval; nothing was staged'
      return 1
    }
    _rel_step 'git add' "$completed" add -A -- . || return 1
    completed='git add'
    [ "$(git write-tree 2>/dev/null)" = "$candidate_tree" ] || {
      _failure 'release: staged tree differs from the approved candidate; inspect the index before retrying'
      return 1
    }
    _rel_step 'git commit' "$completed" commit -m "$message" || return 1
    completed='git add, git commit'
    _verify_release_commit "$candidate_tree" "$message" || return 1
  fi

  _release_compile_validate_install "$tag" || {
    _failure 'release: utility compilation, validation, or installation failed; no tag or push was attempted'
    _failure 'recovery: fix the reported problem and rerun the same two-argument release command'
    return 1
  }
  completed="$completed, release compilation and installation"
  _rel_step 'git tag' "$completed" tag "$tag" || return 1
  completed="$completed, git tag"
  [ "$(git rev-parse "$tag^{commit}" 2>/dev/null)" = "$(git rev-parse HEAD 2>/dev/null)" ] || {
    _failure 'release: created tag does not identify verified HEAD; push was not attempted'
    return 1
  }
  _rel_step 'git push tag' "$completed" push origin "$tag" || return 1
  completed="$completed, git push tag"
  _rel_step 'git push branch' "$completed" push origin || return 1
}

_capture_worktree_tree() { # $1=temporary index path $2=error prefix (optional)
  local index="$1" prefix="${2:-release}" tree
  rm -f -- "$index"
  GIT_INDEX_FILE="$index" git read-tree HEAD >/dev/null 2>&1 || {
    _failure "$prefix: initialize temporary candidate index failed"
    return 1
  }
  GIT_INDEX_FILE="$index" git add -A -- . >/dev/null 2>&1 || {
    _failure "$prefix: capture candidate files in temporary index failed"
    return 1
  }
  tree=$(GIT_INDEX_FILE="$index" git write-tree 2>/dev/null) || {
    _failure "$prefix: write candidate tree failed"
    return 1
  }
  printf '%s' "$tree"
}

_verify_release_commit() { # $1=approved tree $2=message
  local approved_tree="$1" message="$2" committed_tree
  committed_tree=$(git rev-parse 'HEAD^{tree}' 2>/dev/null) || {
    _failure 'release: resolve committed tree failed'
    return 1
  }
  [ "$committed_tree" = "$approved_tree" ] || {
    _failure 'release: new commit differs from the approved candidate tree'
    return 1
  }
  [ "$(git show -s --format=%B HEAD 2>/dev/null)" = "$message" ] || {
    _failure 'release: new commit message differs from the approved release message'
    return 1
  }
  [ -z "$(git status --porcelain --untracked-files=all 2>/dev/null)" ] || {
    _failure 'release: working tree is not clean after commit'
    return 1
  }
}

_release_validate_prepared() { # $1=tag $2=message
  local tag="$1" message="$2" version="${1#v}" root="$PWD" path kind actual cmd_dir decls
  [ "$tag" = "v$version" ] && _is_strict_stable_semver "$version" || {
    _failure "release: tag must use strict stable SemVer: $(_go_quote "$tag")"
    return 1
  }
  _validate_release_message release "$message" || return 1
  if git rev-parse -q --verify "refs/tags/$tag" >/dev/null 2>&1; then
    _failure "release: tag $tag already exists"
    return 1
  fi
  _prep_detect_version_targets "$root"
  _prep_validate_multi_utility_versions "$root" || return 1
  while IFS="$(printf '\t')" read -r path kind; do
    [ -n "$path" ] || continue
    case "$kind" in
    programVersion) actual=$(_extract_program_version "$path") ;;
    TemplateVersion) actual=$(_extract_template_version "$path") ;;
    *) actual='' ;;
    esac
    [ "$actual" = "$version" ] || {
      _failure "release: prepared $kind in $path is $(_go_quote "$actual"); require $(_go_quote "$version")"
      return 1
    }
  done <<EOF
$_prep_vtargets
EOF
  _validate_prepared_changelogs "$root" "$version" "$message" || return 1
}

_validate_binary_provenance() { # $1=binary $2=revision $3=label
  local binary="$1" revision="$2" label="$3" info
  info=$(go version -m "$binary" 2>/dev/null) || {
    _failure "release: read $label Go build information failed"
    return 1
  }
  printf '%s\n' "$info" | grep -Fq "vcs.revision=$revision" || {
    _failure "release: $label does not identify committed HEAD"
    return 1
  }
  printf '%s\n' "$info" | grep -Fq 'vcs.modified=false' || {
    _failure "release: $label build information is dirty"
    return 1
  }
}

_release_compile_validate_install() { # $1=tag
  local tag="$1" release_version="${1#v}" revision bin_dir ext d target version decls compiled output
  local targets=() versions=()
  for d in cmd/*/; do
    [ -f "${d}main.go" ] || continue
    targets+=("$(basename "$d")")
  done
  [ "${#targets[@]}" -gt 0 ] || {
    _failure 'release: no cmd/<target>/main.go utilities were discovered'
    return 1
  }
  local sorted
  sorted=$(printf '%s\n' "${targets[@]}" | LC_ALL=C sort)
  targets=()
  while IFS= read -r target || [ -n "$target" ]; do
    [ -n "$target" ] && targets+=("$target")
  done <<EOF
$sorted
EOF

  printf '\n%s\n' "$(yel7 '==> Validate release utility declarations')"
  _prep_detect_version_targets "$PWD"
  for target in "${targets[@]}"; do
    decls=$(_count_program_version_declarations "cmd/$target/main.go")
    version=$(_extract_program_version "cmd/$target/main.go")
    if [ "$decls" -ne 1 ] || [ -z "$version" ] || ! _is_strict_stable_semver "$version"; then
      _failure "release: cmd/$target/main.go must declare exactly one strict stable SemVer programVersion"
      return 1
    fi
    if printf '%s\n' "$_prep_vtargets" | cut -f1 | grep -Fxq "$PWD/cmd/$target/main.go" && [ "$version" != "$release_version" ]; then
      _failure "release: prep-changed utility $target version $version does not match $release_version"
      return 1
    fi
    versions+=("$version")
    printf '    %s: programVersion = %s\n' "$(cya4 "cmd/$target")" "$(grn3 "\"$version\"")"
  done

  revision=$(git rev-parse HEAD 2>/dev/null) || {
    _failure 'release: resolve committed HEAD failed'
    return 1
  }
  [ -z "$(git status --porcelain --untracked-files=all 2>/dev/null)" ] || {
    _failure 'release: require clean committed HEAD before compilation'
    return 1
  }
  bin_dir="$(go env GOPATH)/bin" || {
    _failure 'release: resolve GOPATH failed'
    return 1
  }
  ext=$(go env GOEXE) || {
    _failure 'release: resolve GOEXE failed'
    return 1
  }

  printf '\n%s\n' "$(yel7 '==> Compile release utilities')"
  local i=0
  for target in "${targets[@]}"; do
    compiled="$_build_owned_dir/$target$ext"
    run_streaming grn3 go build -mod=readonly -buildvcs=true -o "$compiled" -ldflags '-s -w' "./cmd/$target" || return 1
  done

  printf '\n%s\n' "$(yel7 '==> Validate compiled release utilities')"
  for target in "${targets[@]}"; do
    version="${versions[$i]}"
    compiled="$_build_owned_dir/$target$ext"
    _validate_utility_version_output "$compiled" "$target" "$version" || return 1
    _validate_binary_provenance "$compiled" "$revision" "compiled $target" || return 1
    printf '    verified: %s\n' "$(cya4 "$target")"
    i=$((i + 1))
  done

  mkdir -p "$bin_dir" || {
    _failure "release: create install directory failed: $bin_dir"
    return 1
  }
  printf '\n%s\n' "$(yel7 '==> Install validated release utilities')"
  for target in "${targets[@]}"; do
    compiled="$_build_owned_dir/$target$ext"
    output="$bin_dir/$target$ext"
    _install_validated_utility "$compiled" "$output" "$target" || return 1
  done

  printf '\n%s\n' "$(yel7 '==> Recheck installed release utilities')"
  i=0
  for target in "${targets[@]}"; do
    version="${versions[$i]}"
    output="$bin_dir/$target$ext"
    _validate_utility_version_output "$output" "$target" "$version" || return 1
    _validate_binary_provenance "$output" "$revision" "installed $target" || return 1
    printf '    verified installed: %s\n' "$(cya4 "$output")"
    i=$((i + 1))
  done
}

# _rel_step NAME COMPLETED gitargs... — run one git step; on failure emit the
# recovery message (depending on which steps already completed) and fail.
_rel_step() {
  local name="$1" completed="$2"
  shift 2
  if _run_git "$name" "$@"; then
    return 0
  fi
  _recovery_error "$name" "$_rel_tag_for_recovery" "$completed" "$_git_err" >&2
  return 1
}

# _recovery_error STEP TAG COMPLETED GITERR — mirrors reltool.recoveryError. The
# leading line is "STEP failed: GITERR", and GITERR is itself "STEP failed: exit
# status N", reproducing reltool's doubled wording verbatim.
_recovery_error() {
  local step="$1" tag="$2" completed="$3" giterr="$4"
  printf '%s failed: %s\n' "$step" "$giterr"
  [ -z "$completed" ] && return 0
  printf '\ncompleted before failure: %s\n' "$completed"
  case ",$completed," in
  *", git push tag,"*)
    printf '\nrecovery: tag %s was pushed but the branch push failed\n' "$tag"
    printf '  to retry: git push origin\n'
    ;;
  *", git tag,"*)
    printf '\nrecovery: tag %s exists locally but was not pushed\n' "$tag"
    printf '  to retry push: git push origin %s && git push origin\n' "$tag"
    printf '  to remove tag: git tag -d %s\n' "$tag"
    ;;
  *"git commit"*)
    printf '\nrecovery: the approved release commit exists without tag %s\n' "$tag"
    printf '  fix the reported problem, then rerun the same two-argument release command\n'
    ;;
  *"git add"*)
    printf '\nrecovery: the repository index may contain the approved candidate\n'
    printf '  inspect the index and working tree before retrying the release command\n'
    ;;
  esac
}

_ensure_git_repo() {
  local out rc=0
  out=$(git rev-parse --is-inside-work-tree 2>&1) || rc=$?
  if [ "$rc" -ne 0 ]; then
    printf 'verify git repo: exit status %d: %s\n' "$rc" "$(_trim "$out")" >&2
    return 1
  fi
  [ "$(_trim "$out")" = true ] || {
    printf 'current directory is not inside a git work tree\n' >&2
    return 1
  }
}

# _run_git NAME gitargs... — echo "running: git ..." then run git. On failure,
# record "NAME failed: exit status N" in _git_err and return 1 (the caller
# decides how to surface it, matching reltool's return-don't-print structure).
_run_git() {
  local name="$1"
  shift
  printf '%s %s\n' "$(yel7 'running:')" "$(grn3 "git $*")"
  local rc=0
  git "$@" || rc=$?
  if [ "$rc" -ne 0 ]; then
    _git_err="$name failed: exit status $rc"
    return 1
  fi
}

# ════════════════════════════════════════════════════════════════════════════
# Release-prep path
# ════════════════════════════════════════════════════════════════════════════

prep_usage() {
  cat <<'EOF'
prep vX.Y.Z "release message" [options]

Stages a release by bumping version constants, inserting a CHANGELOG row,
deleting completed AC files, and removing matching plan.md pointers.

Flags:
  -h, -?, --help   show this help
  --dry-run, -n    print intended writes without modifying the working tree

Prints the canonical release command on success. Does not run the release
itself — present the printed command for the director to run.

Release message must be 80 bytes or fewer.
Release message must be one table-safe line.
EOF
}

# prep_run DRY VERSION MESSAGE.
prep_run() {
  local dry="$1" version="$2" message="$3" rc=0 cleanup_rc=0
  _prep_owned_dir=$(mktemp -d "${TMPDIR:-/tmp}/govna-go-prep.XXXXXX") || {
    printf 'prep: create invocation-owned temporary directory failed\n' >&2
    return 1
  }
  trap '_cleanup_prep_owned' EXIT
  trap '_prep_signal_exit 129' HUP
  trap '_prep_signal_exit 130' INT
  trap '_prep_signal_exit 143' TERM
  _prep_run_inner "$dry" "$version" "$message" || rc=$?
  _cleanup_prep_owned || cleanup_rc=$?
  trap - EXIT HUP INT TERM
  [ "$rc" -ne 0 ] && return "$rc"
  return "$cleanup_rc"
}

_cleanup_prep_owned() {
  local path="$_prep_owned_dir"
  _prep_owned_dir=''
  [ -z "$path" ] || [ ! -d "$path" ] || rm -rf -- "$path"
}

_prep_signal_exit() { # $1=conventional signal exit status
  local status="$1"
  trap - EXIT HUP INT TERM
  if ! _cleanup_prep_owned; then :; fi
  exit "$status"
}

_prep_run_inner() {
  local dry="$1" version="$2" message="$3"
  local root="$PWD"
  local vstripped="${version#v}"

  # Phase 1: validate inputs.
  if ! _is_strict_stable_semver "$vstripped" || [ "$version" != "v$vstripped" ]; then
    printf 'prep: version must match strict vMAJOR.MINOR.PATCH: %s\n' "$(_go_quote "$version")" >&2
    return 1
  fi
  _validate_release_message prep "$message" || return 1

  # Phase 2: validate git state.
  _prep_validate_git_state "$root" "$version" || return 1

  if [ -n "${GOVNA_PREP_VALIDATION_TOKEN:-}" ]; then
    printf 'prep: validation-token environment evidence is unsupported for Go\n' >&2
    return 1
  fi

  # Phase 3: detect version targets (+ warning). Called directly (not in a
  # command substitution) so the _prep_warning/_prep_vtargets globals propagate.
  _prep_detect_version_targets "$root"
  _prep_validate_multi_utility_versions "$root" || return 1
  local vtargets="$_prep_vtargets"
  [ -n "$_prep_warning" ] && printf '%s\n' "$_prep_warning"
  _prep_validate_version_plan "$vtargets" "$vstripped" || return 1

  # Phase 4: detect CHANGELOG targets (+ idempotency guard). Direct call so
  # _prep_cl_err/_prep_ctargets propagate.
  _prep_detect_changelog_targets "$root" "$version" || {
    printf 'prep: detect CHANGELOG targets: %s\n' "$_prep_cl_err" >&2
    return 1
  }
  local ctargets="$_prep_ctargets"

  # Phase 5: parse AC refs and require one selected AC file per unique ref.
  local acnums acfiles
  acnums=$(_prep_parse_ac_refs "$message")
  acfiles=$(_prep_find_ac_files "$root" "$acnums")
  _prep_validate_ac_selection "$root" "$acnums" "$acfiles" || return 1
  local ielines
  ielines=$(_prep_find_ie_lines "$root" "$acnums")

  if [ "$dry" -eq 1 ]; then
    _prep_print_dry_run "$vtargets" "$ctargets" "$vstripped" "$message" "$acfiles" "$ielines"
    _prep_emit_release_command "$version" "$message"
    return 0
  fi

  # Phase 6: capture the pre-write tree and the exact planned path set.
  local before_tree after_tree expected_paths actual_paths
  before_tree=$(_capture_worktree_tree "$_prep_owned_dir/before.index" prep) || return 1
  expected_paths=$(_prep_expected_paths "$root" "$vtargets" "$ctargets" "$acfiles" "$ielines")
  _prep_prepare_expected_files "$root" "$vtargets" "$ctargets" "$vstripped" "$message" "$ielines" || return 1

  # Phase 7a: apply version bumps.
  local path kind
  while IFS="$(printf '\t')" read -r path kind; do
    [ -n "$path" ] || continue
    _prep_apply_version_bump "$path" "$kind" "$vstripped" || {
      printf 'prep: bump %s: %s\n' "$path" "$_prep_bump_err" >&2
      return 1
    }
  done <<EOF
$vtargets
EOF

  # Phase 7b: insert CHANGELOG rows.
  while IFS= read -r path; do
    [ -n "$path" ] || continue
    _prep_apply_changelog_insert "$path" "$vstripped" "$message" || {
      printf 'prep: insert CHANGELOG row in %s: %s\n' "$path" "$_prep_cl_err" >&2
      return 1
    }
  done <<EOF
$ctargets
EOF

  # Phase 7c: delete AC files.
  while IFS= read -r path; do
    [ -n "$path" ] || continue
    rm -- "$path" || {
      printf 'prep: delete %s: failed\n' "$path" >&2
      return 1
    }
    printf 'prep: deleted %s\n' "$path"
  done <<EOF
$acfiles
EOF

  # Phase 7d: sweep AC-pointer IE lines from plan.md.
  _prep_remove_ie_lines "$root" "$ielines" || {
    printf 'prep: sweep plan.md AC-pointer IEs: %s\n' "$_prep_ie_err" >&2
    return 1
  }
  local line
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    printf 'prep: removed plan.md IE line: %s\n' "$(_trim "$line")"
  done <<EOF
$ielines
EOF

  # Phase 8: reject every result outside the planned transformations.
  after_tree=$(_capture_worktree_tree "$_prep_owned_dir/after.index" prep) || return 1
  actual_paths=$(git diff --name-only "$before_tree" "$after_tree" 2>/dev/null) || {
    printf 'prep: compare planned and resulting trees failed\n' >&2
    return 1
  }
  if [ "$actual_paths" != "$expected_paths" ]; then
    printf 'prep: resulting paths differ from the planned transformations\nplanned:\n%s\nresult:\n%s\n' \
      "$expected_paths" "$actual_paths" >&2
    return 1
  fi
  _prep_verify_expected_files "$root" "$vtargets" "$ctargets" "$ielines" || return 1
  _prep_verify_result "$root" "$vtargets" "$ctargets" "$vstripped" "$message" "$acfiles" "$ielines" || return 1

  # Phase 9: emit release command.
  _prep_emit_release_command "$version" "$message"
}

_prep_validate_git_state() { # $1=root $2=version
  local root="$1" version="$2" out rc=0
  out=$(cd "$root" && git rev-parse --is-inside-work-tree 2>&1) || rc=$?
  if [ "$rc" -ne 0 ]; then
    printf 'prep: verify git repo: exit status %d: %s\n' "$rc" "$(_trim "$out")" >&2
    return 1
  fi
  [ "$(_trim "$out")" = true ] || {
    printf 'prep: not inside a git work tree\n' >&2
    return 1
  }
  if (cd "$root" && git rev-parse -q --verify "refs/tags/$version" >/dev/null 2>&1); then
    printf 'prep: tag %s already exists\n' "$version" >&2
    return 1
  fi
  local latest
  latest=$(cd "$root" && git describe --tags --abbrev=0 2>/dev/null) || return 0
  local head ref out rc
  rc=0; out=$(cd "$root" && git rev-parse HEAD 2>&1) || rc=$?
  if [ "$rc" -ne 0 ]; then
    printf 'prep: compare HEAD to %s: exit status %d\n' "$latest" "$rc" >&2
    return 1
  fi
  head="$out"
  rc=0; out=$(cd "$root" && git rev-parse "$latest" 2>&1) || rc=$?
  if [ "$rc" -ne 0 ]; then
    printf 'prep: compare HEAD to %s: exit status %d\n' "$latest" "$rc" >&2
    return 1
  fi
  ref="$out"
  [ "$head" != "$ref" ] && return 0
  local dirty
  rc=0; out=$(cd "$root" && git status --porcelain 2>&1) || rc=$?
  if [ "$rc" -ne 0 ]; then
    printf 'prep: check working tree: exit status %d\n' "$rc" >&2
    return 1
  fi
  dirty="$out"
  if [ -z "$dirty" ]; then
    printf 'prep: nothing to release: HEAD is at %s and working tree is clean\n' "$latest" >&2
    return 1
  fi
}

_prep_module_basename() { # $1=root -> module basename or empty
  local root="$1" line modpath
  [ -f "$root/go.mod" ] || { printf ''; return; }
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
    module\ * | module"$(printf '\t')"*)
      modpath=$(_trim "${line#module}")
      [ -z "$modpath" ] && { printf ''; return; }
      printf '%s' "${modpath##*/}"
      return
      ;;
    esac
  done <"$root/go.mod"
  printf ''
}

# _prep_detect_version_targets ROOT — prints "path<TAB>kind" lines; sets
# _prep_warning. Reproduces the primary-cmd convention + TemplateVersion target.
_prep_detect_version_targets() {
  local root="$1"
  _prep_warning=''
  local base
  base=$(_prep_module_basename "$root")

  local pv=() d name mainp
  for d in "$root"/cmd/*/; do
    [ -d "$d" ] || continue
    name=$(basename "$d")
    mainp="$root/cmd/$name/main.go"
    [ -f "$mainp" ] || continue
    if grep -Eq 'programVersion[[:space:]]*(string[[:space:]]*)?=[[:space:]]*"[^"]*"' "$mainp"; then
      pv+=("$mainp")
    fi
  done

  local primary='' secondaries=() p
  local primary_path="$root/cmd/$base/main.go"
  if [ -n "$base" ]; then
    for p in ${pv[@]+"${pv[@]}"}; do
      if [ "$p" = "$primary_path" ]; then primary="$p"; else secondaries+=("$p"); fi
    done
  fi

  local targets=()
  local tab
  tab=$(printf '\t')
  if [ -n "$primary" ]; then
    targets+=("$primary${tab}programVersion")
    if [ "${#secondaries[@]}" -gt 0 ]; then
      local joined
      joined=$(_join_comma_space "${secondaries[@]}")
      _prep_warning="primary cmd/$base/main.go bumped; ${#secondaries[@]} secondary programVersion target(s) skipped (independent versioning, each utility owns its own version per its own AC). Skipped: $joined"
    fi
  elif [ "${#pv[@]}" -eq 1 ]; then
    targets+=("${pv[0]}${tab}programVersion")
  elif [ "${#pv[@]}" -gt 1 ]; then
    local hint='no go.mod-derived primary cmd'
    [ -n "$base" ] && hint="no primary cmd/$base/main.go"
    local joined
    joined=$(_join_comma_space ${pv[@]+"${pv[@]}"})
    _prep_warning="multi-utility repo detected (${#pv[@]} programVersion targets, $hint): per-utility programVersion bumps skipped (each utility owns its own version per its own AC). Skipped: $joined"
  fi

  if [ -d "$root/internal/templates/base" ]; then
    local tvgo="$root/internal/templates/version.go"
    if [ -f "$tvgo" ] && grep -Eq 'const[[:space:]]+TemplateVersion[[:space:]]*=[[:space:]]*"[^"]+"' "$tvgo"; then
      targets+=("$tvgo${tab}TemplateVersion")
    fi
  fi

  if [ "${#targets[@]}" -gt 0 ]; then
    _prep_vtargets=$(printf '%s\n' "${targets[@]}" | LC_ALL=C sort)
  else
    _prep_vtargets=''
  fi
}

_count_program_version_declarations() { # $1=main.go path -> declaration count
  awk '
    /^[[:space:]]*const[[:space:]]+programVersion[[:space:]]*(string[[:space:]]*)?=/ { count++ }
    /const[[:space:]]*\(/ { grouped=1; next }
    grouped && /programVersion[[:space:]]*(string[[:space:]]*)?=/ { count++ }
    grouped && /^[[:space:]]*\)/ { grouped=0 }
    END { print count+0 }' "$1"
}

_prep_validate_multi_utility_versions() { # $1=root
  local root="$1" cmd_dir mainp count=0 version decls
  for cmd_dir in "$root"/cmd/*/; do
    [ -d "$cmd_dir" ] || continue
    [ -f "$cmd_dir/main.go" ] || continue
    count=$((count + 1))
  done
  [ "$count" -gt 1 ] || return 0
 for cmd_dir in "$root"/cmd/*/; do
   [ -f "$cmd_dir/main.go" ] || continue
    mainp="${cmd_dir}main.go"
   decls=$(_count_program_version_declarations "$mainp")
   version=$(_extract_program_version "$mainp")
   if [ "$decls" -ne 1 ] || [ -z "$version" ]; then
      printf 'prep: %s must contain exactly one non-empty programVersion declaration; add one explicit utility version and retry\n' \
        "$mainp" >&2
     return 1
   fi
   if ! _is_strict_stable_semver "$version"; then
      printf 'prep: %s declares invalid utility version %s; use MAJOR.MINOR.PATCH with no leading zeroes, prerelease, or build metadata\n' \
        "$mainp" "$(_go_quote "$version")" >&2
      return 1
    fi
  done
}

_prep_validate_version_plan() { # $1=targets $2=requested version
  local targets="$1" requested="$2" path kind current tab
  tab=$(printf '\t')
  while IFS="$tab" read -r path kind; do
    [ -n "$path" ] || continue
    case "$kind" in
    programVersion)
      if [ "$(_count_program_version_declarations "$path")" -ne 1 ]; then
        printf 'prep: %s must contain exactly one programVersion declaration\n' "$path" >&2
        return 1
      fi
      current=$(_extract_program_version "$path")
      ;;
    TemplateVersion) current=$(_extract_template_version "$path") ;;
    *)
      printf 'prep: unknown version target kind: %s\n' "$kind" >&2
      return 1
      ;;
    esac
    if ! _is_strict_stable_semver "$current"; then
      printf 'prep: %s has invalid %s %s; require strict stable SemVer\n' "$path" "$kind" "$(_go_quote "$current")" >&2
      return 1
    fi
    if [ "$current" = "$requested" ]; then
      printf 'prep: %s %s already equals requested version %s\n' "$path" "$kind" "$requested" >&2
      return 1
    fi
  done <<EOF
$targets
EOF
}

_join_comma_space() {
  local out='' a
  for a in "$@"; do
    if [ -z "$out" ]; then out="$a"; else out="$out, $a"; fi
  done
  printf '%s' "$out"
}

_prep_detect_changelog_targets() { # $1=root $2=version -> sets _prep_ctargets/_prep_cl_err
  local root="$1" version="$2" vstripped="${2#v}"
  _prep_cl_err=''
  _prep_ctargets=''
  local p out='' count
  for p in "$root/CHANGELOG.md" "$root/internal/templates/CHANGELOG.md"; do
    [ -f "$p" ] || continue
    _validate_changelog_shape "$p" || return 1
    count=$(LC_ALL=C awk -F'|' -v version="$vstripped" '
      { value=$2; gsub(/^[[:space:]]+|[[:space:]]+$/, "", value); if (value == version) count++ }
      END { print count+0 }' "$p")
    if [ "$count" -ne 0 ]; then
      _prep_cl_err="$p already has a row for $vstripped (prep is not idempotent on CHANGELOG)"
      return 1
    fi
    if [ -z "$out" ]; then out="$p"; else out="$out
$p"; fi
  done
  if [ -z "$out" ]; then
    _prep_cl_err='no canonical CHANGELOG.md target was found'
    return 1
  fi
  _prep_ctargets="$out"
}

_validate_changelog_shape() { # $1=path
  local path="$1"
  LC_ALL=C awk '
    NR == 1 && $0 != "# Changelog" { bad=1 }
    NR == 2 && $0 != "" { bad=1 }
    NR == 3 && $0 != "| Version | Summary |" { bad=1 }
    NR == 4 && $0 != "|---------|---------|" { bad=1 }
    NR == 5 && $0 != "| Unreleased | |" { bad=1 }
    NR >= 5 && $0 !~ /^\| [^|]+ \|(\\\||[^|])*\|$/ { bad=1 }
    END { if (NR < 5 || bad) exit 1 }
  ' "$path" || {
    _prep_cl_err="$path does not use the canonical changelog heading and two-column table shape"
    return 1
  }
}

_validate_changelog_release_row() { # $1=path $2=version $3=message
  local path="$1" row="| $2 | $3 |"
  _validate_changelog_shape "$path" || return 1
  LC_ALL=C awk -v row="$row" '
    $0 == row { count++; if (NR == 6) immediate=1 }
    END { if (count != 1 || !immediate) exit 1 }
  ' "$path" || {
    _prep_cl_err="$path must contain one exact release row immediately after Unreleased"
    return 1
  }
}

_validate_prepared_changelogs() { # $1=root $2=version $3=message
  local root="$1" version="$2" message="$3" path found=0
  for path in "$root/CHANGELOG.md" "$root/internal/templates/CHANGELOG.md"; do
    [ -f "$path" ] || continue
    found=1
    _validate_changelog_release_row "$path" "$version" "$message" || {
      printf 'release: %s\n' "$_prep_cl_err" >&2
      return 1
    }
  done
  [ "$found" -eq 1 ] || {
    _failure 'release: no canonical CHANGELOG.md target was found'
    return 1
  }
}

_prep_parse_ac_refs() { # $1=message -> sorted unique AC numbers, one per line
  printf '%s' "$1" | grep -oE 'AC[0-9]+' | sed 's/^AC//' | LC_ALL=C sort -n -u || true
}

_prep_find_ac_files() { # $1=root $2=acnums -> sorted govna/ac<N>-*.md paths
  local root="$1" acnums="$2"
  [ -n "$acnums" ] || return 0
  local f name num
  for f in "$root"/govna/ac*.md; do
    [ -f "$f" ] || continue
    name=$(basename "$f")
    [ "$name" = "ac-template.md" ] && continue
    case "$name" in
    ac[0-9]*-*.md) num=$(printf '%s' "$name" | sed -E 's/^ac([0-9]+)-.*/\1/') ;;
    *) continue ;;
    esac
    if printf '%s\n' "$acnums" | grep -qx "$num"; then
      printf '%s\n' "$f"
    fi
  done | LC_ALL=C sort
}

_prep_validate_ac_selection() { # $1=root $2=acnums $3=acfiles
  local root="$1" acnums="$2" acfiles="$3" number count path name
  if [ -z "$acnums" ]; then
    printf 'prep: release message must name at least one AC<number> reference\n' >&2
    return 1
  fi
  while IFS= read -r number; do
    [ -n "$number" ] || continue
    count=0
    while IFS= read -r path; do
      [ -n "$path" ] || continue
      name=$(basename "$path")
      case "$name" in ac"$number"-*.md) count=$((count + 1)) ;; esac
    done <<EOF
$acfiles
EOF
    if [ "$count" -ne 1 ]; then
      printf 'prep: AC%s must select exactly one govna/ac%s-*.md file; found %d\n' "$number" "$number" "$count" >&2
      return 1
    fi
  done <<EOF
$acnums
EOF
}

_prep_find_ie_lines() { # $1=root $2=acnums -> matching plan.md lines
  local root="$1" acnums="$2"
  [ -n "$acnums" ] || return 0
  [ -f "$root/plan.md" ] || return 0
  local line num
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
    *"→ govna/ac"[0-9]*-*) num=$(printf '%s' "$line" | sed -E 's/.*→[[:space:]]+govna\/ac([0-9]+)-.*/\1/') ;;
    *) continue ;;
    esac
    if printf '%s\n' "$acnums" | grep -qx "$num"; then
      printf '%s\n' "$line"
    fi
  done <"$root/plan.md"
}

_prep_remove_ie_lines() { # $1=root $2=ielines(newline-sep)
  local root="$1" lines="$2"
  [ -n "$lines" ] || return 0
  local tmp
  tmp=$(mktemp "${TMPDIR:-/tmp}/prep-plan.XXXXXX")
  _prep_ie_err=''
  drop="$lines" awk '
    BEGIN { n = split(ENVIRON["drop"], arr, "\n"); for (i = 1; i <= n; i++) if (arr[i] != "") d[arr[i]] = 1 }
    { if ($0 in d) next; print }
  ' "$root/plan.md" >"$tmp" || { rm -f "$tmp"; _prep_ie_err="awk failed on plan.md"; return 1; }
  if ! cat "$tmp" 2>/dev/null >"$root/plan.md"; then
    rm -f "$tmp"; _prep_ie_err="write failed: $root/plan.md"; return 1
  fi
  rm -f "$tmp"
}

_prep_expected_paths() { # root vtargets ctargets acfiles ielines
  local root="$1" vtargets="$2" ctargets="$3" acfiles="$4" ielines="$5" path kind tab
  tab=$(printf '\t')
  {
    while IFS="$tab" read -r path kind; do
      [ -n "$path" ] && printf '%s\n' "${path#"$root"/}"
    done <<EOF
$vtargets
EOF
    while IFS= read -r path; do
      [ -n "$path" ] && printf '%s\n' "${path#"$root"/}"
    done <<EOF
$ctargets
EOF
    while IFS= read -r path; do
      [ -n "$path" ] && printf '%s\n' "${path#"$root"/}"
    done <<EOF
$acfiles
EOF
    [ -z "$ielines" ] || printf '%s\n' 'plan.md'
  } | LC_ALL=C sort -u
}

_prep_prepare_expected_files() { # root vtargets ctargets version message ielines
  local root="$1" vtargets="$2" ctargets="$3" version="$4" message="$5" ielines="$6"
  local expected_root="$_prep_owned_dir/expected" path kind relative tab
  tab=$(printf '\t')
  while IFS="$tab" read -r path kind; do
    [ -n "$path" ] || continue
    relative="${path#"$root"/}"
    mkdir -p "$expected_root/$(dirname "$relative")" || return 1
    cp "$path" "$expected_root/$relative" || return 1
    _prep_apply_version_bump "$expected_root/$relative" "$kind" "$version" || return 1
  done <<EOF
$vtargets
EOF
  while IFS= read -r path; do
    [ -n "$path" ] || continue
    relative="${path#"$root"/}"
    mkdir -p "$expected_root/$(dirname "$relative")" || return 1
    cp "$path" "$expected_root/$relative" || return 1
    _prep_apply_changelog_insert "$expected_root/$relative" "$version" "$message" || return 1
  done <<EOF
$ctargets
EOF
  if [ -n "$ielines" ]; then
    mkdir -p "$expected_root" || return 1
    cp "$root/plan.md" "$expected_root/plan.md" || return 1
    _prep_remove_ie_lines "$expected_root" "$ielines" || return 1
  fi
}

_prep_verify_expected_files() { # root vtargets ctargets ielines
  local root="$1" vtargets="$2" ctargets="$3" ielines="$4"
  local expected_root="$_prep_owned_dir/expected" path kind relative tab
  tab=$(printf '\t')
  while IFS="$tab" read -r path kind; do
    [ -n "$path" ] || continue
    relative="${path#"$root"/}"
    cmp -s "$expected_root/$relative" "$path" || {
      printf 'prep: %s differs from its planned version transformation\n' "$relative" >&2
      return 1
    }
  done <<EOF
$vtargets
EOF
  while IFS= read -r path; do
    [ -n "$path" ] || continue
    relative="${path#"$root"/}"
    cmp -s "$expected_root/$relative" "$path" || {
      printf 'prep: %s differs from its planned changelog transformation\n' "$relative" >&2
      return 1
    }
  done <<EOF
$ctargets
EOF
  if [ -n "$ielines" ] && ! cmp -s "$expected_root/plan.md" "$root/plan.md"; then
    printf 'prep: plan.md differs from its planned pointer removals\n' >&2
    return 1
  fi
}

_prep_verify_result() { # root vtargets ctargets version message acfiles ielines
  local root="$1" vtargets="$2" ctargets="$3" version="$4" message="$5" acfiles="$6" ielines="$7"
  local path kind actual line tab
  tab=$(printf '\t')
  while IFS="$tab" read -r path kind; do
    [ -n "$path" ] || continue
    case "$kind" in
    programVersion) actual=$(_extract_program_version "$path") ;;
    TemplateVersion) actual=$(_extract_template_version "$path") ;;
    *) actual='' ;;
    esac
    if [ "$actual" != "$version" ] || ! _is_strict_stable_semver "$actual"; then
      printf 'prep: resulting %s in %s does not equal strict version %s\n' "$kind" "$path" "$version" >&2
      return 1
    fi
  done <<EOF
$vtargets
EOF
  while IFS= read -r path; do
    [ -n "$path" ] || continue
    _validate_changelog_release_row "$path" "$version" "$message" || {
      printf 'prep: %s\n' "$_prep_cl_err" >&2
      return 1
    }
  done <<EOF
$ctargets
EOF
  while IFS= read -r path; do
    [ -n "$path" ] || continue
    if [ -e "$path" ] || [ -L "$path" ]; then
      printf 'prep: planned AC deletion remains: %s\n' "$path" >&2
      return 1
    fi
  done <<EOF
$acfiles
EOF
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    if [ -f "$root/plan.md" ] && grep -Fqx "$line" "$root/plan.md"; then
      printf 'prep: planned plan.md pointer remains: %s\n' "$(_trim "$line")" >&2
      return 1
    fi
  done <<EOF
$ielines
EOF
}

_prep_apply_version_bump() { # $1=path $2=kind $3=vstripped
  local path="$1" kind="$2" v="$3"
  _prep_bump_err=''
  local pat
  case "$kind" in
  programVersion) pat='(programVersion[[:space:]]*(string[[:space:]]*)?=[[:space:]]*)"[^"]*"' ;;
  TemplateVersion) pat='(const[[:space:]]+TemplateVersion[[:space:]]*=[[:space:]]*)"[^"]*"' ;;
  *)
    _prep_bump_err="unknown version target kind: $kind"
    return 1
    ;;
  esac
  if ! grep -Eq "$pat" "$path"; then
    _prep_bump_err="no version constant matched in $path"
    return 1
  fi
  local tmp
  tmp=$(mktemp "${TMPDIR:-/tmp}/prep-bump.XXXXXX")
  if ! sed -E "s/$pat/\\1\"$v\"/g" "$path" >"$tmp"; then
    rm -f "$tmp"; _prep_bump_err="sed failed on $path"; return 1
  fi
  if ! cat "$tmp" 2>/dev/null >"$path"; then rm -f "$tmp"; _prep_bump_err="write failed: $path"; return 1; fi
  rm -f "$tmp"
}

_prep_apply_changelog_insert() { # $1=path $2=vstripped $3=message
  local path="$1" v="$2" msg="$3"
  _validate_changelog_shape "$path" || return 1
  local tmp row
  tmp=$(mktemp "${TMPDIR:-/tmp}/prep-cl.XXXXXX")
  row="| $v | $msg |"
  if ! row="$row" awk '
    BEGIN { row = ENVIRON["row"] }
    { print }
    !done && /^\| Unreleased \|/ { print row; done = 1 }
  ' "$path" >"$tmp"; then
    rm -f "$tmp"; _prep_cl_err="awk failed on $path"; return 1
  fi
  if ! cat "$tmp" 2>/dev/null >"$path"; then rm -f "$tmp"; _prep_cl_err="write failed: $path"; return 1; fi
  rm -f "$tmp"
}

_prep_emit_release_command() { # $1=version $2=message
  printf '\nrelease command:\n  ./build.sh %s %s\n' "$1" "$(_shell_quote "$2")"
}

_prep_print_dry_run() { # vtargets ctargets vstripped message acfiles ielines
  local vtargets="$1" ctargets="$2" vstripped="$3" message="$4" acfiles="$5" ielines="$6"
  local path kind p line tab
  tab=$(printf '\t')
  printf '\n--- dry run (no writes) ---\n'
  printf 'version bumps:\n'
  while IFS="$tab" read -r path kind; do
    [ -n "$path" ] && printf '  %s → %s (%s)\n' "$path" "$vstripped" "$kind"
  done <<EOF
$vtargets
EOF
  printf 'CHANGELOG inserts:\n'
  while IFS= read -r p; do
    [ -n "$p" ] && printf '  %s: | %s | %s |\n' "$p" "$vstripped" "$message"
  done <<EOF
$ctargets
EOF
  printf 'AC deletions:\n'
  while IFS= read -r p; do
    [ -n "$p" ] && printf '  delete %s\n' "$p"
  done <<EOF
$acfiles
EOF
  printf 'plan.md AC-pointer IE removals:\n'
  while IFS= read -r line; do
    [ -n "$line" ] && printf '  remove: %s\n' "$(_trim "$line")"
  done <<EOF
$ielines
EOF
  printf -- '--- end dry run ---\n'
}

# ════════════════════════════════════════════════════════════════════════════
# Dispatch
# ════════════════════════════════════════════════════════════════════════════

main() {
  _color_init

  if [ "${1:-}" = prep ]; then
    shift
    prep_main "$@"
    return $?
  fi

  if [ "$#" -ge 1 ] && printf '%s' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    rel_main "$@"
    return $?
  fi

  build_main "$@"
}

build_main() {
  # Help only when it is the sole argument (mirrors buildtool.ParseArgs); a help
  # flag mixed with other args is an error.
  if [ "$#" -eq 1 ]; then
    case "$1" in -h | -\? | --help) build_usage; return 0 ;; esac
  fi
  local verbose=0
  local targets=()
  local arg
  for arg in "$@"; do
    case "$arg" in
    -v | --verbose) verbose=1 ;;
    -h | -\? | --help)
      printf 'help flags must be used by themselves\n' >&2
      return 2
      ;;
    -*)
      printf 'unsupported option %s; use target names plus optional -v, --verbose\n' "$(_go_quote "$arg")" >&2
      return 2
      ;;
    *) targets+=("$arg") ;;
    esac
  done
  build_run "$verbose" ${targets[@]+"${targets[@]}"}
}

rel_main() {
  if [ "$#" -eq 0 ]; then rel_usage; return 0; fi
  if [ "$#" -eq 1 ]; then
    case "$1" in -h | -\? | --help) rel_usage; return 0 ;; esac
  fi
  if [ "$#" -ne 2 ]; then
    printf 'usage: rel vX.Y.Z "release message"\n' >&2
    return 2
  fi
  local tag message
  tag="$1"
  message="$2"
  case "$tag" in
  -*)
    printf 'unsupported option %s; use positional args or -h, -?, --help\n' "$(_go_quote "$tag")" >&2
    return 2
    ;;
  esac
  if [ "$tag" != "v${tag#v}" ] || ! _is_strict_stable_semver "${tag#v}"; then
    printf 'release tag must match strict vMAJOR.MINOR.PATCH: %s\n' "$(_go_quote "$tag")" >&2
    return 2
  fi
  _validate_release_message release "$message" || return 2

  rel_run "$tag" "$message" || return 1

}

prep_main() {
  # ParseArgs (mirrors preptool.ParseArgs): help-only, flags, two positionals.
  if [ "$#" -eq 0 ]; then prep_usage; return 0; fi
  if [ "$#" -eq 1 ]; then
    case "$1" in -h | -\? | --help) prep_usage; return 0 ;; esac
  fi
  local dry=0
  local positional=()
  local arg
  while [ "$#" -gt 0 ]; do
    arg="$1"
    shift
    case "$arg" in
    -h | -\? | --help)
      printf 'help flags must be used by themselves\n' >&2
      return 2
    ;;
    --dry-run | -n) dry=1 ;;
    -t | --validation-token)
      printf 'prep: validation-token options are unsupported for Go\n' >&2
      return 2
      ;;
    -*)
      printf 'unsupported prep option %s\n' "$(_go_quote "$arg")" >&2
      return 2
      ;;
    *) positional+=("$arg") ;;
    esac
  done
  if [ "${#positional[@]}" -ne 2 ]; then
    printf 'usage: prep vX.Y.Z "release message" [--dry-run|-n]\n' >&2
    return 2
  fi
  local version message
  version="${positional[0]}"
  message="${positional[1]}"
  prep_run "$dry" "$version" "$message"
}

# Guarded entrypoint: run only when executed, not when sourced.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  main "$@"
fi
